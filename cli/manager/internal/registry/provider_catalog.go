package registry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/atomicfile"
)

const providerCatalogFileLimit = 2 << 20

func registryCatalogContext(ctx context.Context, request CatalogRequest) {
	if err := generateProviderCatalog(ctx, request); err != nil {
		fatal("catalog: %v", err)
	}
}

func generateProviderCatalog(ctx context.Context, request CatalogRequest) error {
	manifestPath := request.Manifest
	if manifestPath == "" {
		manifestPath = project.File
	}
	_, metadata, err := readPublishManifest(manifestPath)
	if err != nil {
		return err
	}
	generated, providers, err := generateLocalProviderCatalog(ctx, filepath.Dir(manifestPath), metadata)
	if err != nil {
		return err
	}
	version, err := catalogVersion(metadata.Version, providers)
	if err != nil {
		return err
	}
	if err := validatePublishProviders(providers, metadata, version); err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(manifestPath), wago.ProviderCatalogFile)
	if request.Check {
		committed, err := readRegularFile(path, providerCatalogFileLimit)
		if err != nil {
			return fmt.Errorf("%s is missing or unreadable: %v (run: wago plugin catalog)", path, err)
		}
		if !bytes.Equal(committed, generated) {
			return fmt.Errorf("%s is stale (run: wago plugin catalog)", path)
		}
		fmt.Printf("%s %s is current (%d provider%s)\n", cyan("✓"), path, len(providers), pluralRegistry(len(providers)))
		return nil
	}
	if err := atomicfile.ReplaceFile(path, atomicfile.Options{Mode: 0o644}, func(writer io.Writer) error {
		_, err := writer.Write(generated)
		return err
	}); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("%s Wrote %s with %d provider%s\n", cyan("✓"), path, len(providers), pluralRegistry(len(providers)))
	return nil
}

func readPublishManifest(path string) (map[string]any, publishMetadata, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, publishMetadata{}, fmt.Errorf("reading manifest: %w", err)
	}
	manifest, metadata, _, err := parsePublishManifest(raw)
	if err != nil {
		return nil, publishMetadata{}, fmt.Errorf("%s: %w", path, err)
	}
	return manifest, metadata, nil
}

func catalogVersion(declared string, providers []publishProvider) (string, error) {
	if version := strings.TrimSpace(declared); version != "" {
		return version, nil
	}
	if len(providers) == 0 {
		return "", errors.New("provider catalog returned no providers")
	}
	version := providers[0].Definition.Version
	for _, provider := range providers[1:] {
		if strings.TrimPrefix(provider.Definition.Version, "v") != strings.TrimPrefix(version, "v") {
			return "", fmt.Errorf("provider versions disagree: %s uses %s while %s uses %s", providers[0].Definition.ID, version, provider.Definition.ID, provider.Definition.Version)
		}
	}
	return version, nil
}

func generateLocalProviderCatalog(ctx context.Context, moduleRoot string, metadata publishMetadata) ([]byte, []publishProvider, error) {
	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		return nil, nil, err
	}
	module, err := modulePathFromGoMod(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, nil, err
	}
	if module != metadata.Module {
		return nil, nil, fmt.Errorf("go.mod module is %q, but wago.json package.module is %q", module, metadata.Module)
	}
	workspace, err := os.MkdirTemp("", "wago-provider-catalog-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(workspace)
	outputPath := filepath.Join(workspace, "catalog.json")
	source := fmt.Sprintf(`package main

import (
	"fmt"
	"os"

	wago "github.com/wago-org/wago"
	providers %s
)

func main() {
	data, err := wago.EncodeProviderCatalog(%s, providers.Providers())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[1], data, 0600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`, strconv.Quote(metadata.Module+"/register"), strconv.Quote(metadata.Module+"/register"))
	runnerDir, err := os.MkdirTemp(root, ".wago-provider-catalog-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(runnerDir)
	runnerPath := filepath.Join(runnerDir, "main.go")
	if err := os.WriteFile(runnerPath, []byte(source), 0o600); err != nil {
		return nil, nil, err
	}
	command := exec.CommandContext(ctx, "go", "run", "-mod=mod", runnerPath, outputPath)
	command.Dir = root
	command.Env = appendEnvironment(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	automation.ConfigureCommand(command)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("run %s/register catalog: %w: %s", metadata.Module, err, strings.TrimSpace(string(output)))
	}
	generated, err := readRegularFile(outputPath, providerCatalogFileLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("read generated provider catalog: %w", err)
	}
	document, err := wago.DecodeProviderCatalog(generated)
	if err != nil {
		return nil, nil, fmt.Errorf("decode generated provider catalog: %w", err)
	}
	providers := providerEntries(document.Providers)
	return generated, providers, nil
}

func providerEntries(entries []wago.ProviderCatalogEntry) []publishProvider {
	providers := make([]publishProvider, len(entries))
	for index, entry := range entries {
		providers[index] = publishProvider(entry)
	}
	return providers
}

func appendEnvironment(environment []string, values ...string) []string {
	names := make(map[string]struct{}, len(values))
	for _, value := range values {
		name, _, _ := strings.Cut(value, "=")
		names[name] = struct{}{}
	}
	result := make([]string, 0, len(environment)+len(values))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := names[name]; !replaced {
			result = append(result, entry)
		}
	}
	return append(result, values...)
}

func modulePathFromGoMod(path string) (string, error) {
	raw, err := readRegularFile(path, 1<<20)
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") || strings.HasPrefix(line, "module\t") {
			module := strings.TrimSpace(strings.TrimPrefix(line, "module"))
			if strings.HasPrefix(module, "\"") || strings.HasPrefix(module, "`") {
				unquoted, err := strconv.Unquote(module)
				if err != nil {
					return "", fmt.Errorf("%s has invalid quoted module directive: %w", path, err)
				}
				module = unquoted
			}
			if module == "" {
				break
			}
			return module, nil
		}
	}
	return "", fmt.Errorf("%s has no module directive", path)
}

func readRegularFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, limit)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, limit)
	}
	return data, nil
}
