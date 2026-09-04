package project

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/wago-org/wago/internal/jsonstrict"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/src/core/semver"
)

const (
	File      = "wago.json"
	SchemaURI = "https://wago.sh/v1/schema.json"
)

func Path(dir string) string { return filepath.Join(dir, File) }

func DisplayPath(dir string) string {
	path, err := filepath.Abs(Path(dir))
	if err != nil {
		path = Path(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	path, home = filepath.Clean(path), filepath.Clean(home)
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// Read loads wago.json as a generic map so updates preserve fields owned by
// publishers and future schema versions.
func Read(dir string) (map[string]any, error) {
	var manifest map[string]any
	err := withMetadataRead(dir, func(mutation *Mutation) error {
		var err error
		manifest, err = mutation.ReadManifest()
		return err
	})
	return manifest, err
}

func readManifest(dir string) (map[string]any, error) {
	data, err := os.ReadFile(Path(dir))
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeManifest(data, dir)
}

func decodeManifest(data []byte, dir string) (map[string]any, error) {
	if err := jsonstrict.ValidateUniqueJSON(data); err != nil {
		return nil, fmt.Errorf("%s: %w", DisplayPath(dir), err)
	}
	manifest := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", DisplayPath(dir), err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("%s: %w", DisplayPath(dir), err)
	}
	if err := ValidateManifest(manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", DisplayPath(dir), err)
	}
	return manifest, nil
}

func Write(dir string, manifest map[string]any) error {
	if automation.Locked() {
		return fmt.Errorf("locked mode prevents changing %s", DisplayPath(dir))
	}
	return WithMutation(context.Background(), dir, func(mutation *Mutation) error {
		return mutation.PublishManifest(manifest)
	})
}

func EncodeManifest(manifest map[string]any) ([]byte, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest must be an object")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	var normalized map[string]any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	if err := ValidateManifest(normalized); err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

var (
	manifestSlugPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	manifestPlatformPattern = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9]+$`)
	manifestGitHubPattern   = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
)

var manifestFeatureNames = stringSet(
	"bulk-memory-operations", "exception-handling", "extended-const-expressions",
	"extended-constant-expressions", "gc", "memory64", "multi-memory", "multi-value",
	"mutable-global", "nontrapping-float-to-int-conversion", "reference-types",
	"sign-extension-ops", "simd", "table64", "tail-call", "threads",
	"typed-function-references",
)

var manifestOptimizationNames = stringSet(
	"assoc-tree", "bmi2-rorx", "bounds-facts", "branch-fold",
	"commute-self-update", "compact-i32-frame",
	"dead-gc-new", "entry-arg-pins", "entry-init-elision", "ext-fp-pins",
	"frame-elide", "frame-elide-reghomed", "gc-native-alloc",
	"immutable-table", "immutable-table-type", "inline",
	"inline-callfree", "interval-region-pins", "leaf-scratch-pins",
	"magic-div",
	"mul-add-fuse", "multi-bounds-cert",
	"olddest-rhs-sink", "reg-abi", "reg-merge", "small-frame", "st-flags",
	"shared-adapters", "shared-trap-body", "simd-superopt", "stack-fence", "stack-reg",
	"store-forward", "store-load-fwd", "store8-flags", "tee-sink",
	"three-op-sink", "tree-order", "unary-sink", "uxtw-add",
	"v128-const-cache", "v128-direct-results", "v128-pins",
	"vex-float-mem", "x8-pin", "zero-branch",
)

var retiredManifestOptimizationNames = stringSet(
	"affine-lea", "call-next-use", "fcmp-fuse", "gc-ref-facts",
	"immutable-poly-fastpath", "legacy-fp-pins", "legacy-gp-pins",
	"loop-precheck", "loop-region-pins", "swar-idioms", "tee-spill-elide",
	"v128-sink",
)

// IsRetiredOptimizationName reports whether name was accepted by the v1
// manifest contract but no longer selects compiler behavior. Retired names are
// accepted as no-ops so existing projects remain readable under the same v1
// schema URI.
func IsRetiredOptimizationName(name string) bool {
	_, ok := retiredManifestOptimizationNames[name]
	return ok
}

// RetiredOptimizationNames returns the v1 compatibility names in stable order.
func RetiredOptimizationNames() []string {
	names := make([]string, 0, len(retiredManifestOptimizationNames))
	for name := range retiredManifestOptimizationNames {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidateManifest enforces the checked-in v1 schema before any project read
// or transaction. The manifest remains a generic map so unrelated v1 fields
// survive plugin updates, but unknown or malformed fields fail closed.
func ValidateManifest(manifest map[string]any) error {
	if manifest == nil {
		return fmt.Errorf("manifest must be an object")
	}
	if err := rejectUnknown(manifest, "manifest", "$schema", "plugins", "settings", "package"); err != nil {
		return err
	}
	if raw, ok := manifest["$schema"]; ok {
		schema, ok := raw.(string)
		if !ok || schema != SchemaURI {
			return fmt.Errorf("$schema must be %q", SchemaURI)
		}
	}
	if _, err := requirementsFromMap(manifest, "."); err != nil {
		return err
	}
	if raw, ok := manifest["settings"]; ok {
		if err := validateManifestSettings(raw); err != nil {
			return err
		}
	}
	if raw, ok := manifest["package"]; ok {
		if err := validateManifestPackage(raw); err != nil {
			return err
		}
	}
	return nil
}

func validateManifestSettings(raw any) error {
	settings, err := manifestObject(raw, "settings")
	if err != nil {
		return err
	}
	if err := rejectUnknown(settings, "settings", "features", "optimizations", "runtime"); err != nil {
		return err
	}
	for _, field := range []struct {
		name    string
		allowed map[string]struct{}
	}{{"features", manifestFeatureNames}, {"optimizations", manifestOptimizationNames}} {
		rawValues, ok := settings[field.name]
		if !ok {
			continue
		}
		values, err := manifestObject(rawValues, "settings."+field.name)
		if err != nil {
			return err
		}
		for name, rawValue := range values {
			_, active := field.allowed[name]
			retired := field.name == "optimizations" && IsRetiredOptimizationName(name)
			if !active && !retired {
				return fmt.Errorf("settings.%s contains unknown setting %q", field.name, name)
			}
			if _, ok := rawValue.(bool); !ok {
				return fmt.Errorf("settings.%s.%s must be a boolean", field.name, name)
			}
		}
	}
	if rawRuntime, ok := settings["runtime"]; ok {
		runtimeSettings, err := manifestObject(rawRuntime, "settings.runtime")
		if err != nil {
			return err
		}
		if err := rejectUnknown(runtimeSettings, "settings.runtime", "parallel", "deferredBoundsChecking"); err != nil {
			return err
		}
		if rawParallel, ok := runtimeSettings["parallel"]; ok {
			parallel, ok := rawParallel.(string)
			if !ok || !validManifestParallel(parallel) {
				return fmt.Errorf("settings.runtime.parallel must be auto or a non-negative integer")
			}
		}
		if rawDeferred, ok := runtimeSettings["deferredBoundsChecking"]; ok {
			if _, ok := rawDeferred.(bool); !ok {
				return fmt.Errorf("settings.runtime.deferredBoundsChecking must be a boolean")
			}
		}
	}
	return nil
}

func validateManifestPackage(raw any) error {
	pkg, err := manifestObject(raw, "package")
	if err != nil {
		return err
	}
	if err := rejectUnknown(pkg, "package", "module", "version", "name", "description", "stability", "license", "repository", "homepage", "category", "tags", "authors", "engines", "platforms", "subpackages"); err != nil {
		return err
	}
	module, err := requiredManifestString(pkg, "package", "module", 300)
	if err != nil {
		return err
	}
	if err := ValidatePluginID(module); err != nil {
		return fmt.Errorf("package.module: %w", err)
	}
	for _, field := range []struct {
		name string
		max  int
	}{{"name", 100}, {"description", 500}, {"license", 100}} {
		if _, err := requiredManifestString(pkg, "package", field.name, field.max); err != nil {
			return err
		}
	}
	if rawVersion, ok := pkg["version"]; ok {
		version, ok := rawVersion.(string)
		if !ok || strings.HasPrefix(version, "V") {
			return fmt.Errorf("package.version must be a semantic version")
		}
		if _, err := semver.Parse(version); err != nil {
			return fmt.Errorf("package.version must be a semantic version: %w", err)
		}
	}
	if err := validateManifestStability(pkg, "package"); err != nil {
		return err
	}
	repository, err := requiredManifestString(pkg, "package", "repository", 0)
	if err != nil {
		return err
	}
	if !validManifestURI(repository, true) {
		return fmt.Errorf("package.repository must be an HTTPS URI")
	}
	if rawHomepage, ok := pkg["homepage"]; ok {
		homepage, ok := rawHomepage.(string)
		if !ok || !validManifestURI(homepage, false) {
			return fmt.Errorf("package.homepage must be a URI")
		}
	}
	if err := validateManifestSlugField(pkg, "package", "category"); err != nil {
		return err
	}
	if err := validateManifestStringList(pkg, "package", "tags", 32, manifestSlugPattern); err != nil {
		return err
	}
	if err := validateManifestAuthors(pkg); err != nil {
		return err
	}
	if err := validateManifestEngines(pkg, "package"); err != nil {
		return err
	}
	if err := validateManifestStringList(pkg, "package", "platforms", 0, manifestPlatformPattern); err != nil {
		return err
	}
	if rawSubpackages, ok := pkg["subpackages"]; ok {
		values, ok := rawSubpackages.([]any)
		if !ok {
			return fmt.Errorf("package.subpackages must be an array")
		}
		seen := map[string]bool{}
		for index, rawSubpackage := range values {
			path := fmt.Sprintf("package.subpackages[%d]", index)
			subpackage, err := manifestObject(rawSubpackage, path)
			if err != nil {
				return err
			}
			if err := rejectUnknown(subpackage, path, "module", "name", "description", "stability", "tags", "engines", "platforms"); err != nil {
				return err
			}
			submodule, err := requiredManifestString(subpackage, path, "module", 300)
			if err != nil {
				return err
			}
			if err := ValidatePluginID(submodule); err != nil {
				return fmt.Errorf("%s.module: %w", path, err)
			}
			if submodule == module || !strings.HasPrefix(submodule, module+"/") {
				return fmt.Errorf("%s.module %q must be below package.module %q", path, submodule, module)
			}
			if seen[submodule] {
				return fmt.Errorf("package.subpackages contains duplicate module %q", submodule)
			}
			seen[submodule] = true
			if _, err := requiredManifestString(subpackage, path, "name", 100); err != nil {
				return err
			}
			if _, err := requiredManifestString(subpackage, path, "description", 500); err != nil {
				return err
			}
			if err := validateManifestStability(subpackage, path); err != nil {
				return err
			}
			if err := validateManifestStringList(subpackage, path, "tags", 32, manifestSlugPattern); err != nil {
				return err
			}
			if err := validateManifestEngines(subpackage, path); err != nil {
				return err
			}
			if err := validateManifestStringList(subpackage, path, "platforms", 0, manifestPlatformPattern); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateManifestAuthors(pkg map[string]any) error {
	raw, ok := pkg["authors"]
	if !ok {
		return fmt.Errorf("package.authors is required")
	}
	authors, ok := raw.([]any)
	if !ok || len(authors) == 0 {
		return fmt.Errorf("package.authors must be a non-empty array")
	}
	seen := map[string]bool{}
	for index, rawAuthor := range authors {
		path := fmt.Sprintf("package.authors[%d]", index)
		author, err := manifestObject(rawAuthor, path)
		if err != nil {
			return err
		}
		if err := rejectUnknown(author, path, "name", "email", "url", "github"); err != nil {
			return err
		}
		if _, err := requiredManifestString(author, path, "name", 120); err != nil {
			return err
		}
		if rawEmail, ok := author["email"]; ok {
			email, ok := rawEmail.(string)
			if !ok || !validManifestEmail(email) {
				return fmt.Errorf("%s.email must be an email address", path)
			}
		}
		if rawURL, ok := author["url"]; ok {
			value, ok := rawURL.(string)
			if !ok || !validManifestURI(value, false) {
				return fmt.Errorf("%s.url must be a URI", path)
			}
		}
		if rawGitHub, ok := author["github"]; ok {
			github, ok := rawGitHub.(string)
			if !ok || !manifestGitHubPattern.MatchString(github) {
				return fmt.Errorf("%s.github must be a GitHub username", path)
			}
		}
		encoded, _ := json.Marshal(author)
		key := string(encoded)
		if seen[key] {
			return fmt.Errorf("package.authors contains a duplicate author")
		}
		seen[key] = true
	}
	return nil
}

func validateManifestEngines(object map[string]any, path string) error {
	raw, ok := object["engines"]
	if !ok {
		return nil
	}
	engines, err := manifestObject(raw, path+".engines")
	if err != nil {
		return err
	}
	for name, rawConstraint := range engines {
		if len(name) > 64 || !manifestSlugPattern.MatchString(name) {
			return fmt.Errorf("%s.engines contains invalid engine %q", path, name)
		}
		constraint, ok := rawConstraint.(string)
		if !ok || ValidateConstraint(constraint) != nil {
			return fmt.Errorf("%s.engines.%s must be a semantic-version range", path, name)
		}
	}
	return nil
}

func validateManifestStringList(object map[string]any, path, field string, max int, pattern *regexp.Regexp) error {
	raw, ok := object[field]
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if !ok || max > 0 && len(values) > max {
		return fmt.Errorf("%s.%s must be an array with at most %d entries", path, field, max)
	}
	seen := map[string]bool{}
	for index, rawValue := range values {
		value, ok := rawValue.(string)
		if !ok || len(value) > 64 || !pattern.MatchString(value) {
			return fmt.Errorf("%s.%s[%d] is invalid", path, field, index)
		}
		if seen[value] {
			return fmt.Errorf("%s.%s contains duplicate %q", path, field, value)
		}
		seen[value] = true
	}
	return nil
}

func validateManifestSlugField(object map[string]any, path, field string) error {
	raw, ok := object[field]
	if !ok {
		return nil
	}
	value, ok := raw.(string)
	if !ok || len(value) > 64 || !manifestSlugPattern.MatchString(value) {
		return fmt.Errorf("%s.%s must be a lowercase slug", path, field)
	}
	return nil
}

func validateManifestStability(object map[string]any, path string) error {
	raw, ok := object["stability"]
	if !ok {
		return nil
	}
	value, ok := raw.(string)
	if !ok || value != "experimental" && value != "stable" && value != "deprecated" {
		return fmt.Errorf("%s.stability must be experimental, stable, or deprecated", path)
	}
	return nil
}

func requiredManifestString(object map[string]any, path, field string, max int) (string, error) {
	raw, ok := object[field]
	if !ok {
		return "", fmt.Errorf("%s.%s is required", path, field)
	}
	value, ok := raw.(string)
	if !ok || strings.TrimSpace(value) == "" || max > 0 && len(value) > max {
		return "", fmt.Errorf("%s.%s must be a non-empty string", path, field)
	}
	return value, nil
}

func manifestObject(raw any, path string) (map[string]any, error) {
	object, ok := raw.(map[string]any)
	if !ok || object == nil {
		return nil, fmt.Errorf("%s must be an object", path)
	}
	return object, nil
}

func rejectUnknown(object map[string]any, path string, allowed ...string) error {
	known := stringSet(allowed...)
	unknown := make([]string, 0)
	for name := range object {
		if _, ok := known[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("%s field %q is not part of the v1 schema", path, unknown[0])
}

func stringSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func validManifestURI(value string, httpsOnly bool) bool {
	if value == "" || value != strings.TrimSpace(value) || strings.Contains(value, "\\") {
		return false
	}
	for _, char := range value {
		if char <= ' ' || char == 0x7f {
			return false
		}
	}
	colon := strings.IndexByte(value, ':')
	if colon <= 0 || !validManifestScheme(value[:colon]) {
		return false
	}
	if !httpsOnly {
		return true
	}
	if value[:colon] != "https" || !strings.HasPrefix(value[colon+1:], "//") {
		return false
	}
	authority := value[colon+3:]
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	if authority == "" || strings.Contains(authority, "@") {
		return false
	}
	return true
}

func validManifestScheme(value string) bool {
	for index, char := range value {
		if index == 0 && (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') {
			return false
		}
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '+' && char != '-' && char != '.' {
			return false
		}
	}
	return true
}

func validManifestEmail(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || strings.Count(value, "@") != 1 {
		return false
	}
	local, domain, _ := strings.Cut(value, "@")
	if local == "" || domain == "" || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	for _, char := range value {
		if char <= ' ' || char == 0x7f {
			return false
		}
	}
	return true
}

func validManifestParallel(value string) bool {
	if value == "auto" {
		return true
	}
	if value == "" {
		return false
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}

func Initialize(dir string) (bool, error) {
	return InitializeWith(dir, nil)
}

// InitializeWith creates or updates a manifest while preserving fields that
// are not owned by the caller. Values replace fields with the same key.
func InitializeWith(dir string, values map[string]any) (bool, error) {
	var created bool
	err := WithMutation(context.Background(), dir, func(mutation *Mutation) error {
		_, statErr := os.Stat(Path(dir))
		created = os.IsNotExist(statErr)
		if statErr != nil && !created {
			return statErr
		}
		manifest, err := mutation.ReadManifest()
		if err != nil {
			return err
		}
		EnsureMetadata(manifest)
		if _, ok := manifest["plugins"]; !ok {
			manifest["plugins"] = map[string]any{}
		}
		for key, value := range values {
			manifest[key] = value
		}
		return mutation.PublishManifest(manifest)
	})
	return created, err
}

func EnsureMetadata(manifest map[string]any) {
	if _, ok := manifest["$schema"]; !ok {
		manifest["$schema"] = SchemaURI
	}
}

// EnsureGitignore appends entry to ./.gitignore if not already present. It is
// best effort and creates the file only inside a Git working tree.
func EnsureGitignore(entry string) {
	const name = ".gitignore"
	data, err := os.ReadFile(name)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			value := strings.TrimSpace(line)
			if value == entry || value == strings.TrimRight(entry, "/") {
				return
			}
		}
		file, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer file.Close()
		if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
			_, _ = file.WriteString("\n")
		}
		_, _ = file.WriteString(entry + "\n")
		return
	}
	if _, err := os.Stat(".git"); err == nil {
		_ = os.WriteFile(name, []byte(entry+"\n"), 0o644)
	}
}
