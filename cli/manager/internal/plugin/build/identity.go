package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	resolvedBuildIdentityVersion = 1
	maxBuildIdentityFiles        = 100_000
	maxBuildIdentityBytes        = 512 << 20
)

var buildEnvironmentKeys = []string{
	"AR", "CC", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_ENABLED", "CGO_FFLAGS", "CGO_LDFLAGS", "CXX", "FC",
	"GO386", "GOAMD64", "GOARCH", "GOARM", "GOARM64", "GODEBUG", "GOEXPERIMENT", "GOFLAGS", "GOFIPS140", "GOHOSTARCH", "GOHOSTOS",
	"GOMIPS", "GOMIPS64", "GOOS", "GOPPC64", "GORISCV64", "GOROOT", "GOTOOLCHAIN", "GOVERSION", "GOWASM", "PKG_CONFIG",
}

var externalInputBuildFlags = []string{"-modfile", "-overlay", "-pkgdir", "-toolexec"}

type resolvedModuleIdentity struct {
	Path      string                  `json:"path"`
	Version   string                  `json:"version,omitempty"`
	Sum       string                  `json:"sum,omitempty"`
	GoModSum  string                  `json:"goModSum,omitempty"`
	GoVersion string                  `json:"goVersion,omitempty"`
	Main      bool                    `json:"main,omitempty"`
	Indirect  bool                    `json:"indirect,omitempty"`
	Replace   *resolvedModuleIdentity `json:"replace,omitempty"`
	Dir       string                  `json:"dir,omitempty"`
}

type listedBuildPackage struct {
	ImportPath   string
	Dir          string
	GoFiles      []string
	CgoFiles     []string
	CFiles       []string
	CXXFiles     []string
	MFiles       []string
	HFiles       []string
	FFiles       []string
	SFiles       []string
	SwigFiles    []string
	SwigCXXFiles []string
	SysoFiles    []string
	EmbedFiles   []string
}

type selectedBuildFile struct {
	ImportPath string
	Kind       string
	Path       string
}

// resolvedBuildHash describes the inputs that the go command will use after
// module resolution. cacheable is false when a selected source file cannot be
// fingerprinted within the fixed work limits; callers must build in that case.
func resolvedBuildHash(dir string, input Input, config Config) (digest string, cacheable bool, err error) {
	h := sha256.New()
	fmt.Fprintf(h, "wago-resolved-build\x00%d\x00%s\x00", resolvedBuildIdentityVersion, Hash(input, config))
	generatedMain, err := renderMain(input, config, "normalized-build-identity")
	if err != nil {
		return "", false, err
	}
	generatedMainHash := sha256.Sum256(generatedMain)
	fmt.Fprintf(h, "generated-main\x00%x\x00go-build\x00-buildvcs=false\x00", generatedMainHash)

	environment, err := selectedBuildEnvironment(dir)
	if err != nil {
		return "", false, err
	}
	for _, key := range buildEnvironmentKeys {
		fmt.Fprintf(h, "env\x00%s\x00%s\x00", key, environment[key])
	}
	cacheable = true
	for _, flag := range externalInputBuildFlags {
		if strings.Contains(environment["GOFLAGS"], flag) {
			cacheable = false
			fmt.Fprintf(h, "external-goflag\x00%s\x00", flag)
		}
	}

	modules, err := selectedModules(dir)
	if err != nil {
		return "", false, err
	}
	for _, module := range modules {
		encoded, err := json.Marshal(module)
		if err != nil {
			return "", false, fmt.Errorf("encode selected module %q: %w", module.Path, err)
		}
		fmt.Fprintf(h, "module\x00%s\x00", encoded)
	}

	files, err := selectedBuildFiles(dir, config.BuildTag)
	if err != nil {
		return "", false, err
	}
	cacheable = cacheable && len(files) <= maxBuildIdentityFiles
	for _, file := range files {
		switch file.Kind {
		case "cgo", "swig", "swig-cxx":
			// External compilers and headers can change without changing Go's
			// module graph. Build them, but never reuse their executable key.
			cacheable = false
		}
	}
	var totalBytes int64
	for _, file := range files {
		fmt.Fprintf(h, "file\x00%s\x00%s\x00%s\x00", file.ImportPath, file.Kind, filepath.ToSlash(file.Path))
		info, statErr := os.Lstat(file.Path)
		if statErr != nil {
			cacheable = false
			fmt.Fprintf(h, "stat-error\x00%v\x00", statErr)
			continue
		}
		fmt.Fprintf(h, "mode\x00%s\x00size\x00%d\x00", info.Mode().Type(), info.Size())
		if info.Mode()&os.ModeSymlink != 0 {
			target, linkErr := os.Readlink(file.Path)
			if linkErr != nil {
				cacheable = false
				fmt.Fprintf(h, "link-error\x00%v\x00", linkErr)
				continue
			}
			fmt.Fprintf(h, "link\x00%s\x00", target)
			info, statErr = os.Stat(file.Path)
			if statErr != nil {
				cacheable = false
				fmt.Fprintf(h, "target-stat-error\x00%v\x00", statErr)
				continue
			}
		}
		if !info.Mode().IsRegular() {
			cacheable = false
			fmt.Fprint(h, "not-regular\x00")
			continue
		}
		if info.Size() < 0 || info.Size() > maxBuildIdentityBytes-totalBytes || len(files) > maxBuildIdentityFiles {
			cacheable = false
			fmt.Fprint(h, "fingerprint-limit\x00")
			continue
		}
		totalBytes += info.Size()
		contentHash, readErr := hashBuildFile(file.Path)
		if readErr != nil {
			cacheable = false
			fmt.Fprintf(h, "read-error\x00%v\x00", readErr)
			continue
		}
		fmt.Fprintf(h, "sha256\x00%s\x00", contentHash)
	}

	for _, name := range []string{"go.mod", "go.sum"} {
		path := filepath.Join(dir, name)
		data, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) && name == "go.sum" {
			fmt.Fprintf(h, "%s\x00absent\x00", name)
			continue
		}
		if readErr != nil {
			return "", false, fmt.Errorf("read resolved %s: %w", name, readErr)
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "%s\x00%x\x00", name, sum)
	}
	return hex.EncodeToString(h.Sum(nil)), cacheable, nil
}

func selectedBuildEnvironment(dir string) (map[string]string, error) {
	output, err := buildGoOutput(dir, "env", "-json")
	if err != nil {
		return nil, err
	}
	all := map[string]any{}
	if err := json.Unmarshal(output, &all); err != nil {
		return nil, fmt.Errorf("decode go build environment: %w", err)
	}
	selected := make(map[string]string, len(buildEnvironmentKeys))
	for _, key := range buildEnvironmentKeys {
		if value, ok := all[key]; ok {
			selected[key] = fmt.Sprint(value)
		} else {
			selected[key] = os.Getenv(key)
		}
	}
	return selected, nil
}

func selectedModules(dir string) ([]resolvedModuleIdentity, error) {
	var modules []resolvedModuleIdentity
	err := decodeBuildGoJSON(dir, []string{"list", "-m", "-json", "all"}, func(decoder *json.Decoder) error {
		for {
			var module resolvedModuleIdentity
			if err := decoder.Decode(&module); err != nil {
				if err == io.EOF {
					return nil
				}
				return fmt.Errorf("decode selected module graph: %w", err)
			}
			modules = append(modules, module)
		}
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(modules, func(i, j int) bool {
		if modules[i].Path == modules[j].Path {
			return modules[i].Version < modules[j].Version
		}
		return modules[i].Path < modules[j].Path
	})
	return modules, nil
}

func selectedBuildFiles(dir, buildTag string) ([]selectedBuildFile, error) {
	args := []string{"list", "-deps", "-json"}
	if buildTag != "" {
		args = append(args, "-tags", buildTag)
	}
	args = append(args, ".")
	generatedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	generatedMain := filepath.Join(generatedDir, "main.go")
	var files []selectedBuildFile
	err = decodeBuildGoJSON(dir, args, func(decoder *json.Decoder) error {
		for {
			var pkg listedBuildPackage
			if err := decoder.Decode(&pkg); err != nil {
				if err == io.EOF {
					return nil
				}
				return fmt.Errorf("decode selected package graph: %w", err)
			}
			groups := []struct {
				kind  string
				files []string
			}{
				{"go", pkg.GoFiles}, {"cgo", pkg.CgoFiles}, {"c", pkg.CFiles}, {"cxx", pkg.CXXFiles},
				{"objc", pkg.MFiles}, {"header", pkg.HFiles}, {"fortran", pkg.FFiles}, {"asm", pkg.SFiles},
				{"swig", pkg.SwigFiles}, {"swig-cxx", pkg.SwigCXXFiles}, {"syso", pkg.SysoFiles}, {"embed", pkg.EmbedFiles},
			}
			packageDir, err := filepath.EvalSymlinks(pkg.Dir)
			if err != nil {
				return err
			}
			for _, group := range groups {
				for _, name := range group.files {
					path := name
					if !filepath.IsAbs(path) {
						path = filepath.Join(packageDir, filepath.FromSlash(path))
					}
					path, err = filepath.Abs(path)
					if err != nil {
						return err
					}
					if filepath.Clean(path) == filepath.Clean(generatedMain) {
						continue
					}
					if len(files) <= maxBuildIdentityFiles {
						files = append(files, selectedBuildFile{ImportPath: pkg.ImportPath, Kind: group.kind, Path: filepath.Clean(path)})
					}
				}
			}
		}
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].ImportPath != files[j].ImportPath {
			return files[i].ImportPath < files[j].ImportPath
		}
		if files[i].Kind != files[j].Kind {
			return files[i].Kind < files[j].Kind
		}
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func hashBuildFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, copyErr := io.Copy(h, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func buildGoOutput(dir string, args ...string) ([]byte, error) {
	command := exec.Command("go", args...)
	command.Dir = dir
	configureGeneratedModuleGoCommand(command)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, message)
		}
		return nil, fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return output, nil
}

func decodeBuildGoJSON(dir string, args []string, decode func(*json.Decoder) error) error {
	command := exec.Command("go", args...)
	command.Dir = dir
	configureGeneratedModuleGoCommand(command)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	decodeErr := decode(json.NewDecoder(stdout))
	if decodeErr != nil {
		_ = stdout.Close()
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if decodeErr != nil {
		return decodeErr
	}
	if waitErr != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), waitErr, message)
		}
		return fmt.Errorf("go %s: %w", strings.Join(args, " "), waitErr)
	}
	return nil
}
