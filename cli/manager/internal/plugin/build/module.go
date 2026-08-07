// The .wago build module: a small generated Go module that compiles wago together
// with a project's plugins into a custom binary. Each plugin is a normal Go module
// dependency (added with `go get`, recorded in .wago/go.mod), blank-imported via
// its `register` package so its init() self-registers it with the engine. wago's
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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"

	"github.com/wago-org/wago/cli/internal/ui"
)

// buildModuleName is the generated module's path. It never leaves the machine.
const buildModuleName = "wago.local/build"

type Config struct {
	RuntimeVersion string
	Profile        string
	BuildTag       string
}

// registerImport is the package a build blank-imports to self-register a plugin:
// the module's conventional `register` subpackage.
func registerImport(module string) string { return module + "/register" }

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
	src, haveSrc := SourceDir()
	goVer := strings.TrimPrefix(runtime.Version(), "go")
	if haveSrc {
		if v := wagoGoDirective(src); v != "" {
			goVer = v
		}
	}
	var edits [][]string
	if !exists {
		edits = append(edits, []string{"mod", "init", buildModuleName})
	}
	edits = append(edits, []string{"mod", "edit", "-go=" + goVer})
	if haveSrc {
		// Local development: build against the wago checkout and mirror its
		// filesystem replaces so private / untagged sibling plugins resolve.
		edits = append(edits,
			[]string{"mod", "edit", "-require=github.com/wago-org/wago@v0.0.0"},
			[]string{"mod", "edit", "-replace=github.com/wago-org/wago=" + filepath.ToSlash(src)},
		)
		for _, r := range mirroredReplaces(src) {
			edits = append(edits, []string{"mod", "edit", "-replace=" + r})
		}
	} else if exists {
		// A generated module can outlive an installed source checkout. Do not
		// leave it pinned to a stale local tree when it should use the proxy.
		edits = append(edits, []string{"mod", "edit", "-dropreplace=github.com/wago-org/wago"})
	}
	// Otherwise wago is expected to be published: `go mod tidy` (in
	// ensureBuiltBinary) resolves it from the module proxy — a globally-installed
	// wago needs no source checkout to build a project's plugins.
	for _, args := range edits {
		cmd := exec.Command("go", args...)
		automation.ConfigureCommand(cmd)
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
	cmd := exec.Command("go", "mod", "edit", "-json")
	automation.ConfigureCommand(cmd)
	cmd.Dir = dir
	data, err := cmd.Output()
	if err != nil {
		return goModJSON{}, false
	}
	var m goModJSON
	if json.Unmarshal(data, &m) != nil {
		return goModJSON{}, false
	}
	return m, true
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

func runGo(dir string, verbose bool, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	automation.ConfigureCommand(cmd)
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
		automation.ConfigureCommand(cmd)
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

// writeBuildMain generates .wago/main.go: import wago's CLI as a library and
// blank-import each dependency's register package.
func WriteMain(dir string, deps []string, config Config) error {
	return withBuildLock(dir, func() error { return writeMain(dir, deps, config, newBuildIdentity(Hash(deps, config))) })
}

func writeMain(dir string, deps []string, config Config, buildIdentity string) error {
	sorted := append([]string(nil), deps...)
	sort.Strings(sorted)
	var b strings.Builder
	b.WriteString("// Code generated by `wago`. DO NOT EDIT.\npackage main\n\nimport (\n")
	b.WriteString("\truntime \"github.com/wago-org/wago/cli/runtime\"\n")
	for _, m := range sorted {
		fmt.Fprintf(&b, "\t_ %q\n", registerImport(m))
	}
	b.WriteString(")\n\n")
	fmt.Fprintf(&b, "const version = %q\n", config.RuntimeVersion)
	fmt.Fprintf(&b, "const buildIdentity = %q\n\n", buildIdentity)
	b.WriteString("func main() { runtime.MainWithArtifactCacheIdentity(version, buildIdentity) }\n")
	return os.WriteFile(filepath.Join(dir, "main.go"), []byte(b.String()), 0o644)
}

// ensureBuiltBinary builds (or reuses a cached) custom wago binary at
// .wago/bin/wago for deps. cached reports a hash hit (deps + toolchain unchanged).
func EnsureBinary(dir string, deps []string, force, verbose bool, config Config) (bin string, cached bool, err error) {
	err = withBuildLock(dir, func() error {
		bin, cached, err = ensureBinary(dir, deps, force, verbose, config)
		return err
	})
	return bin, cached, err
}

func ensureBinary(dir string, deps []string, force, verbose bool, config Config) (bin string, cached bool, err error) {
	bin = BinaryPath(dir)
	hashFile := bin + ".hash"
	want := Hash(deps, config)
	moduleChanged, err := syncBuildModule(dir)
	if err != nil {
		return "", false, err
	}
	if !force && !moduleChanged {
		if b, err := os.ReadFile(hashFile); err == nil && strings.TrimSpace(string(b)) == want {
			if _, err := os.Stat(bin); err == nil {
				return bin, true, nil
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return "", false, err
	}
	buildIdentity := newBuildIdentity(want)
	if err := writeMain(dir, deps, config, buildIdentity); err != nil {
		return "", false, err
	}
	// Resolve the import graph (fetch any published plugins; local replaces stay
	// local), then compile.
	_, haveSrc := SourceDir()
	// -buildvcs=false: the generated build module is a local throwaway; VCS
	// stamping is meaningless and would otherwise fail when .wago sits inside an
	// unrelated or partial git work tree.
	staged := bin + ".tmp"
	_ = os.Remove(staged)
	buildStep := []string{"build", "-buildvcs=false"}
	if tag := config.BuildTag; tag != "" {
		buildStep = append(buildStep, "-tags", tag)
	}
	buildStep = append(buildStep, "-o", staged, ".")
	for _, step := range [][]string{{"mod", "tidy"}, buildStep} {
		if verbose {
			fmt.Fprintf(os.Stderr, "%s go %s\n", ui.Dim("→"), strings.Join(step, " "))
		}
		if err := runGo(dir, verbose, step...); err != nil {
			_ = os.Remove(staged)
			if step[0] == "mod" && !haveSrc {
				return "", false, fmt.Errorf("go mod tidy: %w\n  (wago may not be published yet — set WAGO_SRC to a wago checkout to build from source)", err)
			}
			return "", false, fmt.Errorf("go %s: %w", step[0], err)
		}
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(bin)
	}
	if err := os.Rename(staged, bin); err != nil {
		_ = os.Remove(staged)
		return "", false, fmt.Errorf("install plugin build: %w", err)
	}
	_ = os.WriteFile(hashFile, []byte(want), 0o644)
	return bin, false, nil
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
		automation.ConfigureCommand(cmd)
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

// buildHash keys the built binary on the exact dependency set, toolchain, and
// Wago source used by the generated module. Including local source state keeps
// `go run ./cli/wago` from handing commands to a stale plugin binary.
func Hash(deps []string, config Config) string {
	sorted := append([]string(nil), deps...)
	sort.Strings(sorted)
	h := sha256.New()
	fmt.Fprintf(h, "wago-build\x00%s\x00%s\x00%s\x00%s/%s\x00", config.RuntimeVersion, config.Profile, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if src, ok := SourceDir(); ok {
		fmt.Fprintf(h, "source\x00%s\x00", localSourceFingerprint(src))
	}
	for _, d := range sorted {
		fmt.Fprintf(h, "%s\x00", d)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func localSourceFingerprint(src string) string {
	h := sha256.New()
	fmt.Fprint(h, filepath.Clean(src), "\x00")
	for _, args := range [][]string{
		{"rev-parse", "HEAD"},
		{"diff", "--no-ext-diff", "--binary", "HEAD", "--", "."},
	} {
		command := exec.Command("git", append([]string{"-C", src}, args...)...)
		if output, err := command.Output(); err == nil {
			_, _ = h.Write(output)
		}
		_, _ = h.Write([]byte{0})
	}
	command := exec.Command("git", "-C", src, "ls-files", "--others", "--exclude-standard", "-z")
	if output, err := command.Output(); err == nil {
		for _, name := range bytes.Split(output, []byte{0}) {
			if len(name) == 0 {
				continue
			}
			_, _ = h.Write(name)
			_, _ = h.Write([]byte{0})
			if content, readErr := os.ReadFile(filepath.Join(src, filepath.FromSlash(string(name)))); readErr == nil {
				_, _ = h.Write(content)
			}
			_, _ = h.Write([]byte{0})
		}
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
