// The .wago build module: a small generated Go module that compiles wago together
// with a project's plugins into a custom binary. Each plugin is a normal Go module
// dependency (added with `go get`, recorded in .wago/go.mod), blank-imported via
// its `register` package so its init() self-registers it with the engine. wago's
// own CLI is imported as a library (github.com/wago-org/wago/cli/wagocli), so there
// are no source edits and no overlay — just `go build`.
package wagocli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// buildModuleName is the generated module's path. It never leaves the machine.
const buildModuleName = "wago.local/build"

// buildDirFor returns the .wago build directory: <cwd>/.wago by default, or
// <home>/.wago with --global (a single CLI-wide plugin set).
func buildDirFor(global bool) (string, error) {
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".wago"), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, ".wago"), nil
}

// depsSource returns the directory whose wago.json holds the dependency list for a
// build: the current directory locally, or the global build dir with --global.
func depsSource(global bool) (string, error) {
	if global {
		return buildDirFor(true)
	}
	return ".", nil
}

// registerImport is the package a build blank-imports to self-register a plugin:
// the module's conventional `register` subpackage.
func registerImport(module string) string { return module + "/register" }

// ensureBuildModule creates the .wago module's go.mod if absent. It requires and
// path-replaces wago to the local source, and mirrors wago's own filesystem
// `replace`s (as absolute paths) so private / untagged sibling plugins resolve in
// local development the same way they do for a wago checkout. Once wago is
// published these local replaces simply won't exist and `go get` fetches releases.
func ensureBuildModule(dir string) error {
	gomod := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(gomod); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	src, haveSrc := wagoSourceDir()
	goVer := strings.TrimPrefix(runtime.Version(), "go")
	if haveSrc {
		if v := wagoGoDirective(src); v != "" {
			goVer = v
		}
	}
	edits := [][]string{
		{"mod", "init", buildModuleName},
		{"mod", "edit", "-go=" + goVer},
	}
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
	}
	// Otherwise wago is expected to be published: `go mod tidy` (in
	// ensureBuiltBinary) resolves it from the module proxy — a globally-installed
	// wago needs no source checkout to build a project's plugins.
	for _, args := range edits {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			os.Remove(gomod)
			return fmt.Errorf("go %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
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
func goGetDep(dir, modspec string) error {
	cmd := exec.Command("go", "get", modspec)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// writeBuildMain generates .wago/main.go: import wago's CLI as a library and
// blank-import each dependency's register package.
func writeBuildMain(dir string, deps []string) error {
	sorted := append([]string(nil), deps...)
	sort.Strings(sorted)
	var b strings.Builder
	b.WriteString("// Code generated by `wago pkg`. DO NOT EDIT.\npackage main\n\nimport (\n")
	b.WriteString("\twagocli \"github.com/wago-org/wago/cli/wagocli\"\n")
	for _, m := range sorted {
		fmt.Fprintf(&b, "\t_ %q\n", registerImport(m))
	}
	b.WriteString(")\n\n")
	fmt.Fprintf(&b, "const version = %q\n\n", versionString())
	b.WriteString("func main() { wagocli.Main(version) }\n")
	return os.WriteFile(filepath.Join(dir, "main.go"), []byte(b.String()), 0o644)
}

// ensureBuiltBinary builds (or reuses a cached) custom wago binary at
// .wago/bin/wago for deps. cached reports a hash hit (deps + toolchain unchanged).
func ensureBuiltBinary(dir string, deps []string) (bin string, cached bool, err error) {
	bin = filepath.Join(dir, "bin", "wago"+exeSuffix())
	hashFile := bin + ".hash"
	want := buildHash(deps)
	if b, err := os.ReadFile(hashFile); err == nil && strings.TrimSpace(string(b)) == want {
		if _, err := os.Stat(bin); err == nil {
			return bin, true, nil
		}
	}
	if err := ensureBuildModule(dir); err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return "", false, err
	}
	if err := writeBuildMain(dir, deps); err != nil {
		return "", false, err
	}
	// Resolve the import graph (fetch any published plugins; local replaces stay
	// local), then compile.
	_, haveSrc := wagoSourceDir()
	for _, step := range [][]string{{"mod", "tidy"}, {"build", "-o", bin, "."}} {
		cmd := exec.Command("go", step...)
		cmd.Dir = dir
		cmd.Env = os.Environ()
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if step[0] == "mod" && !haveSrc {
				return "", false, fmt.Errorf("go mod tidy: %w\n  (wago may not be published yet — set WAGO_SRC to a wago checkout to build from source)", err)
			}
			return "", false, fmt.Errorf("go %s: %w", step[0], err)
		}
	}
	_ = os.WriteFile(hashFile, []byte(want), 0o644)
	return bin, false, nil
}

// buildHash keys the built binary on the exact dependency set plus the toolchain.
func buildHash(deps []string) string {
	sorted := append([]string(nil), deps...)
	sort.Strings(sorted)
	h := sha256.New()
	fmt.Fprintf(h, "wago-build\x00%s\x00%s\x00%s/%s\x00", versionString(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
	for _, d := range sorted {
		fmt.Fprintf(h, "%s\x00", d)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// wagoSourceDir returns a local wago checkout to build against, if one is
// available (WAGO_SRC, or running inside the wago module). When false, wago is
// taken from the module proxy instead — the published-install path.
func wagoSourceDir() (string, bool) {
	d, err := wagoModuleDir()
	if err != nil {
		return "", false
	}
	return d, true
}

// wagoModuleDir locates the wago source to build against. Uses WAGO_SRC if set,
// else the current Go module when that is github.com/wago-org/wago.
func wagoModuleDir() (string, error) {
	if d := os.Getenv("WAGO_SRC"); d != "" {
		return d, nil
	}
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("locating wago source: %w (set WAGO_SRC to the wago checkout)", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		return "", fmt.Errorf("not inside a Go module; set WAGO_SRC to the wago checkout")
	}
	if b, err := os.ReadFile(gomod); err != nil || !strings.Contains(string(b), "module github.com/wago-org/wago") {
		return "", fmt.Errorf("current module is not github.com/wago-org/wago; set WAGO_SRC to the wago checkout")
	}
	return filepath.Dir(gomod), nil
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
