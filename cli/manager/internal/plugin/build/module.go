// The .wago build module: a small generated Go module that compiles wago together
// with a project's plugins into a custom binary. Each plugin is a normal Go module
// dependency (added with `go get`, recorded in .wago/go.mod), imported through
// its `register` package so Providers() contributes to an explicit catalog. Wago's
// runtime is imported as a library (github.com/wago-org/wago/cli/runtime), so there
// are no source edits and no overlay — just `go build`.

package build

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"

	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/managedrelease"
)

const (
	// buildModuleName is the generated module's path. It never leaves the machine.
	buildModuleName = "wago.local/build"
	wagoModuleName  = "github.com/wago-org/wago"
)

type Config struct {
	RuntimeVersion string
	Profile        string
	BuildTag       string
}

type Input struct {
	Sources         []project.PluginSource    `json:"sources"`
	ProviderImports []string                  `json:"providerImports"`
	Selections      []project.PluginSelection `json:"selections"`
}

func InputFromLock(lock project.LockDocument) (Input, error) {
	if err := project.ValidateLock(lock); err != nil {
		return Input{}, err
	}
	input := Input{}
	type linkedRelease struct {
		id          string
		source      project.PluginSource
		provider    project.ProviderSource
		fingerprint string
	}
	seenSource := map[string]linkedRelease{}
	seenProvider := map[string]linkedRelease{}
	ids := make([]string, 0, len(lock.Plugins))
	for id := range lock.Plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		entry := lock.Plugins[id]
		linked := linkedRelease{id: id, source: entry.Source, provider: entry.Provider, fingerprint: entry.ReleaseFingerprint}
		if previous, ok := seenSource[entry.Source.Module]; ok {
			if previous.source != linked.source || previous.provider != linked.provider || previous.fingerprint != linked.fingerprint {
				return Input{}, fmt.Errorf("plugins %q and %q link conflicting releases for source module %q", previous.id, id, entry.Source.Module)
			}
		} else {
			seenSource[entry.Source.Module] = linked
			input.Sources = append(input.Sources, entry.Source)
		}
		if previous, ok := seenProvider[entry.Provider.ImportPath]; ok {
			if previous.source != linked.source || previous.provider != linked.provider || previous.fingerprint != linked.fingerprint {
				return Input{}, fmt.Errorf("plugins %q and %q link provider import %q from conflicting source releases", previous.id, id, entry.Provider.ImportPath)
			}
		} else {
			seenProvider[entry.Provider.ImportPath] = linked
			input.ProviderImports = append(input.ProviderImports, entry.Provider.ImportPath)
		}
		input.Selections = append(input.Selections, project.PluginSelection{
			ID: id, DefinitionDigest: entry.DefinitionDigest,
			Direct: entry.Direct, Dependencies: cloneStringMap(entry.Dependencies),
			Grants:    append([]project.AuthorityGrant(nil), entry.Grants...),
			Contracts: append([]project.ContractBinding(nil), entry.Bindings...),
			Config:    append(json.RawMessage(nil), entry.Config...),
		})
	}
	sort.Slice(input.Sources, func(i, j int) bool { return input.Sources[i].Module < input.Sources[j].Module })
	sort.Strings(input.ProviderImports)
	return input, nil
}

func cloneStringMap(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

// ensureBuildModule creates or refreshes the .wago module. Refreshing matters
// because a cached project build may have been created from a different Wago
// checkout than the currently selected runtime.
func EnsureModule(dir string) error {
	return withBuildLock(dir, func() error {
		_, err := syncBuildModule(dir)
		return err
	})
}

// syncBuildModule reconciles the generated module with the Wago source selected
// for this invocation. changed reports whether go.mod changed, which invalidates
// an otherwise matching executable cache entry.
func syncBuildModule(dir string) (changed bool, err error) {
	gomod := filepath.Join(dir, "go.mod")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	before, readErr := os.ReadFile(gomod)
	exists := readErr == nil
	current := goModJSON{}
	if exists {
		var err error
		current, err = readGoModStrict(dir)
		if err != nil {
			return false, fmt.Errorf("read generated build module: %w", err)
		}
	}
	src, haveSrc := SourceDir()
	goVer := strings.TrimPrefix(runtime.Version(), "go")
	if haveSrc {
		if v := wagoGoDirective(src); v != "" {
			goVer = v
		}
	}
	desiredReplaces := map[string]string{}
	if haveSrc {
		desiredReplaces[wagoModuleName] = filepath.ToSlash(src)
		for _, replacement := range mirroredReplaces(src) {
			old, replacement, ok := strings.Cut(replacement, "=")
			if ok && old != wagoModuleName {
				desiredReplaces[old] = replacement
			}
		}
	}
	var edits [][]string
	if !exists {
		edits = append(edits, []string{"mod", "init", buildModuleName})
	}
	edits = append(edits, []string{"mod", "edit", "-go=" + goVer})
	for _, replacement := range current.Replace {
		old := moduleVersionSpec(replacement.Old.Path, replacement.Old.Version)
		want, wanted := desiredReplaces[old]
		if !wanted || want != moduleVersionSpec(replacement.New.Path, replacement.New.Version) {
			edits = append(edits, []string{"mod", "edit", "-dropreplace=" + old})
		}
	}
	if haveSrc {
		// Local development builds against the Wago checkout and mirrors its
		// current replaces. Reconciliation above removes replaces that disappeared
		// or changed instead of leaving mutable local code in the generated module.
		edits = append(edits, []string{"mod", "edit", "-require=" + wagoModuleName + "@v0.0.0"})
		keys := make([]string, 0, len(desiredReplaces))
		for old := range desiredReplaces {
			keys = append(keys, old)
		}
		sort.Strings(keys)
		for _, old := range keys {
			edits = append(edits, []string{"mod", "edit", "-replace=" + old + "=" + desiredReplaces[old]})
		}
	}
	// Otherwise wago is expected to be published: `go mod tidy` (in
	// ensureBuiltBinary) resolves it from the module proxy — a globally-installed
	// wago needs no source checkout to build a project's plugins.
	for _, args := range edits {
		cmd := exec.Command("go", args...)
		configureGeneratedModuleGoCommand(cmd)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			if !exists {
				os.Remove(gomod)
			}
			return false, fmt.Errorf("go %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	after, err := os.ReadFile(gomod)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(before, after), nil
}

// goModJSON is the subset of `go mod edit -json` we read.
type goModJSON struct {
	Go      string
	Replace []struct {
		Old struct{ Path, Version string }
		New struct{ Path, Version string }
	}
}

func readGoMod(dir string) (goModJSON, bool) {
	m, err := readGoModStrict(dir)
	return m, err == nil
}

func readGoModStrict(dir string) (goModJSON, error) {
	cmd := exec.Command("go", "mod", "edit", "-json")
	configureGeneratedModuleGoCommand(cmd)
	cmd.Dir = dir
	data, err := cmd.Output()
	if err != nil {
		return goModJSON{}, err
	}
	var m goModJSON
	if err := json.Unmarshal(data, &m); err != nil {
		return goModJSON{}, err
	}
	return m, nil
}

func moduleVersionSpec(path, version string) string {
	if version != "" {
		return path + "@" + version
	}
	return path
}

// RejectLockedSourceReplacements prevents a generated build module from
// substituting mutable local code (or another module release) for an exact
// version and checksum recorded in wago-lock.json.
func RejectLockedSourceReplacements(dir string, sources []project.PluginSource) error {
	if len(sources) == 0 {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	m, err := readGoModStrict(dir)
	if err != nil {
		return fmt.Errorf("read generated build module: %w", err)
	}
	locked := make(map[string]project.PluginSource, len(sources))
	for _, source := range sources {
		// The active Wago source checkout is the generated runtime itself, not
		// mutable third-party plugin code. Local development intentionally replaces
		// this module and fingerprints its complete Git state in Hash.
		if source.Module == wagoModuleName {
			continue
		}
		locked[source.Module] = source
	}
	for _, replacement := range m.Replace {
		source, ok := locked[replacement.Old.Path]
		if !ok {
			continue
		}
		old := moduleVersionSpec(replacement.Old.Path, replacement.Old.Version)
		new := moduleVersionSpec(replacement.New.Path, replacement.New.Version)
		return fmt.Errorf("locked plugin source %s@%s cannot use go.mod replace %s => %s; remove the replace so Go can verify the locked version and checksum", source.Module, source.Version, old, new)
	}
	return nil
}

// wagoGoDirective returns wago's declared go version (e.g. "1.22"), or "".
func wagoGoDirective(src string) string {
	m, _ := readGoMod(src)
	return m.Go
}

// mirroredReplaces renders wago's `replace` directives as `old=new` specs for the
// build module, resolving filesystem paths to absolute (relative to src).
func mirroredReplaces(src string) []string {
	m, ok := readGoMod(src)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(m.Replace))
	for _, r := range m.Replace {
		old := r.Old.Path
		if r.Old.Version != "" {
			old += "@" + r.Old.Version
		}
		newSpec := r.New.Path
		if isFilesystemPath(r.New.Path) {
			p := r.New.Path
			if !filepath.IsAbs(p) {
				p = filepath.Join(src, p)
			}
			// Skip a local replace whose target isn't present — e.g. an installed
			// wago has no sibling plugin checkout — so the plugin resolves via
			// `go get` (published) instead of a dangling path.
			if _, err := os.Stat(p); err != nil {
				continue
			}
			newSpec = filepath.ToSlash(p)
		} else if r.New.Version != "" {
			newSpec = r.New.Path + "@" + r.New.Version
		}
		out = append(out, old+"="+newSpec)
	}
	return out
}

func isFilesystemPath(p string) bool {
	return p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || filepath.IsAbs(p)
}

// goGetDep runs `go get modspec` in the build module (modspec may be module@ver).
// goRun runs `go <args>` in dir. When verbose, it streams go's output live;
// otherwise it captures it and only surfaces it on failure (quiet success, like
// npm). Errors include the tail of go's output for context.
func RunGo(dir string, verbose bool, args ...string) error {
	return withBuildLock(dir, func() error { return runGo(dir, verbose, args...) })
}

func configureGeneratedModuleGoCommand(command *exec.Cmd) {
	env := command.Env
	if env == nil {
		env = os.Environ()
	}
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "GOWORK=") {
			filtered = append(filtered, entry)
		}
	}
	command.Env = append(filtered, "GOWORK=off")
	automation.ConfigureCommand(command)
}

func runGo(dir string, verbose bool, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	configureGeneratedModuleGoCommand(cmd)
	if verbose {
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) > 0 {
		os.Stderr.Write(out)
	}
	return err
}

// goGetDep runs `go get modspec` in the build module (verbose streams output).
func Get(dir, modspec string, verbose bool) error {
	if verbose {
		return RunGo(dir, true, "get", modspec)
	}
	return withBuildLock(dir, func() error {
		cmd := exec.Command("go", "get", modspec)
		cmd.Dir = dir
		cmd.Env = os.Environ()
		configureGeneratedModuleGoCommand(cmd)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		return &FetchError{Err: err, Output: string(out)}
	})
}

// FetchError retains Go's diagnostic so callers can classify common failures
// without dumping Git and module-cache implementation details into the UI.
type FetchError struct {
	Err    error
	Output string
}

func (e *FetchError) Error() string { return e.Err.Error() }
func (e *FetchError) Unwrap() error { return e.Err }

func IsNotFound(err error) bool {
	var fetch *FetchError
	if !errors.As(err, &fetch) {
		return false
	}
	output := strings.ToLower(fetch.Output)
	return strings.Contains(output, "repository not found") ||
		strings.Contains(output, "unrecognized import path")
}

// goUpdate runs `go get -u target` (update to latest) in the build module.
func Update(dir, target string, verbose bool) error {
	return RunGo(dir, verbose, "get", "-u", target)
}

// WriteMain generates a catalog whose register packages are called explicitly.
func WriteMain(dir string, input Input, config Config) error {
	return withBuildLock(dir, func() error { return writeMain(dir, input, config, newBuildIdentity(Hash(input, config))) })
}

func writeMain(dir string, input Input, config Config, buildIdentity string) error {
	data, err := renderMain(input, config, buildIdentity)
	if err != nil {
		return err
	}
	return atomicfile.ReplaceFile(filepath.Join(dir, "main.go"), atomicfile.Options{Mode: 0o644}, func(writer io.Writer) error {
		_, err := writer.Write(data)
		return err
	})
}

func renderMain(input Input, config Config, buildIdentity string) ([]byte, error) {
	selectionJSON, err := json.Marshal(input.Selections)
	if err != nil {
		return nil, fmt.Errorf("encode plugin selections: %w", err)
	}
	var b strings.Builder
	b.WriteString("// Code generated by `wago`. DO NOT EDIT.\npackage main\n\nimport (\n")
	b.WriteString("\t\"encoding/json\"\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"os\"\n")
	b.WriteString("\twago \"github.com/wago-org/wago\"\n")
	b.WriteString("\truntime \"github.com/wago-org/wago/cli/runtime\"\n")
	for index, importPath := range input.ProviderImports {
		fmt.Fprintf(&b, "\tprovider%d %q\n", index, importPath)
	}
	b.WriteString(")\n\n")
	fmt.Fprintf(&b, "const version = %q\n", config.RuntimeVersion)
	fmt.Fprintf(&b, "const buildIdentity = %q\n\n", buildIdentity)
	fmt.Fprintf(&b, "var selectionJSON = []byte(%q)\n\n", selectionJSON)
	b.WriteString("func pluginSet() wago.PluginSet {\n")
	b.WriteString("\tvar selections []wago.PluginSelection\n")
	b.WriteString("\tif err := json.Unmarshal(selectionJSON, &selections); err != nil { panic(err) }\n")
	b.WriteString("\tvar providers []wago.PluginProvider\n")
	for index := range input.ProviderImports {
		fmt.Fprintf(&b, "\tproviders = append(providers, provider%d.Providers()...)\n", index)
	}
	b.WriteString("\treturn wago.PluginSet{Providers: providers, Selections: selections}\n}\n\n")
	b.WriteString("func main() {\n")
	b.WriteString("\tset := pluginSet()\n")
	b.WriteString("\tif os.Getenv(\"WAGO_INTERNAL_VALIDATE_PLUGIN_SET\") == \"1\" {\n")
	b.WriteString("\t\tif _, err := wago.InspectPluginPlan(set); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }; return\n")
	b.WriteString("\t}\n")
	b.WriteString("\truntime.MainWithPluginSet(version, buildIdentity, set)\n")
	b.WriteString("}\n")
	return []byte(b.String()), nil
}

// ensureBuiltBinary builds (or reuses a cached) custom wago binary at
// .wago/bin/wago for deps. cached reports a hash hit (deps + toolchain unchanged).
func EnsureBinary(dir string, input Input, force, verbose bool, config Config) (bin string, cached bool, err error) {
	err = withBuildLock(dir, func() error {
		bin, cached, err = ensureBinary(dir, input, force, verbose, config)
		return err
	})
	return bin, cached, err
}

func ensureBinary(dir string, input Input, force, verbose bool, config Config) (bin string, cached bool, err error) {
	bin = BinaryPath(dir)
	hashFile := bin + ".hash"
	// Check before reconciliation so a preexisting replacement cannot be
	// silently dropped and mistaken for a reusable exact-source build.
	if err := RejectLockedSourceReplacements(dir, input.Sources); err != nil {
		return "", false, err
	}
	if _, err := syncBuildModule(dir); err != nil {
		return "", false, err
	}
	// syncBuildModule may mirror replaces from a development Wago checkout.
	// Locked plugin sources must remain exact even in that configuration.
	if err := RejectLockedSourceReplacements(dir, input.Sources); err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return "", false, err
	}
	// go mod tidy needs the current provider imports before it can resolve the
	// exact module graph. The pending identity is stable and is never compiled.
	if err := writeMain(dir, input, config, "pending-"+Hash(input, config)); err != nil {
		return "", false, err
	}
	if err := tidyBuildModule(dir, verbose); err != nil {
		_, haveSrc := SourceDir()
		if !haveSrc {
			return "", false, fmt.Errorf("go mod tidy: %w\n  (wago may not be published yet; set WAGO_SRC to a wago checkout to build from source)", err)
		}
		return "", false, fmt.Errorf("go mod tidy: %w", err)
	}
	want, cacheable, err := resolvedBuildHash(dir, input, config)
	if err != nil {
		return "", false, err
	}
	if !force && cacheable {
		if b, err := os.ReadFile(hashFile); err == nil && strings.TrimSpace(string(b)) == want {
			if _, err := os.Stat(bin); err == nil {
				return bin, true, nil
			}
		}
	}
	// Invalidate the old key before replacing the binary. A crash can now cause
	// an extra rebuild, but it cannot pair a new binary with an old valid key.
	if err := os.Remove(hashFile); err != nil && !os.IsNotExist(err) {
		return "", false, fmt.Errorf("invalidate plugin build hash: %w", err)
	}
	buildIdentity := newBuildIdentity(want)
	if err := writeMain(dir, input, config, buildIdentity); err != nil {
		return "", false, err
	}
	// -buildvcs=false: the generated build module is a local throwaway; VCS
	// stamping is meaningless and would otherwise fail when .wago sits inside an
	// unrelated or partial git work tree.
	temporary, err := atomicfile.CreateTemp(bin)
	if err != nil {
		return "", false, fmt.Errorf("prepare plugin build: %w", err)
	}
	staged := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(staged)
		return "", false, fmt.Errorf("prepare plugin build: %w", err)
	}
	defer os.Remove(staged)
	buildStep := []string{"build", "-buildvcs=false"}
	if tag := config.BuildTag; tag != "" {
		buildStep = append(buildStep, "-tags", tag)
	}
	buildStep = append(buildStep, "-o", staged, ".")
	if verbose {
		fmt.Fprintf(os.Stderr, "%s go %s\n", ui.Dim("→"), strings.Join(buildStep, " "))
	}
	if err := runGo(dir, verbose, buildStep...); err != nil {
		_ = os.Remove(staged)
		return "", false, fmt.Errorf("go build: %w", err)
	}
	// Local sources are outside the build lock. Prove that they did not change
	// between the cache decision and compilation before publishing the result.
	after, afterCacheable, err := resolvedBuildHash(dir, input, config)
	if err != nil {
		return "", false, err
	}
	if err := rejectChangedBuildInputs(want, after); err != nil {
		return "", false, err
	}
	if err := atomicfile.CommitTempFile(staged, bin, atomicfile.Options{Mode: 0o755, Sync: true}); err != nil {
		return "", false, fmt.Errorf("install plugin build: %w", err)
	}
	if !cacheable || !afterCacheable {
		return bin, false, nil
	}
	if err := atomicfile.ReplaceFile(hashFile, atomicfile.Options{Mode: 0o644}, func(writer io.Writer) error {
		_, err := io.WriteString(writer, want)
		return err
	}); err != nil {
		return "", false, fmt.Errorf("publish plugin build hash: %w", err)
	}
	return bin, false, nil
}

func tidyBuildModule(dir string, verbose bool) error {
	_, vendorMode, err := inspectVendorModules(dir)
	if err != nil {
		return err
	}
	if vendorMode {
		return nil
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "%s go mod tidy\n", ui.Dim("→"))
	}
	return runGo(dir, verbose, "mod", "tidy")
}

func rejectChangedBuildInputs(want, after string) error {
	if after != want {
		return fmt.Errorf("plugin build inputs changed during compilation; retry the build")
	}
	return nil
}

func newBuildIdentity(buildHash string) string {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ""
	}
	h := sha256.New()
	h.Write([]byte("wago-plugin-runtime\x00"))
	h.Write([]byte(buildHash))
	h.Write(nonce[:])
	return hex.EncodeToString(h.Sum(nil))
}

// ModuleVersion returns a module's selected version from a generated plugin
// build module while excluding concurrent edits to that module.
func ModuleVersion(dir, module string) (version string, ok bool) {
	_ = withBuildLock(dir, func() error {
		cmd := exec.Command("go", "list", "-m", "-f={{.Version}}", module)
		configureGeneratedModuleGoCommand(cmd)
		cmd.Dir = dir
		if output, err := cmd.Output(); err == nil {
			version = strings.TrimSpace(string(output))
			ok = version != ""
		}
		return nil
	})
	return version, ok
}

func BinaryPath(dir string) string {
	return filepath.Join(dir, "bin", "wago"+exeSuffix())
}

// Hash is the stable pre-resolution identity for generated source. EnsureBinary
// adds the resolved module graph, build environment, and selected source files
// before it uses a hash as an executable cache key.
func Hash(input Input, config Config) string {
	h := sha256.New()
	fmt.Fprintf(h, "wago-build-v2\x00%s\x00%s\x00%s\x00%s\x00%s/%s\x00", config.RuntimeVersion, config.Profile, config.BuildTag, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if encoded, err := json.Marshal(input); err == nil {
		_, _ = h.Write(encoded)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// wagoSourceDir returns a local wago checkout to build against, if one is
// available (WAGO_SRC, or running inside the wago module). When false, wago is
// taken from the module proxy instead — the published-install path.
func SourceDir() (string, bool) {
	d, err := ModuleDir()
	if err != nil {
		return "", false
	}
	return d, true
}

// wagoModuleDir locates the wago source to build against. Uses WAGO_SRC if set,
// else the current Go module when that is github.com/wago-org/wago.
func ModuleDir() (string, error) {
	if d := os.Getenv("WAGO_SRC"); d != "" {
		return d, nil
	}
	if source := managedrelease.Source(); source != "" {
		return source, nil
	}
	// Inside a wago checkout (e.g. hacking on wago itself)? Use it.
	command := exec.Command("go", "env", "GOMOD")
	automation.ConfigureCommand(command)
	if out, err := command.Output(); err == nil {
		gomod := strings.TrimSpace(string(out))
		if gomod != "" && gomod != os.DevNull {
			if b, err := os.ReadFile(gomod); err == nil && strings.Contains(string(b), "module github.com/wago-org/wago") {
				return filepath.Dir(gomod), nil
			}
		}
	}
	// Otherwise the source the installer keeps at ~/.wago/src, so an installed
	// wago builds plugins with no checkout. (Only needed while wago is unpublished;
	// once it ships, the .wago module just `go get`s it.)
	if d := InstalledSource(); d != "" {
		return d, nil
	}
	return "", fmt.Errorf("no wago source found; set WAGO_SRC to a wago checkout, or reinstall via wago.sh so the source is kept for plugin builds")
}

// installedWagoSource returns the wago source the installer places at ~/.wago/src,
// or "" if it isn't a wago checkout.
func InstalledSource() string {
	if source := managedrelease.Source(); source != "" {
		return source
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".wago", "src")
	if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && strings.Contains(string(b), "module github.com/wago-org/wago") {
		return dir
	}
	return ""
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
