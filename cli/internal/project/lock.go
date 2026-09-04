package project

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wago-org/wago/internal/jsonstrict"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/src/core/semver"
)

const (
	LockFile          = "wago-lock.json"
	LockFormatVersion = 1
)

const (
	AuthorityRequired = "required"
	AuthorityOptional = "optional"
)

var knownAuthorities = map[string]struct{}{
	"host.import.define":             {},
	"host.caller.identify":           {},
	"host.caller.invoke":             {},
	"host.arguments.read":            {},
	"runtime.close.observe":          {},
	"module.source.transform":        {},
	"module.compile.observe":         {},
	"module.close.observe":           {},
	"instance.instantiate.intercept": {},
	"instance.instantiate.observe":   {},
	"instance.close.observe":         {},
	"instance.invoke.intercept":      {},
	"instance.invoke.observe":        {},
	"instance.manage":                {},
	"core.module.compile":            {},
	"core.instance.instantiate":      {},
	"core.funcref.create":            {},
	"compiler.type.define":           {},
	"compiler.instruction.define":    {},
}

type PluginSource struct {
	Module   string `json:"module"`
	Version  string `json:"version"`
	Checksum string `json:"checksum"`
}

type ProviderSource struct {
	ImportPath string `json:"importPath"`
}

type AuthorityScope struct {
	Modules        []string `json:"modules,omitempty"`
	MaxInstances   uint32   `json:"maxInstances,omitempty"`
	MaxMemoryBytes uint64   `json:"maxMemoryBytes,omitempty"`
}

type AuthorityRequest struct {
	Name   string         `json:"name"`
	Mode   string         `json:"mode"`
	Reason string         `json:"reason"`
	Scope  AuthorityScope `json:"scope"`
}

type AuthorityGrant struct {
	Name  string         `json:"name"`
	Scope AuthorityScope `json:"scope"`
}

type ContractProvider struct {
	ID    string `json:"id"`
	Major uint32 `json:"major"`
}

type ContractRequirement struct {
	ID    string `json:"id"`
	Major uint32 `json:"major"`
	Mode  string `json:"mode"`
}

type ContractSet struct {
	Provides []ContractProvider    `json:"provides"`
	Requires []ContractRequirement `json:"requires"`
}

type ContractBinding struct {
	ID        string   `json:"id"`
	Major     uint32   `json:"major"`
	Providers []string `json:"providers"`
}

// LockEntry is one immutable catalog resolution. Direct marks roots selected
// in wago.json; every reachable transitive resolution is present as another
// entry and linked through Dependencies.
type LockEntry struct {
	Direct               bool               `json:"direct"`
	Source               PluginSource       `json:"source"`
	Provider             ProviderSource     `json:"provider"`
	DefinitionDigest     string             `json:"definitionDigest"`
	ReleaseFingerprint   string             `json:"releaseFingerprint"`
	Dependencies         map[string]string  `json:"dependencies"`
	RequestedAuthorities []AuthorityRequest `json:"requestedAuthorities"`
	Grants               []AuthorityGrant   `json:"grants"`
	Contracts            ContractSet        `json:"contracts"`
	Bindings             []ContractBinding  `json:"bindings"`
	Config               json.RawMessage    `json:"config"`
}

type LockDocument struct {
	FormatVersion int                  `json:"formatVersion"`
	Plugins       map[string]LockEntry `json:"plugins"`
}

func NewLockDocument() LockDocument {
	return LockDocument{FormatVersion: LockFormatVersion, Plugins: map[string]LockEntry{}}
}

func LockPath(dir string) string { return filepath.Join(dir, LockFile) }

func ReadLock(dir string) (LockDocument, error) {
	var document LockDocument
	err := withMetadataRead(dir, func(mutation *Mutation) error {
		var err error
		document, err = mutation.ReadLock()
		return err
	})
	return document, err
}

func readLock(dir string) (LockDocument, error) {
	data, err := os.ReadFile(LockPath(dir))
	if os.IsNotExist(err) {
		return NewLockDocument(), nil
	}
	if err != nil {
		return LockDocument{}, err
	}
	document, err := DecodeLock(data)
	if err != nil {
		return LockDocument{}, fmt.Errorf("%s: %w", displayFilePath(LockPath(dir)), err)
	}
	return document, nil
}

func DecodeLock(data []byte) (LockDocument, error) {
	if err := jsonstrict.ValidateTypedJSON(data, LockDocument{}); err != nil {
		return LockDocument{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document LockDocument
	if err := decoder.Decode(&document); err != nil {
		return LockDocument{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return LockDocument{}, err
	}
	if err := ValidateLock(document); err != nil {
		return LockDocument{}, err
	}
	return document, nil
}

func EncodeLock(document LockDocument) ([]byte, error) {
	if document.FormatVersion == 0 {
		document.FormatVersion = LockFormatVersion
	}
	if document.Plugins == nil {
		document.Plugins = map[string]LockEntry{}
	}
	if err := ValidateLock(document); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func WriteLock(dir string, document LockDocument) error {
	if automation.Locked() {
		return fmt.Errorf("locked mode prevents changing %s", displayFilePath(LockPath(dir)))
	}
	return WithMutation(context.Background(), dir, func(mutation *Mutation) error {
		return mutation.PublishLock(document)
	})
}

func ValidateLock(document LockDocument) error {
	if document.FormatVersion != LockFormatVersion {
		return fmt.Errorf("unsupported lock formatVersion %d; expected %d", document.FormatVersion, LockFormatVersion)
	}
	if document.Plugins == nil {
		return errors.New("plugins must be an object")
	}
	for id, entry := range document.Plugins {
		if err := ValidatePluginID(id); err != nil {
			return err
		}
		if err := validateLockEntry(id, entry); err != nil {
			return fmt.Errorf("plugin %q: %w", id, err)
		}
	}
	if err := validateSharedSourceReleases(document.Plugins); err != nil {
		return err
	}
	for id, entry := range document.Plugins {
		for dependency := range entry.Dependencies {
			if _, ok := document.Plugins[dependency]; !ok {
				return fmt.Errorf("plugin %q: dependency %q has no resolution", id, dependency)
			}
		}
	}
	if err := validateLockGraph(document.Plugins); err != nil {
		return err
	}
	return validateDirectReachability(document.Plugins)
}

func validateSharedSourceReleases(plugins map[string]LockEntry) error {
	type selectedRelease struct {
		id          string
		source      PluginSource
		provider    ProviderSource
		fingerprint string
	}
	selected := make(map[string]selectedRelease, len(plugins))
	for _, id := range sortedLockKeys(plugins) {
		entry := plugins[id]
		previous, ok := selected[entry.Source.Module]
		if !ok {
			selected[entry.Source.Module] = selectedRelease{
				id: id, source: entry.Source, provider: entry.Provider, fingerprint: entry.ReleaseFingerprint,
			}
			continue
		}
		if previous.source != entry.Source || previous.provider != entry.Provider || previous.fingerprint != entry.ReleaseFingerprint {
			return fmt.Errorf(
				"plugins %q and %q select conflicting releases for source module %q: %s %s versus %s %s",
				previous.id, id, entry.Source.Module,
				previous.source.Version, previous.fingerprint, entry.Source.Version, entry.ReleaseFingerprint,
			)
		}
	}
	return nil
}

func validateDirectReachability(plugins map[string]LockEntry) error {
	reachable := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		entry := plugins[id]
		for dependency := range entry.Dependencies {
			visit(dependency)
		}
		for _, binding := range entry.Bindings {
			for _, provider := range binding.Providers {
				visit(provider)
			}
		}
	}
	for id, entry := range plugins {
		if entry.Direct {
			visit(id)
		}
	}
	for _, id := range sortedLockKeys(plugins) {
		if !reachable[id] {
			return fmt.Errorf("plugin %q is unreachable from every direct plugin", id)
		}
	}
	return nil
}

// ValidateLockedResolution proves that a strict lock is a complete, pruned,
// reproducible resolution of the current manifest without consulting a catalog.
func ValidateLockedResolution(requirements []PluginRequirement, document LockDocument) error {
	if err := ValidateLock(document); err != nil {
		return err
	}
	roots := map[string]string{}
	for _, requirement := range requirements {
		roots[requirement.ID] = requirement.Constraint
		entry, ok := document.Plugins[requirement.ID]
		if !ok || !entry.Direct {
			return fmt.Errorf("direct plugin %q is not resolved", requirement.ID)
		}
		if err := exactVersionSatisfies(entry.Source.Version, requirement.Constraint); err != nil {
			return fmt.Errorf("direct plugin %q: %w", requirement.ID, err)
		}
	}
	for id, entry := range document.Plugins {
		_, root := roots[id]
		if entry.Direct != root {
			return fmt.Errorf("plugin %q direct marker does not match wago.json", id)
		}
		for dependency, constraint := range entry.Dependencies {
			if err := exactVersionSatisfies(document.Plugins[dependency].Source.Version, constraint); err != nil {
				return fmt.Errorf("plugin %q dependency %q: %w", id, dependency, err)
			}
		}
	}
	reachable := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		entry := document.Plugins[id]
		for dependency := range entry.Dependencies {
			visit(dependency)
		}
		for _, binding := range entry.Bindings {
			for _, provider := range binding.Providers {
				visit(provider)
			}
		}
	}
	for id := range roots {
		visit(id)
	}
	for id := range document.Plugins {
		if !reachable[id] {
			return fmt.Errorf("transitive plugin %q is unreachable from every direct requirement", id)
		}
	}
	return nil
}

func exactVersionSatisfies(version, constraint string) error {
	ok, err := semver.Satisfies(version, constraint)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("locked version %s does not satisfy %q", version, constraint)
	}
	return nil
}

func validateLockEntry(id string, entry LockEntry) error {
	if err := ValidatePluginID(entry.Source.Module); err != nil {
		return fmt.Errorf("source.module: %w", err)
	}
	if strings.TrimSpace(entry.Source.Version) == "" || !strings.HasPrefix(entry.Source.Version, "v") {
		return errors.New("source.version must be an exact v-prefixed Go module version")
	}
	if _, err := semver.Parse(entry.Source.Version); err != nil {
		return fmt.Errorf("source.version must be an exact v-prefixed Go module version: %w", err)
	}
	if !ValidGoChecksum(entry.Source.Checksum) {
		return errors.New("source.checksum must be a Go h1 checksum")
	}
	if id != entry.Source.Module && !strings.HasPrefix(id, entry.Source.Module+"/") {
		return fmt.Errorf("plugin ID must belong to source.module %q", entry.Source.Module)
	}
	if err := ValidatePluginID(entry.Provider.ImportPath); err != nil {
		return fmt.Errorf("provider.importPath: %w", err)
	}
	if entry.Provider.ImportPath != entry.Source.Module+"/register" {
		return fmt.Errorf("provider.importPath must be %q", entry.Source.Module+"/register")
	}
	if !validDigest(entry.DefinitionDigest) {
		return errors.New("definitionDigest must be sha256 followed by 64 lowercase hexadecimal characters")
	}
	if !validDigest(entry.ReleaseFingerprint) {
		return errors.New("releaseFingerprint must be sha256 followed by 64 lowercase hexadecimal characters")
	}
	if entry.Dependencies == nil {
		return errors.New("dependencies must be an object")
	}
	for dependency, constraint := range entry.Dependencies {
		if err := ValidatePluginID(dependency); err != nil {
			return fmt.Errorf("dependency: %w", err)
		}
		if ValidateConstraint(strings.TrimSpace(constraint)) != nil {
			return fmt.Errorf("dependency %q has invalid version constraint %q", dependency, constraint)
		}
	}
	if entry.RequestedAuthorities == nil || entry.Grants == nil {
		return errors.New("requestedAuthorities and grants must be arrays")
	}
	if entry.Contracts.Provides == nil || entry.Contracts.Requires == nil || entry.Bindings == nil {
		return errors.New("contracts.provides, contracts.requires, and bindings must be arrays")
	}
	if len(entry.Config) == 0 || !json.Valid(entry.Config) {
		return errors.New("config must contain one JSON value")
	}
	if err := validateAuthorityPolicy(entry.RequestedAuthorities, entry.Grants); err != nil {
		return err
	}
	if err := validateContracts(entry.Contracts); err != nil {
		return err
	}
	return validateBindings(entry.Contracts, entry.Bindings)
}

// ValidGoChecksum reports whether checksum is a canonical Go h1 checksum.
func ValidGoChecksum(checksum string) bool {
	const encodedSHA256Length = 44
	if len(checksum) != len("h1:")+encodedSHA256Length || !strings.HasPrefix(checksum, "h1:") {
		return false
	}
	digest, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(checksum, "h1:"))
	return err == nil && len(digest) == 32
}

func validateAuthorityPolicy(requests []AuthorityRequest, grants []AuthorityGrant) error {
	requested := make(map[string]AuthorityRequest, len(requests))
	for _, request := range requests {
		if _, ok := knownAuthorities[request.Name]; !ok {
			return fmt.Errorf("unknown requested authority %q", request.Name)
		}
		if _, duplicate := requested[request.Name]; duplicate {
			return fmt.Errorf("requested authority %q is repeated", request.Name)
		}
		if request.Mode != AuthorityRequired && request.Mode != AuthorityOptional {
			return fmt.Errorf("authority %q mode must be required or optional", request.Name)
		}
		if strings.TrimSpace(request.Reason) == "" {
			return fmt.Errorf("authority %q needs a human-readable reason", request.Name)
		}
		if err := validateAuthorityScope(request.Name, request.Scope); err != nil {
			return err
		}
		requested[request.Name] = request
	}
	granted := make(map[string]AuthorityGrant, len(grants))
	for _, grant := range grants {
		request, ok := requested[grant.Name]
		if !ok {
			return fmt.Errorf("grant %q was not requested", grant.Name)
		}
		if _, duplicate := granted[grant.Name]; duplicate {
			return fmt.Errorf("grant %q is repeated", grant.Name)
		}
		if err := validateAuthorityScope(grant.Name, grant.Scope); err != nil {
			return err
		}
		if !scopeNarrows(request.Scope, grant.Scope) {
			return fmt.Errorf("grant %q widens its requested scope", grant.Name)
		}
		granted[grant.Name] = grant
	}
	return nil
}

func validateAuthorityScope(authority string, scope AuthorityScope) error {
	switch authority {
	case "host.import.define", "compiler.type.define", "compiler.instruction.define":
		if len(scope.Modules) == 0 || scope.MaxInstances != 0 || scope.MaxMemoryBytes != 0 {
			return fmt.Errorf("authority %q requires scope.modules and no limits", authority)
		}
		seen := map[string]bool{}
		for _, module := range scope.Modules {
			if strings.TrimSpace(module) == "" || module != strings.TrimSpace(module) || strings.Contains(module, "*") || seen[module] {
				return fmt.Errorf("authority %q scope.modules must contain unique exact module names", authority)
			}
			seen[module] = true
		}
	case "instance.manage", "core.instance.instantiate":
		if len(scope.Modules) != 0 || scope.MaxInstances == 0 || scope.MaxMemoryBytes == 0 {
			return fmt.Errorf("authority %q requires positive maxInstances and maxMemoryBytes and no modules", authority)
		}
	default:
		if len(scope.Modules) != 0 || scope.MaxInstances != 0 || scope.MaxMemoryBytes != 0 {
			return fmt.Errorf("authority %q does not accept a scope", authority)
		}
	}
	return nil
}

func scopeNarrows(requested, granted AuthorityScope) bool {
	if granted.MaxInstances > requested.MaxInstances {
		return false
	}
	if granted.MaxMemoryBytes > requested.MaxMemoryBytes {
		return false
	}
	allowed := make(map[string]bool, len(requested.Modules))
	for _, module := range requested.Modules {
		allowed[module] = true
	}
	for _, module := range granted.Modules {
		if !allowed[module] {
			return false
		}
	}
	return len(requested.Modules) == 0 || len(granted.Modules) > 0
}

func validateBindings(contracts ContractSet, bindings []ContractBinding) error {
	requirements := make(map[string]ContractRequirement, len(contracts.Requires))
	for _, requirement := range contracts.Requires {
		requirements[fmt.Sprintf("%s@%d", requirement.ID, requirement.Major)] = requirement
	}
	seen := map[string]bool{}
	for _, binding := range bindings {
		if err := validateContract(binding.ID, binding.Major); err != nil {
			return fmt.Errorf("contract binding: %w", err)
		}
		key := fmt.Sprintf("%s@%d", binding.ID, binding.Major)
		requirement, ok := requirements[key]
		if !ok {
			return fmt.Errorf("contract binding %q was not required", key)
		}
		if seen[key] {
			return fmt.Errorf("contract binding %q is repeated", key)
		}
		seen[key] = true
		switch requirement.Mode {
		case "required":
			if len(binding.Providers) != 1 {
				return fmt.Errorf("required contract binding %q must have exactly one provider", key)
			}
		case "optional":
			if len(binding.Providers) > 1 {
				return fmt.Errorf("optional contract binding %q has multiple providers", key)
			}
		}
		providerSeen := map[string]bool{}
		for _, provider := range binding.Providers {
			if err := ValidatePluginID(provider); err != nil {
				return fmt.Errorf("contract binding %q provider: %w", key, err)
			}
			if providerSeen[provider] {
				return fmt.Errorf("contract binding %q repeats provider %q", key, provider)
			}
			providerSeen[provider] = true
		}
	}
	for key := range requirements {
		if !seen[key] {
			return fmt.Errorf("contract %q has no explicit reviewed binding", key)
		}
	}
	return nil
}

func validateContracts(contracts ContractSet) error {
	provided := map[string]bool{}
	for _, contract := range contracts.Provides {
		if err := validateContract(contract.ID, contract.Major); err != nil {
			return fmt.Errorf("provided contract: %w", err)
		}
		key := fmt.Sprintf("%s@%d", contract.ID, contract.Major)
		if provided[key] {
			return fmt.Errorf("provided contract %q is repeated", key)
		}
		provided[key] = true
	}
	required := map[string]bool{}
	for _, contract := range contracts.Requires {
		if err := validateContract(contract.ID, contract.Major); err != nil {
			return fmt.Errorf("required contract: %w", err)
		}
		key := fmt.Sprintf("%s@%d", contract.ID, contract.Major)
		if required[key] {
			return fmt.Errorf("required contract %q is repeated", key)
		}
		if contract.Mode != "required" && contract.Mode != "optional" && contract.Mode != "many" {
			return fmt.Errorf("required contract %q mode must be required, optional, or many", key)
		}
		required[key] = true
	}
	return nil
}

func validateContract(id string, major uint32) error {
	if err := ValidatePluginID(id); err != nil {
		return err
	}
	if major == 0 {
		return errors.New("contract major must be positive")
	}
	return nil
}

func validateLockGraph(plugins map[string]LockEntry) error {
	const (
		unseen = iota
		visiting
		done
	)
	state := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case visiting:
			return fmt.Errorf("plugin dependency cycle includes %q", id)
		case done:
			return nil
		}
		state[id] = visiting
		predecessors := sortedMapKeys(plugins[id].Dependencies)
		for _, binding := range plugins[id].Bindings {
			predecessors = append(predecessors, binding.Providers...)
		}
		sort.Strings(predecessors)
		for _, dependency := range predecessors {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = done
		return nil
	}
	for _, id := range sortedLockKeys(plugins) {
		if err := visit(id); err != nil {
			return err
		}
	}
	return validateContractGraph(plugins)
}

func validateContractGraph(plugins map[string]LockEntry) error {
	providers := map[string][]string{}
	for id, entry := range plugins {
		for _, contract := range entry.Contracts.Provides {
			key := fmt.Sprintf("%s@%d", contract.ID, contract.Major)
			providers[key] = append(providers[key], id)
		}
	}
	for id, entry := range plugins {
		requirements := make(map[string]ContractRequirement, len(entry.Contracts.Requires))
		for _, requirement := range entry.Contracts.Requires {
			key := fmt.Sprintf("%s@%d", requirement.ID, requirement.Major)
			requirements[key] = requirement
		}
		for _, binding := range entry.Bindings {
			key := fmt.Sprintf("%s@%d", binding.ID, binding.Major)
			available := map[string]bool{}
			for _, provider := range providers[key] {
				available[provider] = true
			}
			for _, provider := range binding.Providers {
				if !available[provider] {
					return fmt.Errorf("plugin %q binds contract %q to %q, which does not provide it", id, key, provider)
				}
			}
			if requirements[key].Mode == "many" && !sameStrings(binding.Providers, providers[key]) {
				return fmt.Errorf("plugin %q many contract binding %q must include every selected provider", id, key)
			}
		}
	}
	return nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	a, b := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func validDigest(value string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+64 {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("multiple JSON values are not allowed")
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedLockKeys(values map[string]LockEntry) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func displayFilePath(path string) string {
	dir := filepath.Dir(path)
	if filepath.Base(path) == File {
		return DisplayPath(dir)
	}
	displayDir := DisplayPath(dir)
	if displayDir == "~" {
		return "~/" + filepath.Base(path)
	}
	return filepath.Join(displayDir, filepath.Base(path))
}
