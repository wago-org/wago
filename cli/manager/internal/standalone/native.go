package standalone

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// prepareNativeRuntime copies the selected runtime sources into the private build
// directory and adds the generated initializer in its owning package. Neither
// module-cache sources nor the user's checkout are modified.
func prepareNativeRuntime(buildDir string, environment []string) (string, error) {
	command := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/wago-org/wago")
	command.Dir = buildDir
	command.Env = environment
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("locate standalone runtime: %w\n%s", err, diagnostic.Bytes())
	}
	source := strings.TrimSpace(string(output))
	if source == "" {
		return "", fmt.Errorf("standalone runtime source directory is empty")
	}
	target := filepath.Join(buildDir, "native-runtime")
	if err := copyRuntimeSources(source, target); err != nil {
		return "", err
	}
	if err := runGo(buildDir, environment, false, "mod", "edit", "-replace=github.com/wago-org/wago="+target); err != nil {
		return "", err
	}
	return target, nil
}

func linkNativeModule(target string, generated map[string][]byte) error {
	for name, body := range generated {
		if name != "standalone_module.go" && name != "standalone_dispatcher.go" {
			return fmt.Errorf("unexpected generated standalone source %q", name)
		}
		if err := os.WriteFile(filepath.Join(target, "src/wago", name), body, 0644); err != nil {
			return err
		}
	}
	facade := []byte("package wago\nimport impl \"github.com/wago-org/wago/src/wago\"\nfunc StandaloneCompiled() *Compiled { return impl.StandaloneCompiled() }\n")
	if err := os.WriteFile(filepath.Join(target, "standalone_module.go"), facade, 0644); err != nil {
		return err
	}
	return nil
}

func copyRuntimeSources(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		name := entry.Name()
		if relative != "." && (strings.HasPrefix(name, ".") || name == "bench" || name == "tests" || name == "corpus" || name == "docs" || name == "examples" || name == "node_modules" || name == "vendor") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0755)
		}
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		switch filepath.Ext(name) {
		case ".go", ".s", ".S", ".h", ".c", ".ll", ".syso", ".mod", ".sum":
		default:
			if name != "LICENSE" {
				return nil
			}
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported runtime source file %s", relative)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
