package registry

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
)

type publishProvider struct {
	ImportPath       string                `json:"importPath"`
	Definition       wago.PluginDefinition `json:"definition"`
	DefinitionDigest string                `json:"definitionDigest"`
}

func registryPublish(options PublishRequest) { registryPublishContext(context.Background(), options) }

func registryPublishContext(ctx context.Context, options PublishRequest) {
	if err := automation.RequireOnline("plugin publication"); err != nil {
		fatal("publish: %v", err)
	}
	token := resolveToken()
	if token == "" {
		fatal("publish: not logged in (run: wago auth login)")
	}
	manifestPath := options.Manifest
	if manifestPath == "" {
		manifestPath = project.File
	}
	manifest, metadata, err := readPublishManifest(manifestPath)
	if err != nil {
		fatal("publish: %v", err)
	}
	moduleRoot := filepath.Dir(manifestPath)
	version := strings.TrimSpace(metadata.Version)
	if version == "" {
		version = strings.TrimSpace(gitOutputAt(moduleRoot, "describe", "--tags", "--abbrev=0"))
	}
	if version == "" {
		fatal("publish: no version; set package.version or tag the repository")
	}
	version = canonicalGoVersion(version)
	catalog, err := buildProviderCatalog(ctx, manifestPath, metadata)
	if err != nil {
		fatal("publish: local provider catalog: %v", err)
	}
	if strings.TrimPrefix(catalog.version, "v") != strings.TrimPrefix(version, "v") {
		fatal("publish: local provider version %s does not match release %s", catalog.version, version)
	}
	if err := checkProviderCatalogCurrent(catalog); err != nil {
		fatal("publish: %v", err)
	}
	commit, err := resolvePublishTag(ctx, moduleRoot, manifestPath, version, defaultPublishTagUI())
	if errors.Is(err, errPublishCancelled) {
		return
	}
	if err != nil {
		fatal("publish: %v", err)
	}
	if !fullGitCommit(commit) {
		fatal("publish: commit must be the full 40-character commit for tag %s", version)
	}
	download, err := downloadModuleSource(ctx, metadata.Module, version)
	if err != nil {
		fatal("publish: source artifact: %v", err)
	}
	artifactManifestRaw, err := readRegularFile(filepath.Join(download.Dir, project.File), providerCatalogFileLimit)
	if err != nil {
		fatal("publish: exact source artifact %s: %v", project.File, err)
	}
	artifactManifest, artifactMetadata, _, err := parsePublishManifest(artifactManifestRaw)
	if err != nil {
		fatal("publish: exact source artifact %s: %v", project.File, err)
	}
	if err := comparePublishedPackageManifest(manifest, artifactManifest); err != nil {
		fatal("publish: exact source artifact %s: %v", project.File, err)
	}
	if artifactMetadata.Module != metadata.Module {
		fatal("publish: exact source artifact module is %s, want %s", artifactMetadata.Module, metadata.Module)
	}
	artifactCatalog, err := readRegularFile(filepath.Join(download.Dir, wago.ProviderCatalogFile), providerCatalogFileLimit)
	if err != nil {
		fatal("publish: exact source artifact %s: %v", wago.ProviderCatalogFile, err)
	}
	if !bytes.Equal(artifactCatalog, catalog.generated) {
		fatal("publish: exact source artifact %s does not match the local generated catalog", wago.ProviderCatalogFile)
	}
	document, err := wago.DecodeProviderCatalog(artifactCatalog)
	if err != nil {
		fatal("publish: exact source artifact %s: %v", wago.ProviderCatalogFile, err)
	}
	providers := providerEntries(document.Providers)
	if err := validatePublishProviders(providers, artifactMetadata, version); err != nil {
		fatal("publish: exact source artifact provider catalog: %v", err)
	}
	body := map[string]any{
		"manifest": artifactManifest, "version": version, "checksum": download.Sum, "providers": providers,
		"commit": commit, "notes": options.Notes, "unpackedKB": UnpackedKB(moduleRoot),
	}
	status, data, err := apiRequestContext(ctx, http.MethodPost, "/api/publish", token, body)
	if err != nil {
		fatal("publish: %v", err)
	}
	switch status {
	case http.StatusOK:
		fmt.Printf("%s Published %s %s with %d provider%s\n", cyan("✓"), bold(metadata.Module), version, len(providers), pluralRegistry(len(providers)))
		fmt.Printf("  %s\n", dim(packageURL(metadata.Module)))
	case http.StatusConflict:
		fatal("publish: version %s is already published", version)
	case http.StatusForbidden:
		fatal("publish: you are not the owner of %s", metadata.Module)
	case http.StatusUnauthorized:
		fatal("publish: not logged in (run: wago auth login)")
	default:
		fatal("publish: %s", apiError(status, data))
	}
}

func unresolvedReleaseTagInstructions(version string) string {
	return fmt.Sprintf(`cannot resolve release tag %[1]s.
The tag must match package.version in wago.json and point to the commit containing the release manifest and wago.providers.json.

If %[1]s already exists remotely:
  git fetch origin tag %[1]s

If this is a new release, first commit the exact release files, then run:
  git tag %[1]s
  git push origin HEAD %[1]s

Then retry:
  wago plugin publish`, version)
}

type publishMetadata struct {
	Module, Version, Repository, Homepage, License string
	Authors                                        []string
	Definitions                                    map[string]publishDefinitionMetadata
}

type publishDefinitionMetadata struct {
	Name, Description string
	Stability         wago.Stability
	Engines           map[string]string
	Platforms         []string
}

func parsePublishManifest(raw []byte) (map[string]any, publishMetadata, []string, error) {
	var manifest map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, publishMetadata{}, nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, publishMetadata{}, nil, fmt.Errorf("multiple JSON values are not allowed")
		}
		return nil, publishMetadata{}, nil, err
	}
	if err := project.ValidateManifest(manifest); err != nil {
		return nil, publishMetadata{}, nil, err
	}
	if schema, _ := manifest["$schema"].(string); schema != project.SchemaURI {
		return nil, publishMetadata{}, nil, fmt.Errorf("$schema must be %q", project.SchemaURI)
	}
	pkg, ok := manifest["package"].(map[string]any)
	if !ok {
		return nil, publishMetadata{}, nil, fmt.Errorf("this manifest configures an application, not a publishable plugin; add the required package object to wago.json (see https://docs.wago.sh/reference/configuration)")
	}
	module, _ := pkg["module"].(string)
	if err := project.ValidatePluginID(module); err != nil {
		return nil, publishMetadata{}, nil, err
	}
	version, _ := pkg["version"].(string)
	metadata := publishMetadata{
		Module: module, Version: version,
		Repository: manifestStringValue(pkg, "repository"), Homepage: manifestStringValue(pkg, "homepage"), License: manifestStringValue(pkg, "license"),
		Authors: publishAuthorNames(pkg["authors"]), Definitions: map[string]publishDefinitionMetadata{},
	}
	metadata.Definitions[module] = publishDefinitionFromManifest(pkg, publishDefinitionMetadata{})
	imports := []string{module + "/register"}
	if subpackages, ok := pkg["subpackages"].([]any); ok {
		for index, rawSubpackage := range subpackages {
			subpackage, ok := rawSubpackage.(map[string]any)
			if !ok {
				return nil, publishMetadata{}, nil, fmt.Errorf("package.subpackages[%d] must be an object", index)
			}
			id, _ := subpackage["module"].(string)
			if err := project.ValidatePluginID(id); err != nil {
				return nil, publishMetadata{}, nil, fmt.Errorf("package.subpackages[%d]: %w", index, err)
			}
			if id != module && !strings.HasPrefix(id, module+"/") {
				return nil, publishMetadata{}, nil, fmt.Errorf("subpackage %q must belong to module %q", id, module)
			}
			if _, duplicate := metadata.Definitions[id]; duplicate {
				return nil, publishMetadata{}, nil, fmt.Errorf("duplicate published provider %q", id)
			}
			metadata.Definitions[id] = publishDefinitionFromManifest(subpackage, metadata.Definitions[module])
		}
	}
	return manifest, metadata, imports, nil
}

func validatePublishProviders(providers []publishProvider, metadata publishMetadata, version string) error {
	if len(providers) == 0 || len(providers) > 128 {
		return fmt.Errorf("catalog must contain 1 to 128 providers")
	}
	seen := map[string]bool{}
	for _, provider := range providers {
		definition := provider.Definition
		if provider.ImportPath != metadata.Module+"/register" {
			return fmt.Errorf("provider %q import path must be %q", definition.ID, metadata.Module+"/register")
		}
		expected, ok := metadata.Definitions[definition.ID]
		if !ok {
			return fmt.Errorf("provider %q is not declared by package or package.subpackages", definition.ID)
		}
		if seen[definition.ID] {
			return fmt.Errorf("provider %q is repeated", definition.ID)
		}
		seen[definition.ID] = true
		if strings.TrimPrefix(definition.Version, "v") != strings.TrimPrefix(version, "v") {
			return fmt.Errorf("provider %s version %s does not match release %s", definition.ID, definition.Version, version)
		}
		if definition.Name != expected.Name || definition.Description != expected.Description || definition.Stability != expected.Stability {
			return fmt.Errorf("provider %q name, description, and stability must match wago.json", definition.ID)
		}
		if !sameStringMap(definition.Compatibility.Engines, expected.Engines) || !sameStrings(definition.Compatibility.Platforms, expected.Platforms) {
			return fmt.Errorf("provider %q compatibility must match wago.json", definition.ID)
		}
		if definition.Provenance.Repository != metadata.Repository || definition.Provenance.Homepage != metadata.Homepage || definition.Provenance.License != metadata.License || !sameStrings(definition.Provenance.Authors, metadata.Authors) {
			return fmt.Errorf("provider %q provenance must match wago.json", definition.ID)
		}
		digest, err := wago.DefinitionDigest(definition)
		if err != nil {
			return fmt.Errorf("provider %q definition: %w", definition.ID, err)
		}
		if digest != provider.DefinitionDigest {
			return fmt.Errorf("provider %q definition digest changed while publishing", definition.ID)
		}
	}
	for id := range metadata.Definitions {
		if !seen[id] {
			return fmt.Errorf("wago.json declares provider %q but register.Providers omitted it", id)
		}
	}
	return nil
}

func publishDefinitionFromManifest(object map[string]any, inherited publishDefinitionMetadata) publishDefinitionMetadata {
	definition := inherited
	definition.Name = manifestStringValue(object, "name")
	definition.Description = manifestStringValue(object, "description")
	if value := manifestStringValue(object, "stability"); value != "" {
		definition.Stability = wago.Stability(value)
	}
	if raw, ok := object["engines"].(map[string]any); ok {
		definition.Engines = make(map[string]string, len(raw))
		for name, value := range raw {
			definition.Engines[name], _ = value.(string)
		}
	}
	if raw, ok := object["platforms"].([]any); ok {
		definition.Platforms = make([]string, 0, len(raw))
		for _, value := range raw {
			text, _ := value.(string)
			definition.Platforms = append(definition.Platforms, text)
		}
	}
	return definition
}

func publishAuthorNames(raw any) []string {
	authors, _ := raw.([]any)
	names := make([]string, 0, len(authors))
	for _, rawAuthor := range authors {
		author, _ := rawAuthor.(map[string]any)
		names = append(names, manifestStringValue(author, "name"))
	}
	return names
}

func manifestStringValue(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func sameStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type downloadedModule struct {
	Path, Version, Sum, Dir string
}

func downloadModuleSource(ctx context.Context, module, version string) (downloadedModule, error) {
	version = canonicalGoVersion(version)
	dir, err := os.MkdirTemp("", "wago-publish-source-*")
	if err != nil {
		return downloadedModule{}, err
	}
	defer os.RemoveAll(dir)
	command := exec.CommandContext(ctx, "go", "mod", "download", "-json", module+"@"+version)
	command.Dir = dir
	command.Env = isolatedGoEnvironment(os.Environ())
	automation.ConfigureCommand(command)
	output, err := command.CombinedOutput()
	if err != nil {
		return downloadedModule{}, fmt.Errorf("download %s@%s: %w: %s", module, version, err, strings.TrimSpace(string(output)))
	}
	var report struct{ Path, Version, Sum, Dir, Error string }
	if err := json.Unmarshal(output, &report); err != nil {
		return downloadedModule{}, err
	}
	if report.Error != "" {
		return downloadedModule{}, fmt.Errorf("download %s@%s: %s", module, version, report.Error)
	}
	if report.Path != module || report.Version != version || !strings.HasPrefix(report.Sum, "h1:") || report.Dir == "" {
		return downloadedModule{}, fmt.Errorf("download returned source %s@%s checksum %q directory %q", report.Path, report.Version, report.Sum, report.Dir)
	}
	return downloadedModule{Path: report.Path, Version: report.Version, Sum: report.Sum, Dir: report.Dir}, nil
}

func comparePublishedPackageManifest(local, artifact map[string]any) error {
	localPackage, _ := local["package"].(map[string]any)
	artifactPackage, _ := artifact["package"].(map[string]any)
	localJSON, err := json.Marshal(localPackage)
	if err != nil {
		return err
	}
	artifactJSON, err := json.Marshal(artifactPackage)
	if err != nil {
		return err
	}
	if !bytes.Equal(localJSON, artifactJSON) {
		return fmt.Errorf("manifest.package does not match the local manifest")
	}
	return nil
}

func canonicalGoVersion(version string) string {
	version = strings.TrimSpace(version)
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

func fullGitCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func isolatedGoEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, "GOWORK=") {
			result = append(result, entry)
		}
	}
	return append(result, "GOWORK=off")
}

func pluralRegistry(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func registryUnpublish(options UnpublishRequest) {
	registryUnpublishContext(context.Background(), options)
}

func registryUnpublishContext(ctx context.Context, options UnpublishRequest) {
	yes := options.Yes
	token := resolveToken()
	if token == "" {
		fatal("unpublish: not logged in (run: wago auth login)")
	}
	name, ver := splitVersion(options.Target)
	target := name
	if ver != "" {
		target = name + "@" + ver
	}
	if !yes && !confirm(fmt.Sprintf("Unpublish %s? This cannot be undone.", target)) {
		fmt.Println("aborted")
		return
	}
	path := "/api/packages/" + url.PathEscape(name)
	if ver != "" {
		path += "/versions/" + url.PathEscape(ver)
	}
	status, data, err := apiRequestContext(ctx, http.MethodDelete, path, token, nil)
	if err != nil {
		fatal("unpublish: %v", err)
	}
	switch status {
	case http.StatusOK:
		fmt.Printf("%s Unpublished %s\n", cyan("✓"), target)
	case http.StatusForbidden:
		fatal("unpublish: you are not the owner of %s", name)
	case http.StatusNotFound:
		fatal("unpublish: %s not found", target)
	case http.StatusUnauthorized:
		fatal("unpublish: not logged in (run: wago auth login)")
	default:
		fatal("unpublish: %s", apiError(status, data))
	}
}

func registryDeprecate(options DeprecateRequest) {
	registryDeprecateContext(context.Background(), options)
}

func registryDeprecateContext(ctx context.Context, options DeprecateRequest) {
	undo, message := options.Undo, options.Message
	token := resolveToken()
	if token == "" {
		fatal("deprecate: not logged in (run: wago auth login)")
	}
	name, ver := splitVersion(options.Target)
	target := name
	if ver != "" {
		target = name + "@" + ver
	}
	body := map[string]any{"message": message, "version": ver, "undo": undo}
	path := "/api/packages/" + url.PathEscape(name) + "/deprecate"
	status, data, err := apiRequestContext(ctx, http.MethodPost, path, token, body)
	if err != nil {
		fatal("deprecate: %v", err)
	}
	switch status {
	case http.StatusOK:
		verb := "Deprecated"
		if undo {
			verb = "Un-deprecated"
		}
		fmt.Printf("%s %s %s\n", cyan("✓"), verb, target)
	case http.StatusForbidden:
		fatal("deprecate: you are not the owner of %s", name)
	case http.StatusNotFound:
		fatal("deprecate: %s not found", target)
	case http.StatusUnauthorized:
		fatal("deprecate: not logged in (run: wago auth login)")
	default:
		fatal("deprecate: %s", apiError(status, data))
	}
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
