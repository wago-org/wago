package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/manager/internal/registry"
	"github.com/wago-org/wago/internal/httpclient"
	"github.com/wago-org/wago/src/core/semver"
	corewago "github.com/wago-org/wago/src/wago"
)

// Catalog is the sole registry surface used by dependency resolution.
type Catalog interface {
	Candidates(context.Context, string, []string) ([]CatalogRelease, error)
}

type CatalogRelease struct {
	ID                 string                    `json:"id"`
	Version            string                    `json:"version"`
	Source             project.PluginSource      `json:"source"`
	Provider           project.ProviderSource    `json:"provider"`
	Definition         corewago.PluginDefinition `json:"definition"`
	DefinitionDigest   string                    `json:"definitionDigest"`
	ReleaseFingerprint string                    `json:"releaseFingerprint"`
}

type catalogEnvelope struct {
	Plugins    []CatalogRelease `json:"plugins"`
	Total      int              `json:"total"`
	Offset     int              `json:"offset"`
	Limit      int              `json:"limit"`
	NextOffset *int             `json:"nextOffset,omitempty"`
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type HTTPCatalog struct {
	BaseURL string
	Client  HTTPDoer
}

type defaultCatalogHTTPClient struct{ client *httpclient.Client }

func (client defaultCatalogHTTPClient) Do(request *http.Request) (*http.Response, error) {
	return client.client.Open(request.Context(), request)
}

var registryCatalogHTTP = defaultCatalogHTTPClient{client: httpclient.NewAPI()}

const (
	catalogPageLimit                 = 256
	catalogResponseLimit       int64 = 4 << 20
	maxCatalogPages                  = 4
	maxCatalogBytes                  = maxCatalogPages * catalogResponseLimit
	maxCatalogCandidates             = 1024
	maxCatalogNestedCollection       = 128
	maxCatalogStructureDepth         = 32
)

func (catalog HTTPCatalog) Candidates(ctx context.Context, id string, constraints []string) ([]CatalogRelease, error) {
	if err := automation.RequireOnline("plugin catalog resolution"); err != nil {
		return nil, err
	}
	if err := registry.ValidateBaseURL(catalog.BaseURL); err != nil {
		return nil, err
	}
	if catalog.Client == nil {
		catalog.Client = registryCatalogHTTP
	}
	baseEndpoint, err := url.Parse(strings.TrimRight(catalog.BaseURL, "/") + "/api/v1/plugins/candidates")
	if err != nil {
		return nil, err
	}
	constraints = sortedUnique(constraints)
	var releases []CatalogRelease
	seenReleases := map[string]bool{}
	wantTotal := -1
	pages := 0
	var catalogBytes int64
	for offset := 0; ; {
		if pages >= maxCatalogPages {
			return nil, fmt.Errorf("resolve %s: catalog pagination exceeds %d-page bound", id, maxCatalogPages)
		}
		pages++
		endpoint := *baseEndpoint
		query := endpoint.Query()
		query.Set("id", id)
		query.Set("limit", fmt.Sprint(catalogPageLimit))
		query.Set("offset", fmt.Sprint(offset))
		for _, constraint := range constraints {
			query.Add("range", constraint)
		}
		endpoint.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, err
		}
		response, err := catalog.Client.Do(request)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, catalogResponseLimit+1))
		closeErr := response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		pageBytes := int64(len(data))
		if pageBytes > catalogResponseLimit {
			return nil, errors.New("plugin catalog response exceeds 4 MiB")
		}
		if pageBytes > maxCatalogBytes-catalogBytes {
			return nil, fmt.Errorf("resolve %s: catalog metadata exceeds %d MiB cumulative bound", id, maxCatalogBytes>>20)
		}
		catalogBytes += pageBytes
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("resolve %s: %s", id, registry.ResponseError(response.StatusCode, data))
		}
		if err := preflightCatalogPage(data, catalogPageLimit); err != nil {
			return nil, fmt.Errorf("resolve %s: catalog returned invalid metadata", id)
		}
		// configSchema is arbitrary JSON whose property names are case-sensitive;
		// the surrounding catalog structs use encoding/json's folded field lookup.
		if err := registry.ValidateUniqueFoldedJSON(data, "configSchema"); err != nil {
			return nil, fmt.Errorf("resolve %s: catalog returned invalid metadata", id)
		}
		var envelope catalogEnvelope
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&envelope); err != nil {
			return nil, fmt.Errorf("resolve %s: catalog returned invalid metadata", id)
		}
		if err := requireJSONEOF(decoder); err != nil {
			return nil, fmt.Errorf("resolve %s: catalog returned invalid metadata", id)
		}
		if envelope.Total <= 0 || envelope.Offset != offset || envelope.Limit != catalogPageLimit || len(envelope.Plugins) == 0 || len(envelope.Plugins) > catalogPageLimit {
			return nil, fmt.Errorf("resolve %s: catalog returned invalid page offset=%d limit=%d count=%d total=%d", id, envelope.Offset, envelope.Limit, len(envelope.Plugins), envelope.Total)
		}
		if wantTotal < 0 {
			wantTotal = envelope.Total
		} else if envelope.Total != wantTotal {
			return nil, fmt.Errorf("resolve %s: catalog total changed during pagination (%d to %d)", id, wantTotal, envelope.Total)
		}
		if envelope.Total > maxCatalogCandidates {
			return nil, fmt.Errorf("resolve %s: catalog exposes %d candidates, exceeding catalog bound %d", id, envelope.Total, maxCatalogCandidates)
		}
		for _, release := range envelope.Plugins {
			if err := validateCatalogRelease(id, constraints, release); err != nil {
				return nil, err
			}
			key := release.ID + "\x00" + release.Version + "\x00" + release.ReleaseFingerprint
			if seenReleases[key] {
				return nil, fmt.Errorf("resolve %s: catalog repeated a release", id)
			}
			seenReleases[key] = true
		}
		releases = append(releases, envelope.Plugins...)
		if envelope.NextOffset == nil {
			if len(releases) != wantTotal {
				return nil, fmt.Errorf("resolve %s: catalog pagination ended after %d of %d candidates", id, len(releases), wantTotal)
			}
			break
		}
		if *envelope.NextOffset <= offset || *envelope.NextOffset != offset+len(envelope.Plugins) || *envelope.NextOffset >= wantTotal {
			return nil, fmt.Errorf("resolve %s: catalog returned invalid nextOffset %d", id, *envelope.NextOffset)
		}
		offset = *envelope.NextOffset
	}
	sortCatalogReleases(releases)
	return releases, nil
}

func preflightCatalogPage(data []byte, maximumPlugins int) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("catalog page must be a JSON object")
	}
	members := 0
	for decoder.More() {
		members++
		if members > maxCatalogNestedCollection {
			return errors.New("catalog object exceeds the nested collection limit")
		}
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return errors.New("catalog page contains a non-string key")
		}
		if !strings.EqualFold(key, "plugins") {
			if err := preflightCatalogValue(decoder, 1, strings.EqualFold(key, "configSchema")); err != nil {
				return err
			}
			continue
		}
		array, err := decoder.Token()
		if err != nil || array != json.Delim('[') {
			return errors.New("catalog plugins must be an array")
		}
		count := 0
		for decoder.More() {
			count++
			if count > maximumPlugins {
				return errors.New("catalog plugins exceed the page limit")
			}
			if err := preflightCatalogValue(decoder, 1, false); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("catalog plugins array is incomplete")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return errors.New("catalog page object is incomplete")
	}
	return requireJSONEOF(decoder)
}

func preflightCatalogValue(decoder *json.Decoder, depth int, exactSchema bool) error {
	if exactSchema {
		return skipJSONValue(decoder)
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return errors.New("unexpected closing JSON delimiter")
	}
	if depth > maxCatalogStructureDepth {
		return errors.New("catalog metadata exceeds the structure depth limit")
	}
	items := 0
	for decoder.More() {
		items++
		if items > maxCatalogNestedCollection {
			return errors.New("catalog metadata exceeds the nested collection limit")
		}
		exactChild := false
		if delimiter == '{' {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("catalog metadata contains a non-string object key")
			}
			exactChild = strings.EqualFold(key, "configSchema")
		}
		if err := preflightCatalogValue(decoder, depth+1, exactChild); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter == '{' && closing != json.Delim('}') || delimiter == '[' && closing != json.Delim(']') {
		return errors.New("catalog metadata contains a mismatched closing delimiter")
	}
	return nil
}

func skipJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return errors.New("unexpected closing JSON delimiter")
	}
	depth := 1
	for depth > 0 {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
		if delimiter, ok = token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

// MemoryCatalog is a deterministic in-memory adapter used by resolver tests.
type MemoryCatalog struct {
	Releases map[string][]CatalogRelease
	Calls    []CatalogCall
}

type CatalogCall struct {
	ID          string
	Constraints []string
}

func (catalog *MemoryCatalog) Candidates(ctx context.Context, id string, constraints []string) ([]CatalogRelease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	constraints = sortedUnique(constraints)
	catalog.Calls = append(catalog.Calls, CatalogCall{ID: id, Constraints: append([]string(nil), constraints...)})
	var matches []CatalogRelease
	for _, release := range catalog.Releases[id] {
		accepted := true
		for _, constraint := range constraints {
			ok, err := semver.Satisfies(release.Version, constraint)
			if err != nil {
				return nil, fmt.Errorf("plugin %s constraint %q: %w", id, constraint, err)
			}
			if !ok {
				accepted = false
				break
			}
		}
		if accepted {
			matches = append(matches, release)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("plugin %s has no release satisfying %s", id, strings.Join(constraints, " and "))
	}
	sortCatalogReleases(matches)
	for _, match := range matches {
		if err := validateCatalogRelease(id, constraints, match); err != nil {
			return nil, err
		}
	}
	return matches, nil
}

type AuthorityReview struct {
	PluginID string
	Direct   bool
	Request  project.AuthorityRequest
	Previous *project.AuthorityGrant
	Proposed project.AuthorityGrant
	Change   string
}

type ResolutionPlan struct {
	Lock            project.LockDocument
	Reviews         []AuthorityReview
	ContractReviews []ContractReview
}

type ContractReview struct {
	PluginID  string
	Request   project.ContractRequirement
	Previous  []string
	Proposed  []string
	Available []string
	Change    string
}

const (
	maxResolvedPlugins        = 1024
	maxResolverSteps          = 100000
	maxResolverCatalogQueries = 2048
)

var errCatalogQueryBudget = errors.New("plugin dependency resolution exceeded the catalog query budget")

// ResolveCatalogGraph performs a deterministic, bounded global solve. Candidate
// releases are tried newest-first, but a later transitive constraint can unwind
// earlier choices, so a valid lower-version graph is never rejected merely
// because a locally highest candidate was incompatible.
func ResolveCatalogGraph(ctx context.Context, catalog Catalog, roots []project.PluginRequirement, previous project.LockDocument) (ResolutionPlan, error) {
	if catalog == nil {
		return ResolutionPlan{}, errors.New("plugin catalog is nil")
	}
	contributions := map[string]map[string]string{}
	direct := map[string]bool{}
	for _, root := range roots {
		if err := project.ValidatePluginID(root.ID); err != nil {
			return ResolutionPlan{}, err
		}
		if err := project.ValidateConstraint(root.Constraint); err != nil {
			return ResolutionPlan{}, fmt.Errorf("plugin %s: %w", root.ID, err)
		}
		if contributions[root.ID] == nil {
			contributions[root.ID] = map[string]string{}
		}
		contributions[root.ID]["@manifest"] = root.Constraint
		direct[root.ID] = true
	}
	solver := catalogSolver{ctx: ctx, catalog: catalog, direct: direct, previous: previous}
	plan, err := solver.solve(map[string]CatalogRelease{}, contributions)
	if err != nil {
		return ResolutionPlan{}, err
	}
	return plan, nil
}

type catalogSolver struct {
	ctx      context.Context
	catalog  Catalog
	direct   map[string]bool
	previous project.LockDocument
	steps    int
	queries  int
	lastErr  error
}

func (solver *catalogSolver) solve(selected map[string]CatalogRelease, constraints map[string]map[string]string) (ResolutionPlan, error) {
	if err := solver.ctx.Err(); err != nil {
		return ResolutionPlan{}, err
	}
	solver.steps++
	if solver.steps > maxResolverSteps {
		return ResolutionPlan{}, fmt.Errorf("plugin dependency resolution exceeded %d candidate steps", maxResolverSteps)
	}
	if len(constraints) > maxResolvedPlugins {
		return ResolutionPlan{}, fmt.Errorf("plugin dependency graph exceeds %d plugins", maxResolvedPlugins)
	}
	id := firstUnselectedID(selected, constraints, solver.direct)
	if id == "" {
		plan, err := solver.finish(selected)
		if err != nil {
			solver.lastErr = err
			return ResolutionPlan{}, err
		}
		return plan, nil
	}
	ranges := sortedMapValues(constraints[id])
	if solver.queries >= maxResolverCatalogQueries {
		return ResolutionPlan{}, fmt.Errorf("%w of %d requests", errCatalogQueryBudget, maxResolverCatalogQueries)
	}
	solver.queries++
	candidates, err := solver.catalog.Candidates(solver.ctx, id, ranges)
	if err != nil {
		solver.lastErr = err
		return ResolutionPlan{}, err
	}
	for _, candidate := range candidates {
		if err := sharedSourceReleaseConflict(selected, candidate); err != nil {
			solver.lastErr = err
			continue
		}
		nextSelected := cloneSelections(selected)
		nextConstraints := cloneContributions(constraints)
		nextSelected[id] = candidate
		compatible := true
		for _, requirement := range candidate.Definition.Requires {
			if err := project.ValidatePluginID(requirement.ID); err != nil {
				solver.lastErr = fmt.Errorf("plugin %s requirement: %w", id, err)
				compatible = false
				break
			}
			if err := project.ValidateConstraint(requirement.Version); err != nil {
				solver.lastErr = fmt.Errorf("plugin %s requirement %s: %w", id, requirement.ID, err)
				compatible = false
				break
			}
			if nextConstraints[requirement.ID] == nil {
				nextConstraints[requirement.ID] = map[string]string{}
			}
			nextConstraints[requirement.ID][id] = requirement.Version
			if assigned, ok := nextSelected[requirement.ID]; ok && !releaseSatisfiesAll(assigned, sortedMapValues(nextConstraints[requirement.ID])) {
				solver.lastErr = fmt.Errorf("plugin %s version %s conflicts with constraints %s", requirement.ID, assigned.Version, strings.Join(sortedMapValues(nextConstraints[requirement.ID]), " and "))
				compatible = false
				break
			}
		}
		if !compatible {
			continue
		}
		plan, err := solver.solve(nextSelected, nextConstraints)
		if err == nil {
			return plan, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errCatalogQueryBudget) || solver.steps > maxResolverSteps {
			return ResolutionPlan{}, err
		}
	}
	if solver.lastErr != nil {
		return ResolutionPlan{}, fmt.Errorf("resolve plugin %s: no candidate completes the graph: %w", id, solver.lastErr)
	}
	return ResolutionPlan{}, fmt.Errorf("resolve plugin %s: no candidate completes the graph", id)
}

func (solver *catalogSolver) finish(selected map[string]CatalogRelease) (ResolutionPlan, error) {
	document := project.NewLockDocument()
	var reviews []AuthorityReview
	for _, id := range sortedReleaseKeys(selected) {
		entry, entryReviews := lockEntryFromRelease(selected[id], solver.direct[id], solver.previous.Plugins[id])
		document.Plugins[id] = entry
		reviews = append(reviews, entryReviews...)
	}
	contractReviews, err := bindContracts(&document, solver.previous)
	if err != nil {
		return ResolutionPlan{}, err
	}
	if err := project.ValidateLock(document); err != nil {
		return ResolutionPlan{}, err
	}
	return ResolutionPlan{Lock: document, Reviews: reviews, ContractReviews: contractReviews}, nil
}

func firstUnselectedID(selected map[string]CatalogRelease, constraints map[string]map[string]string, direct map[string]bool) string {
	ids := make([]string, 0, len(constraints))
	for id := range constraints {
		if _, ok := selected[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if direct[ids[i]] != direct[ids[j]] {
			return direct[ids[i]]
		}
		return ids[i] < ids[j]
	})
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func cloneSelections(input map[string]CatalogRelease) map[string]CatalogRelease {
	output := make(map[string]CatalogRelease, len(input)+1)
	for id, release := range input {
		output[id] = release
	}
	return output
}

func cloneContributions(input map[string]map[string]string) map[string]map[string]string {
	output := make(map[string]map[string]string, len(input)+1)
	for id, sources := range input {
		copy := make(map[string]string, len(sources)+1)
		for source, constraint := range sources {
			copy[source] = constraint
		}
		output[id] = copy
	}
	return output
}

func releaseSatisfiesAll(release CatalogRelease, constraints []string) bool {
	for _, constraint := range constraints {
		ok, err := semver.Satisfies(release.Version, constraint)
		if err != nil || !ok {
			return false
		}
	}
	return true
}

func sharedSourceReleaseConflict(selected map[string]CatalogRelease, candidate CatalogRelease) error {
	for _, id := range sortedReleaseKeys(selected) {
		other := selected[id]
		if other.Source.Module != candidate.Source.Module {
			continue
		}
		if other.Source != candidate.Source || other.Provider != candidate.Provider || other.ReleaseFingerprint != candidate.ReleaseFingerprint {
			return fmt.Errorf(
				"plugins %s and %s select conflicting releases for source module %s: %s %s versus %s %s",
				other.ID, candidate.ID, candidate.Source.Module,
				other.Source.Version, other.ReleaseFingerprint, candidate.Source.Version, candidate.ReleaseFingerprint,
			)
		}
	}
	return nil
}

func lockEntryFromRelease(release CatalogRelease, direct bool, previous project.LockEntry) (project.LockEntry, []AuthorityReview) {
	entry := project.LockEntry{
		Direct: direct, Source: release.Source, Provider: release.Provider,
		DefinitionDigest: release.DefinitionDigest, ReleaseFingerprint: release.ReleaseFingerprint,
		Dependencies: map[string]string{}, RequestedAuthorities: []project.AuthorityRequest{}, Grants: []project.AuthorityGrant{},
		Contracts: project.ContractSet{Provides: []project.ContractProvider{}, Requires: []project.ContractRequirement{}},
		Bindings:  []project.ContractBinding{}, Config: json.RawMessage(`{}`),
	}
	if len(previous.Config) != 0 {
		entry.Config = append(json.RawMessage(nil), previous.Config...)
	}
	for _, requirement := range release.Definition.Requires {
		entry.Dependencies[requirement.ID] = requirement.Version
	}
	for _, spec := range release.Definition.Provides {
		entry.Contracts.Provides = append(entry.Contracts.Provides, project.ContractProvider{ID: spec.ID, Major: spec.Major})
	}
	for _, requirement := range release.Definition.Consumes {
		entry.Contracts.Requires = append(entry.Contracts.Requires, project.ContractRequirement{ID: requirement.ID, Major: requirement.Major, Mode: string(requirement.Mode)})
	}
	oldRequests := map[string]project.AuthorityRequest{}
	oldGrants := map[string]project.AuthorityGrant{}
	for _, request := range previous.RequestedAuthorities {
		oldRequests[request.Name] = request
	}
	for _, grant := range previous.Grants {
		oldGrants[grant.Name] = grant
	}
	var reviews []AuthorityReview
	for _, request := range release.Definition.Authorities {
		converted := project.AuthorityRequest{Name: string(request.Name), Mode: string(request.Mode), Reason: request.Reason, Scope: project.AuthorityScope{
			Modules: append([]string(nil), request.Scope.Modules...), MaxInstances: request.Scope.MaxInstances, MaxMemoryBytes: request.Scope.MaxMemoryBytes,
		}}
		entry.RequestedAuthorities = append(entry.RequestedAuthorities, converted)
		proposed := project.AuthorityGrant{Name: converted.Name, Scope: converted.Scope}
		var old *project.AuthorityGrant
		if grant, ok := oldGrants[converted.Name]; ok && projectGrantFits(converted, grant) {
			copy := grant
			old = &copy
			proposed = grant
		}
		if converted.Mode == project.AuthorityRequired || old != nil {
			entry.Grants = append(entry.Grants, proposed)
		}
		if change := authorityChange(oldRequests[converted.Name], converted, old); change != "" {
			reviews = append(reviews, AuthorityReview{PluginID: release.ID, Direct: direct, Request: converted, Previous: old, Proposed: proposed, Change: change})
		}
	}
	sort.Slice(entry.RequestedAuthorities, func(i, j int) bool { return entry.RequestedAuthorities[i].Name < entry.RequestedAuthorities[j].Name })
	sort.Slice(entry.Grants, func(i, j int) bool { return entry.Grants[i].Name < entry.Grants[j].Name })
	return entry, reviews
}

type contractBindingChoice struct {
	pluginID        string
	bindingIndex    int
	requirement     project.ContractRequirement
	previous        []string
	previouslyBound bool
	available       []string
	options         [][]string
}

const maxContractBindingSteps = 100000

func bindContracts(document *project.LockDocument, previous project.LockDocument) ([]ContractReview, error) {
	providers := map[string][]string{}
	for id, entry := range document.Plugins {
		for _, contract := range entry.Contracts.Provides {
			providers[contractKey(contract.ID, contract.Major)] = append(providers[contractKey(contract.ID, contract.Major)], id)
		}
	}
	for key := range providers {
		sort.Strings(providers[key])
	}
	var choices []contractBindingChoice
	for _, id := range sortedLockPluginKeys(document.Plugins) {
		entry := document.Plugins[id]
		entry.Bindings = make([]project.ContractBinding, 0, len(entry.Contracts.Requires))
		for _, requirement := range entry.Contracts.Requires {
			matches := append([]string{}, providers[contractKey(requirement.ID, requirement.Major)]...)
			previousProviders, previouslyBound := previousBinding(previous.Plugins[id].Bindings, requirement.ID, requirement.Major)
			preserved := filterAvailableInOrder(previousProviders, matches)
			var options [][]string
			switch requirement.Mode {
			case "required":
				if len(matches) == 0 {
					return nil, fmt.Errorf("plugin %s requires a provider for contract %s@%d; found none", id, requirement.ID, requirement.Major)
				}
				if len(preserved) == 1 {
					options = append(options, append([]string(nil), preserved...))
				} else {
					options = append(options, []string{matches[0]})
				}
				for _, provider := range matches {
					candidate := []string{provider}
					if !containsOrderedChoice(options, candidate) {
						options = append(options, candidate)
					}
				}
			case "optional":
				if len(preserved) > 0 {
					options = append(options, append([]string(nil), preserved[:1]...))
					for _, provider := range matches {
						candidate := []string{provider}
						if !containsOrderedChoice(options, candidate) {
							options = append(options, candidate)
						}
					}
					options = append(options, []string{})
				} else {
					options = append(options, []string{})
					for _, provider := range matches {
						options = append(options, []string{provider})
					}
				}
			case "many":
				options = append(options, appendNewProviders(preserved, matches))
			default:
				return nil, fmt.Errorf("plugin %s has unknown contract mode %q", id, requirement.Mode)
			}
			bindingIndex := len(entry.Bindings)
			entry.Bindings = append(entry.Bindings, project.ContractBinding{ID: requirement.ID, Major: requirement.Major, Providers: append([]string{}, options[0]...)})
			choices = append(choices, contractBindingChoice{
				pluginID: id, bindingIndex: bindingIndex, requirement: requirement,
				previous: append([]string(nil), previousProviders...), previouslyBound: previouslyBound,
				available: append([]string(nil), matches...), options: options,
			})
		}
		document.Plugins[id] = entry
	}
	steps := 0
	if err := chooseValidContractBindings(document, choices, 0, &steps); err != nil {
		return nil, fmt.Errorf("no valid exact contract-binding graph: %w", err)
	}
	var reviews []ContractReview
	for _, choice := range choices {
		chosen := document.Plugins[choice.pluginID].Bindings[choice.bindingIndex].Providers
		needsReview := !sameOrderedStrings(choice.previous, chosen)
		// A new optional requirement with available providers needs an explicit
		// decision even though its safe proposal is no provider. An existing
		// reviewed empty binding remains stable across updates.
		if choice.requirement.Mode == "optional" && !choice.previouslyBound && len(choice.available) != 0 {
			needsReview = true
		}
		if !needsReview {
			continue
		}
		change := "new"
		if choice.previouslyBound {
			change = "changed"
		}
		reviews = append(reviews, ContractReview{
			PluginID: choice.pluginID, Request: choice.requirement,
			Previous: append([]string(nil), choice.previous...), Proposed: append([]string(nil), chosen...),
			Available: append([]string(nil), choice.available...), Change: change,
		})
	}
	return reviews, nil
}

func chooseValidContractBindings(document *project.LockDocument, choices []contractBindingChoice, index int, steps *int) error {
	if index == len(choices) {
		return project.ValidateLock(*document)
	}
	choice := choices[index]
	var lastErr error
	for _, option := range choice.options {
		(*steps)++
		if *steps > maxContractBindingSteps {
			return fmt.Errorf("contract binding resolution exceeded %d candidate steps", maxContractBindingSteps)
		}
		entry := document.Plugins[choice.pluginID]
		entry.Bindings[choice.bindingIndex].Providers = append([]string{}, option...)
		document.Plugins[choice.pluginID] = entry
		if err := chooseValidContractBindings(document, choices, index+1, steps); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func containsOrderedChoice(options [][]string, candidate []string) bool {
	for _, option := range options {
		if sameOrderedStrings(option, candidate) {
			return true
		}
	}
	return false
}

func sortedLockPluginKeys(values map[string]project.LockEntry) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func previousBinding(bindings []project.ContractBinding, id string, major uint32) ([]string, bool) {
	for _, binding := range bindings {
		if binding.ID == id && binding.Major == major {
			return append([]string{}, binding.Providers...), true
		}
	}
	return nil, false
}

func filterAvailableInOrder(previous, available []string) []string {
	allowed := map[string]bool{}
	for _, provider := range available {
		allowed[provider] = true
	}
	var result []string
	for _, provider := range previous {
		if allowed[provider] {
			result = append(result, provider)
		}
	}
	return result
}

func appendNewProviders(preserved, available []string) []string {
	result := append([]string{}, preserved...)
	seen := make(map[string]bool, len(preserved))
	for _, provider := range preserved {
		seen[provider] = true
	}
	for _, provider := range available {
		if !seen[provider] {
			result = append(result, provider)
		}
	}
	return result
}

func sameOrderedStrings(left, right []string) bool {
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

func validateCatalogRelease(id string, constraints []string, release CatalogRelease) error {
	if release.ID != id || release.Definition.ID != id {
		return fmt.Errorf("plugin %s catalog identifiers do not match the request", id)
	}
	if err := project.ValidatePluginID(release.Source.Module); err != nil {
		return fmt.Errorf("plugin %s catalog source module is invalid", id)
	}
	if err := project.ValidatePluginID(release.Provider.ImportPath); err != nil {
		return fmt.Errorf("plugin %s catalog provider import is invalid", id)
	}
	if release.Provider.ImportPath != release.Source.Module+"/register" {
		return fmt.Errorf("plugin %s catalog provider does not match its source module", id)
	}
	if !project.ValidGoChecksum(release.Source.Checksum) {
		return fmt.Errorf("plugin %s catalog source checksum is invalid", id)
	}
	const maximumCatalogVersionLength = 200
	if release.Source.Version == "" || len(release.Source.Version) > maximumCatalogVersionLength || !strings.HasPrefix(release.Source.Version, "v") {
		return fmt.Errorf("plugin %s catalog source version is not an exact v-prefixed Go module version", id)
	}
	if _, err := semver.Parse(release.Source.Version); err != nil {
		return fmt.Errorf("plugin %s catalog source version is invalid", id)
	}
	if release.Version == "" || len(release.Version) > maximumCatalogVersionLength || len(release.Definition.Version) > maximumCatalogVersionLength {
		return fmt.Errorf("plugin %s catalog version is invalid", id)
	}
	if release.Version != release.Definition.Version || strings.TrimPrefix(release.Source.Version, "v") != strings.TrimPrefix(release.Version, "v") {
		return fmt.Errorf("plugin %s catalog, source, and definition versions disagree", id)
	}
	for _, requirement := range release.Definition.Requires {
		if err := project.ValidatePluginID(requirement.ID); err != nil {
			return fmt.Errorf("plugin %s catalog requirement is invalid", id)
		}
		if strings.TrimSpace(requirement.Version) == "" {
			return fmt.Errorf("plugin %s requirement version constraint is empty", id)
		}
		if err := project.ValidateConstraint(requirement.Version); err != nil {
			return fmt.Errorf("plugin %s catalog requirement is invalid", id)
		}
	}
	for _, constraint := range release.Definition.Compatibility.Engines {
		if err := project.ValidateConstraint(constraint); err != nil {
			return fmt.Errorf("plugin %s catalog engine compatibility is invalid", id)
		}
	}
	for _, constraint := range constraints {
		ok, err := semver.Satisfies(release.Version, constraint)
		if err != nil || !ok {
			return fmt.Errorf("plugin %s version %s does not satisfy %q", id, release.Version, constraint)
		}
	}
	if !validSHA256(release.DefinitionDigest) {
		return fmt.Errorf("plugin %s catalog definition digest is invalid", id)
	}
	digest, err := corewago.DefinitionDigest(release.Definition)
	if err != nil {
		return fmt.Errorf("plugin %s catalog definition is invalid", id)
	}
	if digest != release.DefinitionDigest {
		return fmt.Errorf("plugin %s definition digest mismatch", id)
	}
	if !validSHA256(release.ReleaseFingerprint) {
		return fmt.Errorf("plugin %s has invalid release fingerprint", id)
	}
	return nil
}

func projectGrantFits(request project.AuthorityRequest, grant project.AuthorityGrant) bool {
	if request.Name != grant.Name {
		return false
	}
	if request.Name == "host.import.define" || request.Name == "compiler.type.define" || request.Name == "compiler.instruction.define" {
		allowed := map[string]bool{}
		for _, module := range request.Scope.Modules {
			allowed[module] = true
		}
		if len(grant.Scope.Modules) == 0 {
			return false
		}
		for _, module := range grant.Scope.Modules {
			if !allowed[module] {
				return false
			}
		}
		return true
	}
	if request.Name == "instance.manage" || request.Name == "core.instance.instantiate" {
		instancesFit := grant.Scope.MaxInstances > 0 && grant.Scope.MaxInstances <= request.Scope.MaxInstances
		memoryFits := grant.Scope.MaxMemoryBytes > 0 && grant.Scope.MaxMemoryBytes <= request.Scope.MaxMemoryBytes
		return instancesFit && memoryFits
	}
	return len(grant.Scope.Modules) == 0 && grant.Scope.MaxInstances == 0 && grant.Scope.MaxMemoryBytes == 0
}

func authorityChange(old project.AuthorityRequest, current project.AuthorityRequest, grant *project.AuthorityGrant) string {
	if old.Name == "" {
		return "new"
	}
	if old.Mode != current.Mode || old.Reason != current.Reason || !sameProjectScope(old.Scope, current.Scope) {
		return "changed"
	}
	if grant == nil {
		return "unreviewed"
	}
	return ""
}

func sameProjectScope(left, right project.AuthorityScope) bool {
	return left.MaxInstances == right.MaxInstances && left.MaxMemoryBytes == right.MaxMemoryBytes && sameStringSets(left.Modules, right.Modules)
}

func sameStringSets(left, right []string) bool {
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

func contractKey(id string, major uint32) string { return fmt.Sprintf("%s@%d", id, major) }

func sortedMapValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return sortedUnique(result)
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func sortedReleaseKeys(values map[string]CatalogRelease) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortCatalogReleases(releases []CatalogRelease) {
	sort.SliceStable(releases, func(i, j int) bool {
		left, leftErr := semver.Parse(releases[i].Version)
		right, rightErr := semver.Parse(releases[j].Version)
		if leftErr == nil && rightErr == nil {
			if precedence := left.Compare(right); precedence != 0 {
				return precedence > 0
			}
		}
		if releases[i].Version != releases[j].Version {
			return releases[i].Version > releases[j].Version
		}
		if releases[i].ReleaseFingerprint != releases[j].ReleaseFingerprint {
			return releases[i].ReleaseFingerprint < releases[j].ReleaseFingerprint
		}
		return releases[i].DefinitionDigest < releases[j].DefinitionDigest
	})
}

func validSHA256(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, character := range strings.TrimPrefix(value, "sha256:") {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("multiple JSON values are not allowed")
}
