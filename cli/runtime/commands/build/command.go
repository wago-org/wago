// Package build implements wago build.
package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/settings"
	"github.com/wago-org/wago/cli/internal/ui"
	runcmd "github.com/wago-org/wago/cli/runtime/commands/run"
	"github.com/wago-org/wago/cli/runtime/internal/modulefile"
	"github.com/wago-org/wago/internal/atomicfile"
)

type Environment interface {
	ProfileFlags() []command.Flag
	LoadRuntime(*wago.RuntimeConfig, []string) *wago.Runtime
}

func Command(environment Environment) *command.Cmd {
	flags := []command.Flag{
		{Name: "output", Short: "o", Arg: "<file>", Help: "output path (default: input name with .wago extension)"},
		runcmd.ParallelFlag(),
	}
	flags = append(flags, environment.ProfileFlags()...)
	knobs := append(runcmd.DeferredBoundsCheckingFlags(), runcmd.OptimizationFlags()...)
	parserFlags := append(append([]command.Flag(nil), flags...), knobs...)
	implementation := implementation{environment: environment}
	return &command.Cmd{
		Name: "build", Summary: "precompile a WebAssembly module to a .wago artifact",
		Automation: command.DryRun,
		Args:       "<file>", Flags: flags, Knobs: knobs,
		Normalize: func(args []string) ([]string, error) {
			return runcmd.NormalizeParallelArgs(args, parserFlags, false)
		},
		Long: ".wago artifacts are host-architecture-specific and must be rebuilt after incompatible Wago upgrades.",
		Run:  implementation.Run,
	}
}

type implementation struct {
	environment Environment
}

func (cmd implementation) Run(c *command.Ctx) {
	deferredBoundsChecking, err := runcmd.DeferredBoundsOverride(c)
	if err != nil {
		usageBuildError(err)
	}
	optimizations, err := runcmd.OptimizationOverrides(c)
	if err != nil {
		usageBuildError(err)
	}
	input := singleFileArg(c.Args)
	output := c.Str("output")
	if output == "" {
		ext := filepath.Ext(input)
		output = strings.TrimSuffix(input, ext) + ".wago"
	}
	selection, err := settings.ResolveCompilation(settings.CompilationRequest{
		Arch: runtime.GOARCH, Parallel: c.Str("parallel"), DeferredBoundsChecking: deferredBoundsChecking, Optimizations: optimizations,
	})
	if err != nil {
		if settings.IsCompilationSettingsError(err) {
			fatalBuildError(err)
		}
		usageBuildError(err)
	}
	if automation.DryRun() {
		plan := map[string]any{
			"input": input, "output": output, "parallel": c.Str("parallel"),
			"functionWorkers": selection.FunctionWorkers, "deferredBoundsChecking": selection.DeferredBoundsChecking,
		}
		if len(selection.Optimizations) != 0 {
			plan["optimizations"] = selection.Optimizations
		}
		automation.PrintPlan("build artifact", plan)
		return
	}
	if sameBuildPath(output, input) {
		ui.Usage("build: output path must differ from input")
	}
	// Build has historically accepted an output symlink. Snapshot its regular
	// target here, rather than teaching the generic atomic-file helper to follow
	// links, so later link swaps cannot redirect publication outside that target.
	outputSnapshot, err := inspectBuildOutput(output)
	if err != nil {
		fatalBuildError(err)
	}
	if outputSnapshot.targetInfo != nil {
		inputInfo, err := os.Stat(input)
		if err != nil {
			fatalBuildError(err)
		}
		inputIdentity, err := captureBuildFileIdentity(input, true, inputInfo)
		if err != nil {
			fatalBuildError(err)
		}
		if sameBuildFileIdentity(inputIdentity, outputSnapshot.targetIdentity) {
			ui.Usage("build: output path must differ from input")
		}
		// Check publication-incompatible metadata only after the input alias check,
		// so a same-file request retains its established, more actionable error. The
		// identity-bound metadata capture repeats this immediately before publication.
		if err := validateBuildOutputPublication(outputSnapshot.publicationPath, outputSnapshot.targetInfo, outputSnapshot.targetIdentity); err != nil {
			fatalBuildError(err)
		}
	}
	source, err := modulefile.Read(input)
	if err != nil {
		fatalBuildError(err)
	}
	if wago.IsCompiled(source) {
		ui.Fatal("build: %s is already a .wago artifact", input)
	}
	cfg := selection.RuntimeConfig()
	rt := cmd.environment.LoadRuntime(cfg, nil)
	module, err := rt.Compile(source)
	if err != nil {
		_ = rt.CloseContext(context.Background())
		fatalBuildError(err)
	}
	// Close the Module before synchronously closing its Runtime so module-close
	// observers finish while their plugin generation is still active.
	finish := func() error {
		return errors.Join(module.Close(), rt.CloseContext(context.Background()))
	}
	artifact, err := module.Compiled().MarshalBinary()
	if err != nil {
		_ = finish()
		fatalBuildError(err)
	}
	// Marshaling owns the artifact bytes, so finish teardown before writing the
	// output. This preserves the old artifact on teardown failure; atomic
	// publication below also preserves it against every later write failure.
	if err := finish(); err != nil {
		ui.Fatal("build: teardown: %v", err)
	}
	publicationPath, outputMode, outputExists, outputInfo, err := outputSnapshot.revalidate()
	if err != nil {
		fatalBuildError(err)
	}
	outputMetadata, err := captureBuildOutputMetadata(publicationPath, outputInfo, outputSnapshot.targetIdentity)
	if err != nil {
		fatalBuildError(err)
	}
	if !outputExists {
		outputMetadata, err = captureNewBuildOutputMetadata(publicationPath)
		if err != nil {
			fatalBuildError(err)
		}
	}
	// atomicfile creates parent directories for callers that want that behavior,
	// but build's former os.WriteFile contract rejected a misspelled -o parent.
	// This compatibility check is intentionally build-specific and does not claim
	// to pin the directory against a concurrent replacement after Stat returns.
	if err := validateBuildOutputParent(publicationPath); err != nil {
		fatalBuildError(err)
	}
	// Publish only a fully written and synced artifact. A failed build must leave
	// an existing runnable artifact intact, including mode, ownership, and ACL.
	// Keep atomicfile's parent-creation default for its other callers, but retain
	// build's historical missing-parent error even if the checked directory is
	// removed before temp creation. Revalidate the original output snapshot at the
	// narrowest portable boundary before rename. The pathname check and rename
	// cannot be one atomic operation across every supported OS, so this reduces but
	// does not claim to eliminate all concurrent replacement races.
	options := atomicfile.Options{
		Mode: outputMode, ModeSet: true, ApplyUmask: !outputExists, Sync: true,
		RequireExistingParent: true, RetainReplaceHandle: retainBuildReplaceHandle(outputExists),
		BeforeReplace: func(destination string) error {
			return outputSnapshot.validateBeforeReplace(destination, outputInfo, outputMetadata)
		},
	}
	if err := atomicfile.ReplaceFile(publicationPath, options, func(writer io.Writer) error {
		if _, err := writer.Write(artifact); err != nil {
			return err
		}
		// Restore metadata after writing because Unix chown may recalculate an ACL.
		// ReplaceFile still applies the original mode and syncs before publication.
		if err := applyBuildOutputMetadata(writer, outputMetadata); err != nil {
			return err
		}
		return nil
	}); err != nil {
		fatalBuildError(err)
	}
	fmt.Printf("%s built %s\n", ui.Cyan("✓"), output)
}

func validateBuildOutputParent(path string) error {
	directory := filepath.Dir(path)
	info, err := os.Stat(directory)
	if err != nil {
		return buildErrorSE("inspect output directory %s: %w", directory, err)
	}
	if !info.IsDir() {
		return buildErrorS("output directory %s is not a directory", directory)
	}
	return nil
}

func (snapshot buildOutputSnapshot) validateBeforeReplace(destination string, expectedInfo os.FileInfo, expectedMetadata buildOutputMetadata) error {
	publicationPath, _, _, currentInfo, err := snapshot.revalidate()
	if err != nil {
		return buildErrorE("output changed before publication: %w", err)
	}
	if !sameBuildPath(publicationPath, destination) {
		return formatBuildError("output changed before publication: %s -> %s", destination, publicationPath)
	}
	if expectedInfo == nil {
		// Missing Windows and Darwin outputs inherit access policy from their parent. Re-sample
		// that metadata at the final boundary so a concurrent permission tightening
		// is not undone by publishing the earlier snapshot. Windows and Darwin
		// capture inherited metadata; other platforms return the zero value here.
		currentMetadata, err := captureNewBuildOutputMetadata(publicationPath)
		if err != nil {
			return buildErrorE("output metadata changed before publication: %w", err)
		}
		if !sameBuildOutputMetadata(expectedMetadata, currentMetadata) {
			return newBuildError("output metadata changed before publication")
		}
		return nil
	}
	if currentInfo.Mode() != expectedInfo.Mode() {
		return formatBuildError("output metadata changed before publication: %s -> %s", expectedInfo.Mode(), currentInfo.Mode())
	}
	currentMetadata, err := captureBuildOutputMetadata(publicationPath, currentInfo, snapshot.targetIdentity)
	if err != nil {
		return buildErrorE("output metadata changed before publication: %w", err)
	}
	// Re-read the access metadata after staging is complete. Publishing the
	// earlier copy after another actor tightened access would silently undo that
	// change. This bounded build-only check narrows the portable validation/rename
	// race without claiming that pathname comparison and rename are one operation.
	if !sameBuildOutputMetadata(expectedMetadata, currentMetadata) {
		return newBuildError("output metadata changed before publication")
	}
	return nil
}

type buildOutputSnapshot struct {
	requestedPath   string
	publicationPath string
	linkInfo        os.FileInfo
	targetInfo      os.FileInfo
	linkIdentity    buildFileIdentity
	targetIdentity  buildFileIdentity
}

func inspectBuildOutput(path string) (buildOutputSnapshot, error) {
	snapshot := buildOutputSnapshot{requestedPath: path, publicationPath: path}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		publicationPath, resolveErr := resolveBuildOutputPublicationPath(path)
		if resolveErr != nil {
			return snapshot, resolveErr
		}
		snapshot.publicationPath = publicationPath
		return snapshot, nil
	}
	if err != nil {
		return snapshot, buildErrorSE("inspect output %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		if !info.Mode().IsRegular() {
			return snapshot, buildErrorS("output %s is not a regular file", path)
		}
		publicationPath, err := resolveBuildOutputPublicationPath(path)
		if err != nil {
			return snapshot, err
		}
		identity, err := captureBuildFileIdentity(path, false, info)
		if err != nil {
			return snapshot, err
		}
		snapshot.targetInfo = info
		snapshot.targetIdentity = identity
		snapshot.publicationPath = publicationPath
		return snapshot, nil
	}

	linkIdentity, err := captureBuildFileIdentity(path, false, info)
	if err != nil {
		return snapshot, err
	}
	resolved, targetInfo, targetIdentity, err := resolveBuildOutputSymlink(path)
	if err != nil {
		return snapshot, err
	}
	snapshot.publicationPath = resolved
	snapshot.linkInfo = info
	snapshot.targetInfo = targetInfo
	snapshot.linkIdentity = linkIdentity
	snapshot.targetIdentity = targetIdentity
	return snapshot, nil
}

func resolveBuildOutputPublicationPath(path string) (string, error) {
	parentPath, leaf := filepath.Split(path)
	if leaf == "" || leaf == "." || leaf == ".." {
		return "", buildErrorS("output path does not name a file: %s", path)
	}
	if parentPath == "" {
		parentPath = "."
	}
	parent, err := filepath.EvalSymlinks(parentPath)
	if err != nil {
		return "", buildErrorSE("resolve output directory %s: %w", parentPath, err)
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return "", buildErrorSE("resolve output directory %s: %w", parentPath, err)
	}
	// Use the physical parent for staging, publication, and cleanup. A caller may
	// still address the output through a symlinked directory, but swapping that
	// link cannot redirect atomicfile's lexical temp name after it is created.
	// This does not pin the directory inode itself: renaming or replacing a
	// physical ancestor remains part of the documented portable pathname race.
	return filepath.Join(parent, leaf), nil
}

func resolveBuildOutputSymlink(path string) (string, os.FileInfo, buildFileIdentity, error) {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", nil, buildFileIdentity{}, buildErrorSE("resolve output symlink %s: %w", path, err)
	}
	for linkCount := 0; ; {
		parentPath, leaf := filepath.Split(resolved)
		if leaf == "" || leaf == "." || leaf == ".." {
			return "", nil, buildFileIdentity{}, buildErrorS("resolve output symlink %s: non-file target", path)
		}
		// Resolve the still-lexical parent before joining its final leaf. Cleaning
		// first could collapse a missing intermediate such as missing/../artifact
		// and grant creation where the former os.WriteFile path returned ENOENT.
		parent, err := filepath.EvalSymlinks(parentPath)
		if err != nil {
			return "", nil, buildFileIdentity{}, buildErrorSE("resolve output symlink %s: %w", path, err)
		}
		resolved = filepath.Join(parent, leaf)
		info, err := os.Lstat(resolved)
		if os.IsNotExist(err) {
			return resolved, nil, buildFileIdentity{}, nil
		}
		if err != nil {
			return "", nil, buildFileIdentity{}, buildErrorSE("inspect output symlink target %s: %w", resolved, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(resolved)
			if err != nil {
				return "", nil, buildFileIdentity{}, buildErrorSE("resolve output symlink %s: %w", path, err)
			}
			linkCount++
			if linkCount > 255 {
				return "", nil, buildFileIdentity{}, buildErrorS("resolve output symlink %s: too many symbolic links", path)
			}
			if target == "" {
				return "", nil, buildFileIdentity{}, buildErrorS("resolve output symlink %s: empty target", path)
			}
			if os.IsPathSeparator(target[len(target)-1]) {
				// A trailing separator requires a directory. Removing it before the
				// missing-leaf check would turn a historically rejected directory
				// symlink into permission to create a regular artifact at that name.
				return "", nil, buildFileIdentity{}, buildErrorS("output symlink target: trailing path separator: %s", target)
			}
			if filepath.IsAbs(target) {
				resolved = target
			} else if filepath.VolumeName(target) != "" {
				// Windows drive-relative targets keep their explicit volume rather
				// than becoming relative to the link's containing directory.
				resolved = target
			} else if os.IsPathSeparator(target[0]) {
				// On Windows, a rooted target without a volume name inherits the
				// link's volume rather than its containing directory.
				resolved = filepath.VolumeName(resolved) + target
			} else {
				// Do not use filepath.Join here: it would clean unresolved target
				// components before the next iteration can prove their parents exist.
				resolved = filepath.Dir(resolved) + string(os.PathSeparator) + target
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return "", nil, buildFileIdentity{}, buildErrorS("output symlink target is not a regular file: %s", resolved)
		}
		identity, err := captureBuildFileIdentity(resolved, false, info)
		if err != nil {
			return "", nil, buildFileIdentity{}, err
		}
		return resolved, info, identity, nil
	}
}

func (snapshot buildOutputSnapshot) revalidate() (path string, mode os.FileMode, exists bool, info os.FileInfo, err error) {
	current, statErr := os.Lstat(snapshot.requestedPath)
	if snapshot.targetInfo == nil && snapshot.linkInfo == nil {
		if os.IsNotExist(statErr) {
			publicationPath, resolveErr := resolveBuildOutputPublicationPath(snapshot.requestedPath)
			if resolveErr != nil {
				return "", 0, false, nil, resolveErr
			}
			if !sameBuildPath(publicationPath, snapshot.publicationPath) {
				return "", 0, false, nil, newBuildError("output directory changed during build")
			}
			return snapshot.publicationPath, 0o644, false, nil, nil
		}
		if statErr != nil {
			return "", 0, false, nil, buildErrorSE("reinspect output %s: %w", snapshot.requestedPath, statErr)
		}
		return "", 0, false, nil, newBuildError("output appeared during build")
	}
	if statErr != nil {
		return "", 0, false, nil, buildErrorSE("reinspect output %s: %w", snapshot.requestedPath, statErr)
	}
	if snapshot.linkInfo == nil {
		if current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() {
			return "", 0, false, nil, newBuildError("output changed during build")
		}
		identity, err := captureBuildFileIdentity(snapshot.requestedPath, false, current)
		if err != nil {
			return "", 0, false, nil, err
		}
		if err := validateBuildOutputPublication(snapshot.requestedPath, current, identity); err != nil {
			return "", 0, false, nil, err
		}
		if !sameBuildFileIdentity(snapshot.targetIdentity, identity) {
			return "", 0, false, nil, newBuildError("output changed during build")
		}
		publicationPath, err := resolveBuildOutputPublicationPath(snapshot.requestedPath)
		if err != nil {
			return "", 0, false, nil, err
		}
		if !sameBuildPath(publicationPath, snapshot.publicationPath) {
			return "", 0, false, nil, newBuildError("output directory changed during build")
		}
		return snapshot.publicationPath, current.Mode().Perm(), true, current, nil
	}
	if current.Mode()&os.ModeSymlink == 0 {
		return "", 0, false, nil, newBuildError("output symlink changed during build")
	}
	linkIdentity, err := captureBuildFileIdentity(snapshot.requestedPath, false, current)
	if err != nil {
		return "", 0, false, nil, err
	}
	if !sameBuildFileIdentity(snapshot.linkIdentity, linkIdentity) {
		return "", 0, false, nil, newBuildError("output symlink changed during build")
	}
	resolved, targetInfo, targetIdentity, err := resolveBuildOutputSymlink(snapshot.requestedPath)
	if err != nil {
		return "", 0, false, nil, err
	}
	if snapshot.targetInfo == nil {
		if !sameBuildPath(resolved, snapshot.publicationPath) || targetInfo != nil {
			return "", 0, false, nil, newBuildError("output symlink changed during build")
		}
		return snapshot.publicationPath, 0o644, false, nil, nil
	}
	if targetInfo == nil || !sameBuildPath(resolved, snapshot.publicationPath) || !sameBuildFileIdentity(snapshot.targetIdentity, targetIdentity) {
		return "", 0, false, nil, newBuildError("output symlink changed during build")
	}
	if err := validateBuildOutputPublication(resolved, targetInfo, targetIdentity); err != nil {
		return "", 0, false, nil, err
	}
	return snapshot.publicationPath, targetInfo.Mode().Perm(), true, targetInfo, nil
}

func singleFileArg(args []string) string {
	if len(args) != 1 {
		ui.Usage("build: need exactly one <file>")
	}
	return args[0]
}
