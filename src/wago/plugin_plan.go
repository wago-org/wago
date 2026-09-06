package wago

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/wago-org/wago/src/core/semver"
)

type plannedPlugin struct {
	provider  PluginProvider
	selection PluginSelection
	digest    string
	plugin    Plugin
	reg       *Registrar
}

type immutablePlan struct {
	ordered    []string
	providers  map[string]PluginProvider
	selections map[string]PluginSelection
	digests    map[string]string
}

// PluginPlan is a side-effect-free report built exclusively from immutable
// provider definitions and reviewed selections. No factory or plugin code runs.
type PluginPlan struct {
	Plugins []PluginPlanEntry `json:"plugins"`
}

type PluginPlanEntry struct {
	ID               string                `json:"id"`
	DefinitionDigest string                `json:"definitionDigest"`
	Grants           []AuthorityGrant      `json:"grants,omitempty"`
	Provides         []ContractSpec        `json:"provides,omitempty"`
	Consumes         []ContractRequirement `json:"consumes,omitempty"`
	Contracts        []ContractBinding     `json:"contracts,omitempty"`
}

// InspectPluginPlan verifies and resolves only immutable metadata. It never
// invokes New, ValidateConfig, Register, Start, or Stop.
func InspectPluginPlan(set PluginSet) (*PluginPlan, error) {
	plan, err := validateImmutablePlan(set)
	if err != nil {
		return nil, err
	}
	report := &PluginPlan{Plugins: make([]PluginPlanEntry, 0, len(plan.ordered))}
	for _, id := range plan.ordered {
		provider := plan.providers[id]
		selection := plan.selections[id]
		entry := PluginPlanEntry{
			ID: id, DefinitionDigest: plan.digests[id],
			Grants:    append([]AuthorityGrant(nil), selection.Grants...),
			Provides:  append([]ContractSpec(nil), provider.Definition.Provides...),
			Consumes:  append([]ContractRequirement(nil), provider.Definition.Consumes...),
			Contracts: append([]ContractBinding(nil), selection.Contracts...),
		}
		sort.Slice(entry.Grants, func(i, j int) bool { return entry.Grants[i].Name < entry.Grants[j].Name })
		sort.Slice(entry.Provides, func(i, j int) bool {
			if entry.Provides[i].ID == entry.Provides[j].ID {
				return entry.Provides[i].Major < entry.Provides[j].Major
			}
			return entry.Provides[i].ID < entry.Provides[j].ID
		})
		sort.Slice(entry.Consumes, func(i, j int) bool {
			if entry.Consumes[i].ID != entry.Consumes[j].ID {
				return entry.Consumes[i].ID < entry.Consumes[j].ID
			}
			if entry.Consumes[i].Major != entry.Consumes[j].Major {
				return entry.Consumes[i].Major < entry.Consumes[j].Major
			}
			return entry.Consumes[i].Mode < entry.Consumes[j].Mode
		})
		report.Plugins = append(report.Plugins, entry)
	}
	return report, nil
}

// ValidatePluginSet performs complete provider-side validation without
// committing contributions, activating handles, or starting plugins. Unlike
// InspectPluginPlan, this executes ValidateConfig, New, and Register so callers
// can verify that linked code matches its immutable definition.
func ValidatePluginSet(set PluginSet) error {
	immutable, err := validateImmutablePlan(set)
	if err != nil {
		return err
	}
	planned, err := registerPlan(immutable)
	if err != nil {
		return err
	}
	return validatePluginCommitConflicts(planned, nil, NoPluginOverrides)
}

// LoadPlugins validates, registers, and commits a complete PluginSet atomically,
// then starts it in dependency order. Startup failure rolls back in reverse order.
func (rt *Runtime) LoadPlugins(ctx context.Context, set PluginSet) error {
	if rt == nil {
		return fmt.Errorf("wago: nil runtime")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := rt.beginPluginLoad(); err != nil {
		return err
	}
	loading := true
	defer func() {
		if loading {
			rt.finishPluginLoad()
		}
	}()
	immutable, err := validateImmutablePlan(set)
	if err != nil {
		return err
	}
	planned, err := registerPlan(immutable)
	if err != nil {
		return err
	}
	if err := rt.commitPluginPlan(planned); err != nil {
		return err
	}
	if err := rt.startPluginPlan(ctx, planned); err != nil {
		rt.beginFailedPluginRollback()
		loading = false
		return errors.Join(err, rt.rollbackCommittedPluginPlan(ctx))
	}
	loading = false
	rt.finishPluginLoad()
	return nil
}

func (rt *Runtime) beginFailedPluginRollback() {
	rt.mu.Lock()
	if rt.state == runtimeLoading {
		rt.state = runtimeClosing
	}
	select {
	case <-rt.loadingDone:
	default:
		close(rt.loadingDone)
	}
	rt.stateCond.Broadcast()
	rt.mu.Unlock()
}

func (rt *Runtime) beginPluginLoad() error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.pluginsLoadAttempted {
		return fmt.Errorf("wago: plugins may be loaded at most once")
	}
	if rt.operational {
		return fmt.Errorf("wago: LoadPlugins must run before the first runtime operation")
	}
	if rt.state == runtimeClosing || rt.state == runtimeClosed {
		return fmt.Errorf("wago: LoadPlugins on a closed runtime")
	}
	if rt.state != runtimeReady {
		return fmt.Errorf("wago: plugin loading is already in progress")
	}
	rt.pluginsLoadAttempted = true
	rt.state = runtimeLoading
	rt.loadingDone = make(chan struct{})
	return nil
}

func (rt *Runtime) finishPluginLoad() {
	rt.mu.Lock()
	if rt.state == runtimeLoading {
		rt.state = runtimeReady
	}
	select {
	case <-rt.loadingDone:
	default:
		close(rt.loadingDone)
	}
	rt.stateCond.Broadcast()
	rt.mu.Unlock()
}

func validateImmutablePlan(set PluginSet) (*immutablePlan, error) {
	plan := &immutablePlan{
		providers:  make(map[string]PluginProvider, len(set.Providers)),
		selections: make(map[string]PluginSelection, len(set.Selections)),
		digests:    make(map[string]string, len(set.Selections)),
	}
	for i, provider := range set.Providers {
		id := provider.Definition.ID
		if id == "" {
			return nil, fmt.Errorf("wago: provider %d has an empty plugin ID", i)
		}
		if _, duplicate := plan.providers[id]; duplicate {
			return nil, fmt.Errorf("wago: duplicate provider for plugin %q", id)
		}
		frozen, err := freezeDefinition(provider.Definition)
		if err != nil {
			return nil, &PluginError{Plugin: id, Phase: PluginPhaseValidate, Err: err}
		}
		digest, err := definitionDigestCanonical(frozen)
		if err != nil {
			return nil, &PluginError{Plugin: id, Phase: PluginPhaseValidate, Err: err}
		}
		provider.Definition = frozen
		plan.providers[id], plan.digests[id] = provider, digest
	}
	for i, selection := range set.Selections {
		if selection.ID == "" {
			return nil, fmt.Errorf("wago: selection %d has an empty plugin ID", i)
		}
		if _, duplicate := plan.selections[selection.ID]; duplicate {
			return nil, fmt.Errorf("wago: duplicate selection for plugin %q", selection.ID)
		}
		provider, linked := plan.providers[selection.ID]
		if !linked {
			return nil, fmt.Errorf("wago: plugin %q is selected but not linked", selection.ID)
		}
		if selection.DefinitionDigest != plan.digests[selection.ID] {
			return nil, &PluginError{Plugin: selection.ID, Phase: PluginPhaseValidate, Path: "definitionDigest", Err: fmt.Errorf("linked definition digest %q does not match reviewed digest %q", plan.digests[selection.ID], selection.DefinitionDigest)}
		}
		if len(selection.Config) != 0 && !json.Valid(selection.Config) {
			return nil, &PluginError{Plugin: selection.ID, Phase: PluginPhaseConfigure, Path: "config", Err: fmt.Errorf("invalid JSON")}
		}
		if err := validateGrants(provider.Definition, selection.Grants); err != nil {
			return nil, &PluginError{Plugin: selection.ID, Phase: PluginPhaseAuthorize, Err: err}
		}
		if err := checkCompat(provider.Definition.Compatibility); err != nil {
			return nil, &PluginError{Plugin: selection.ID, Phase: PluginPhaseResolve, Err: err}
		}
		if err := validateReviewedDependencies(provider.Definition.Requires, selection.Dependencies); err != nil {
			return nil, &PluginError{Plugin: selection.ID, Phase: PluginPhaseResolve, Path: "dependencies", Err: err}
		}
		plan.selections[selection.ID] = freezeSelection(selection)
	}

	edges := make(map[string]map[string]struct{}, len(plan.selections))
	indegree := make(map[string]int, len(plan.selections))
	for id := range plan.selections {
		edges[id] = map[string]struct{}{}
	}
	addEdge := func(from, to string) {
		if _, exists := edges[from][to]; !exists {
			edges[from][to] = struct{}{}
			indegree[to]++
		}
	}
	for id := range plan.selections {
		def := plan.providers[id].Definition
		for _, requirement := range def.Requires {
			if requirement.Version == "" {
				return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("plugin %q dependency version constraint is empty", requirement.ID)}
			}
			dependency, ok := plan.providers[requirement.ID]
			if !ok {
				return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("requires missing plugin %q", requirement.ID)}
			}
			if _, selected := plan.selections[requirement.ID]; !selected {
				return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("requires unselected plugin %q", requirement.ID)}
			}
			constraint, err := semver.ParseConstraint(requirement.Version)
			version, versionErr := semver.Parse(dependency.Definition.Version)
			if err != nil || versionErr != nil || !constraint.Check(version) {
				return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("plugin %q version %q does not satisfy %q", requirement.ID, dependency.Definition.Version, requirement.Version)}
			}
			addEdge(requirement.ID, id)
		}
	}

	majors := map[string]map[uint32]struct{}{}
	exactProviders := map[string][]string{}
	for id := range plan.selections {
		for _, spec := range plan.providers[id].Definition.Provides {
			key := contractKeyString(spec)
			exactProviders[key] = append(exactProviders[key], id)
			if majors[spec.ID] == nil {
				majors[spec.ID] = map[uint32]struct{}{}
			}
			majors[spec.ID][spec.Major] = struct{}{}
		}
	}
	for key := range exactProviders {
		sort.Strings(exactProviders[key])
	}
	for id := range plan.selections {
		bindings := map[string]ContractBinding{}
		for _, binding := range plan.selections[id].Contracts {
			key := contractKeyString(ContractSpec{ID: binding.ID, Major: binding.Major})
			if _, duplicate := bindings[key]; duplicate {
				return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("duplicate contract binding %q", key)}
			}
			binding.Providers = append([]string(nil), binding.Providers...)
			bindings[key] = binding
		}
		for _, requirement := range plan.providers[id].Definition.Consumes {
			bindingKey := contractKeyString(ContractSpec{ID: requirement.ID, Major: requirement.Major})
			binding, ok := bindings[bindingKey]
			if !ok {
				return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("contract %q has no reviewed binding", bindingKey)}
			}
			owners := binding.Providers
			switch requirement.Mode {
			case ContractRequired:
				if len(owners) != 1 {
					if len(owners) == 0 {
						if len(exactProviders[bindingKey]) != 0 {
							return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("required contract %q must bind exactly one provider", bindingKey)}
						}
						if len(majors[requirement.ID]) != 0 {
							return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("contract %q major %d is incompatible with linked providers", requirement.ID, requirement.Major)}
						}
						return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("required contract %q major %d has no provider", requirement.ID, requirement.Major)}
					}
					return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("required contract %q must bind exactly one provider, got %d", bindingKey, len(owners))}
				}
			case ContractOptional:
				if len(owners) > 1 {
					return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("optional contract %q may bind at most one provider, got %d", bindingKey, len(owners))}
				}
			case ContractMany:
				if len(owners) != len(exactProviders[bindingKey]) {
					return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("many contract %q must bind every selected provider, got %d of %d", bindingKey, len(owners), len(exactProviders[bindingKey]))}
				}
			}
			seenOwners := make(map[string]struct{}, len(owners))
			for _, owner := range owners {
				if _, duplicate := seenOwners[owner]; duplicate {
					return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("contract %q binds provider %q more than once", bindingKey, owner)}
				}
				seenOwners[owner] = struct{}{}
				provider, selected := plan.providers[owner]
				if !selected {
					return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("contract %q binds unlinked provider %q", bindingKey, owner)}
				}
				if _, selected = plan.selections[owner]; !selected {
					return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("contract %q binds unselected provider %q", bindingKey, owner)}
				}
				provides := false
				for _, spec := range provider.Definition.Provides {
					if spec.ID == requirement.ID && spec.Major == requirement.Major {
						provides = true
						break
					}
				}
				if !provides {
					return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("contract %q binds provider %q which does not declare it", bindingKey, owner)}
				}
				addEdge(owner, id)
			}
			if requirement.Mode == ContractMany {
				for _, owner := range exactProviders[bindingKey] {
					if _, bound := seenOwners[owner]; !bound {
						return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("many contract %q omits selected provider %q", bindingKey, owner)}
					}
				}
			}
			delete(bindings, bindingKey)
		}
		if len(bindings) != 0 {
			return nil, &PluginError{Plugin: id, Phase: PluginPhaseResolve, Err: fmt.Errorf("selection contains undeclared contract binding")}
		}
	}
	if err := validateDirectReachability(plan); err != nil {
		return nil, err
	}
	ready := make([]string, 0, len(plan.selections))
	for id := range plan.selections {
		if indegree[id] == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	for len(ready) != 0 {
		id := ready[0]
		ready = ready[1:]
		plan.ordered = append(plan.ordered, id)
		nexts := make([]string, 0, len(edges[id]))
		for next := range edges[id] {
			nexts = append(nexts, next)
		}
		sort.Strings(nexts)
		for _, next := range nexts {
			indegree[next]--
			if indegree[next] == 0 {
				ready = append(ready, next)
				sort.Strings(ready)
			}
		}
	}
	if len(plan.ordered) != len(plan.selections) {
		return nil, fmt.Errorf("wago: plugin dependency/contract cycle")
	}
	return plan, nil
}

func freezeDefinition(def PluginDefinition) (PluginDefinition, error) {
	return canonicalPluginDefinition(def)
}

func freezeSelection(in PluginSelection) PluginSelection {
	out := in
	out.Config = append(json.RawMessage(nil), in.Config...)
	out.Dependencies = make(map[string]string, len(in.Dependencies))
	for id, constraint := range in.Dependencies {
		out.Dependencies[id] = constraint
	}
	out.Grants = append([]AuthorityGrant(nil), in.Grants...)
	for i := range out.Grants {
		out.Grants[i].Scope.Modules = append([]string(nil), in.Grants[i].Scope.Modules...)
	}
	out.Contracts = append([]ContractBinding(nil), in.Contracts...)
	for i := range out.Contracts {
		out.Contracts[i].Providers = append([]string(nil), in.Contracts[i].Providers...)
	}
	return out
}

func validateReviewedDependencies(required []PluginRequirement, reviewed map[string]string) error {
	if len(required) != len(reviewed) {
		return fmt.Errorf("reviewed dependency count %d does not match linked definition count %d", len(reviewed), len(required))
	}
	for _, requirement := range required {
		constraint, ok := reviewed[requirement.ID]
		if !ok {
			return fmt.Errorf("linked dependency %q is absent from the reviewed graph", requirement.ID)
		}
		if constraint != requirement.Version {
			return fmt.Errorf("dependency %q reviewed constraint %q does not match linked definition constraint %q", requirement.ID, constraint, requirement.Version)
		}
	}
	return nil
}

func validateDirectReachability(plan *immutablePlan) error {
	if len(plan.selections) == 0 {
		return nil
	}
	reachable := make(map[string]struct{}, len(plan.selections))
	var visit func(string)
	visit = func(id string) {
		if _, seen := reachable[id]; seen {
			return
		}
		reachable[id] = struct{}{}
		for _, requirement := range plan.providers[id].Definition.Requires {
			visit(requirement.ID)
		}
		for _, binding := range plan.selections[id].Contracts {
			for _, provider := range binding.Providers {
				visit(provider)
			}
		}
	}
	for id, selection := range plan.selections {
		if selection.Direct {
			visit(id)
		}
	}
	for _, id := range sortedSelectionIDs(plan.selections) {
		if _, ok := reachable[id]; !ok {
			return &PluginError{Plugin: id, Phase: PluginPhaseResolve, Path: "direct", Err: fmt.Errorf("selected plugin is unreachable from every reviewed direct root")}
		}
	}
	return nil
}

func sortedSelectionIDs(selections map[string]PluginSelection) []string {
	ids := make([]string, 0, len(selections))
	for id := range selections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func validateGrants(def PluginDefinition, grants []AuthorityGrant) error {
	requests := make(map[Authority]AuthorityRequest, len(def.Authorities))
	for _, request := range def.Authorities {
		requests[request.Name] = request
	}
	seen := map[Authority]struct{}{}
	for _, grant := range grants {
		if !validAuthority(grant.Name) {
			return fmt.Errorf("unknown authority grant %q", grant.Name)
		}
		if _, duplicate := seen[grant.Name]; duplicate {
			return fmt.Errorf("duplicate authority grant %q", grant.Name)
		}
		seen[grant.Name] = struct{}{}
		request, declared := requests[grant.Name]
		if !declared {
			return fmt.Errorf("grant %q was not requested", grant.Name)
		}
		if err := validateGrantScope(grant.Name, grant.Scope); err != nil {
			return err
		}
		if !scopeWithin(grant.Name, grant.Scope, request.Scope) {
			return fmt.Errorf("grant %q widens its request", grant.Name)
		}
	}
	return nil
}

func validateGrantScope(authority Authority, scope AuthorityScope) error {
	if authority == AuthorityInstanceManage || authority == AuthorityCoreInstanceInstantiate {
		if len(scope.Modules) != 0 || scope.MaxInstances == 0 || scope.MaxMemoryBytes == 0 {
			return fmt.Errorf("authority %q grant requires positive limits and no modules", authority)
		}
		return nil
	}
	return validateAuthorityScope(authority, scope)
}

func scopeWithin(authority Authority, grant, request AuthorityScope) bool {
	if grant.MaxInstances > request.MaxInstances && request.MaxInstances != 0 {
		return false
	}
	if grant.MaxMemoryBytes > request.MaxMemoryBytes && request.MaxMemoryBytes != 0 {
		return false
	}
	if len(grant.Modules) != 0 {
		allowed := map[string]struct{}{}
		for _, m := range request.Modules {
			allowed[m] = struct{}{}
		}
		for _, m := range grant.Modules {
			if _, ok := allowed[m]; !ok {
				return false
			}
		}
	}
	return true
}

func registerPlan(immutable *immutablePlan) ([]plannedPlugin, error) {
	planned := make([]plannedPlugin, 0, len(immutable.ordered))
	for _, id := range immutable.ordered {
		provider, selection := immutable.providers[id], immutable.selections[id]
		if provider.New == nil {
			return nil, &PluginError{Plugin: id, Phase: PluginPhaseRegister, Err: fmt.Errorf("nil plugin factory")}
		}
		if provider.ValidateConfig != nil {
			var configErr error
			panicErr := callSafely("plugin ValidateConfig", func() {
				configErr = provider.ValidateConfig(append(json.RawMessage(nil), selection.Config...))
			})
			if err := joinPrimary(configErr, panicErr); err != nil {
				return nil, &PluginError{Plugin: id, Phase: PluginPhaseConfigure, Path: "config", Err: err}
			}
		}
		var plugin Plugin
		if err := callSafely("plugin factory", func() { plugin = provider.New() }); err != nil {
			return nil, &PluginError{Plugin: id, Phase: PluginPhaseRegister, Err: err}
		}
		if plugin == nil {
			return nil, &PluginError{Plugin: id, Phase: PluginPhaseRegister, Err: fmt.Errorf("factory returned nil plugin")}
		}
		reg := newRegistrar(provider.Definition, selection)
		var registerErr error
		panicErr := callSafely("plugin Register", func() { registerErr = plugin.Register(reg) })
		if err := joinPrimary(registerErr, panicErr); err != nil {
			return nil, &PluginError{Plugin: id, Phase: PluginPhaseRegister, Err: err}
		}
		reg.sealed = true
		if err := validateRegistration(reg); err != nil {
			return nil, &PluginError{Plugin: id, Phase: PluginPhaseRegister, Err: err}
		}
		planned = append(planned, plannedPlugin{provider: provider, selection: selection, digest: immutable.digests[id], plugin: plugin, reg: reg})
	}
	if err := stageContracts(planned); err != nil {
		return nil, err
	}
	return planned, nil
}

func validateRegistration(reg *Registrar) error {
	for authority := range reg.used {
		if _, declared := reg.requests[authority]; !declared {
			return fmt.Errorf("exercised undeclared authority %q: %w", authority, ErrPermissionDenied)
		}
	}
	for _, imp := range reg.imports {
		if imp.fn == nil || imp.module == "" || imp.name == "" {
			return fmt.Errorf("invalid host import %q", imp.key())
		}
	}
	if len(reg.imports) != 0 {
		if _, ok := reg.used[AuthorityHostImportDefine]; !ok {
			return fmt.Errorf("host imports require authority %q: %w", AuthorityHostImportDefine, ErrPermissionDenied)
		}
	}
	if len(reg.managers) != 0 {
		if _, ok := reg.used[AuthorityInstanceManage]; !ok {
			return fmt.Errorf("managed instances require authority %q: %w", AuthorityInstanceManage, ErrPermissionDenied)
		}
	}
	if len(reg.customTypes) != 0 {
		if _, ok := reg.used[AuthorityCompilerTypeDefine]; !ok {
			return fmt.Errorf("custom types require authority %q: %w", AuthorityCompilerTypeDefine, ErrPermissionDenied)
		}
	}
	if len(reg.instructions) != 0 {
		if _, ok := reg.used[AuthorityCompilerInstructionDefine]; !ok {
			return fmt.Errorf("instructions require authority %q: %w", AuthorityCompilerInstructionDefine, ErrPermissionDenied)
		}
	}
	seenInstructions := map[string]struct{}{}
	for _, ins := range reg.instructions {
		key := ins.spec.Module + "." + ins.spec.Name
		if _, duplicate := seenInstructions[key]; duplicate {
			return fmt.Errorf("duplicate instruction %q: %w", key, ErrPluginConflict)
		}
		seenInstructions[key] = struct{}{}
		reg.imports = append(reg.imports, instructionImport(ins))
	}
	seenImports := map[string]struct{}{}
	for _, imp := range reg.imports {
		if _, duplicate := seenImports[imp.key()]; duplicate {
			return fmt.Errorf("duplicate host import %q: %w", imp.key(), ErrPluginConflict)
		}
		seenImports[imp.key()] = struct{}{}
	}
	declaredProvides := map[string]ContractSpec{}
	for _, spec := range reg.definition.Provides {
		declaredProvides[contractKeyString(spec)] = spec
	}
	actualProvides := map[string]int{}
	for _, provision := range reg.provides {
		actualProvides[contractKeyString(provision.spec)]++
	}
	if !equalContractCounts(declaredProvides, actualProvides) {
		return fmt.Errorf("actual provided contracts do not match definition")
	}
	declaredConsumes := map[string]ContractMode{}
	for _, req := range reg.definition.Consumes {
		declaredConsumes[contractKeyString(ContractSpec{ID: req.ID, Major: req.Major})] = req.Mode
	}
	actualConsumes := map[string]ContractMode{}
	for _, binder := range reg.consumes {
		key := contractKeyString(binder.contractSpec())
		if _, duplicate := actualConsumes[key]; duplicate {
			return fmt.Errorf("contract %q consumed more than once", key)
		}
		actualConsumes[key] = binder.contractMode()
	}
	if !reflect.DeepEqual(declaredConsumes, actualConsumes) {
		return fmt.Errorf("actual consumed contracts do not match definition")
	}
	return nil
}

func contractKeyString(spec ContractSpec) string { return fmt.Sprintf("%s@%d", spec.ID, spec.Major) }
func equalContractCounts(declared map[string]ContractSpec, actual map[string]int) bool {
	if len(declared) != len(actual) {
		return false
	}
	for key := range declared {
		if actual[key] != 1 {
			return false
		}
	}
	return true
}

func stageContracts(plan []plannedPlugin) error {
	providers := map[string]map[string]contractProvision{}
	for _, p := range plan {
		owner := p.provider.Definition.ID
		providers[owner] = map[string]contractProvision{}
		for _, provision := range p.reg.provides {
			providers[owner][contractKeyString(provision.spec)] = provision
		}
	}
	for _, p := range plan {
		for _, binder := range p.reg.consumes {
			key := contractKeyString(binder.contractSpec())
			binding, ok := contractBindingFor(p.selection, binder.contractSpec())
			if !ok {
				return &PluginError{Plugin: p.provider.Definition.ID, Phase: PluginPhaseResolve, Err: fmt.Errorf("contract %q has no reviewed binding", key)}
			}
			values := make([]any, 0, len(binding.Providers))
			for _, owner := range binding.Providers {
				provision, exists := providers[owner][key]
				if !exists {
					return &PluginError{Plugin: p.provider.Definition.ID, Phase: PluginPhaseResolve, Err: fmt.Errorf("contract %q provider %q did not register its declared provision", key, owner)}
				}
				if binder.contractType() != nil && !provision.typ.AssignableTo(binder.contractType()) {
					return &PluginError{Plugin: p.provider.Definition.ID, Phase: PluginPhaseResolve, Err: fmt.Errorf("contract %q type mismatch: provider has %v, consumer wants %v", binder.contractSpec().ID, provision.typ, binder.contractType())}
				}
				values = append(values, provision.value)
			}
			binder.contractSlotValue().values = append([]any(nil), values...)
		}
	}
	return nil
}

func contractBindingFor(selection PluginSelection, spec ContractSpec) (ContractBinding, bool) {
	for _, binding := range selection.Contracts {
		if binding.ID == spec.ID && binding.Major == spec.Major {
			return binding, true
		}
	}
	return ContractBinding{}, false
}

func pluginRunsFor(plan []plannedPlugin) []registeredPluginRun {
	runs := make([]registeredPluginRun, len(plan))
	for i := range plan {
		p := &plan[i]
		run := registeredPluginRun{
			name: p.provider.Definition.ID, lifecycle: p.reg.lifecycle,
			closeInstances: append([]func() error(nil), p.reg.closeInstances...),
			drainInstances: append([]func() error(nil), p.reg.drainInstances...),
			callbacks:      p.reg.callGate,
			handles:        append([]func() error(nil), p.reg.revoke...),
		}
		for _, binder := range p.reg.consumes {
			run.consumed = append(run.consumed, binder.contractSlotValue())
		}
		for j := range plan {
			consumer := &plan[j]
			for _, binder := range consumer.reg.consumes {
				binding, _ := contractBindingFor(consumer.selection, binder.contractSpec())
				for _, owner := range binding.Providers {
					if owner == run.name {
						run.provided = append(run.provided, binder.contractSlotValue())
						break
					}
				}
			}
		}
		runs[i] = run
	}
	return runs
}

func (rt *Runtime) commitPluginPlan(plan []plannedPlugin) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state != runtimeLoading {
		return fmt.Errorf("wago: plugin commit outside its load transaction")
	}
	if err := validatePluginCommitConflicts(plan, rt.instructions, rt.overridePolicy); err != nil {
		return err
	}
	rt.writableImportsLocked()
	needsInstructionABI := false
	hooks := rt.hooks.clone()
	for _, p := range plan {
		id := p.provider.Definition.ID
		needsInstructionABI = needsInstructionABI || len(p.reg.instructions) != 0
		for _, imp := range p.reg.imports {
			key := imp.key()
			rt.imports[key] = p.reg.callGate.wrap(imp.fn)
			rt.importMeta[key] = cloneRegisteredImport(imp)
			rt.importOwner[key] = id
			rt.moduleOwner[imp.module] = id
		}
		for _, cap := range p.reg.caps {
			if _, ok := rt.caps[cap.cap]; !ok {
				rt.capOrder = append(rt.capOrder, cap.cap)
			}
			rt.caps[cap.cap] = id
		}
		for _, ins := range p.reg.instructions {
			rt.instructions[ins.spec.Module+"."+ins.spec.Name] = ins
		}
		for _, binder := range p.reg.consumes {
			slot := binder.contractSlotValue()
			values := append([]any(nil), slot.values...)
			slot.values = nil
			if err := slot.activate(values); err != nil {
				return &PluginError{Plugin: id, Phase: PluginPhaseCommit, Err: err}
			}
		}
		hooks.appendGated(p.reg.hooks, p.reg.callGate)
		for _, activate := range p.reg.activate {
			activate(rt)
		}
		rt.plugins = append(rt.plugins, p.provider.Definition)
	}
	if needsInstructionABI {
		for _, imp := range instructionABIImports() {
			key := imp.key()
			rt.imports[key] = imp.fn
			rt.importMeta[key] = cloneRegisteredImport(imp)
			rt.importOwner[key] = "wago:core"
			rt.moduleOwner[imp.module] = "wago:core"
		}
	}
	rt.pluginRuns = pluginRunsFor(plan)
	// Publish only after every handle, contract, import, and manager used by the
	// complete generation has activated.
	rt.storeHooks(hooks)
	return nil
}

func validatePluginCommitConflicts(plan []plannedPlugin, existing map[string]*registeredInstruction, policy ImportOverridePolicy) error {
	moduleOwner, importOwner := map[string]string{}, map[string]string{}
	instructionOwner := map[string]string{}
	for _, p := range plan {
		id := p.provider.Definition.ID
		for _, imp := range p.reg.imports {
			if owner, exists := moduleOwner[imp.module]; exists && owner != id && policy != AllowTestOverrides {
				return &PluginError{Plugin: id, Phase: PluginPhaseCommit, Err: fmt.Errorf("import module %q already owned by plugin %q: %w", imp.module, owner, ErrPluginConflict)}
			}
			if owner, exists := importOwner[imp.key()]; exists && policy != AllowTestOverrides {
				return &PluginError{Plugin: id, Phase: PluginPhaseCommit, Err: fmt.Errorf("import %q already provided by plugin %q: %w", imp.key(), owner, ErrPluginConflict)}
			}
			moduleOwner[imp.module], importOwner[imp.key()] = id, id
		}
		for _, ins := range p.reg.instructions {
			key := ins.spec.Module + "." + ins.spec.Name
			if owner, exists := instructionOwner[key]; exists {
				return &PluginError{Plugin: id, Phase: PluginPhaseCommit, Err: fmt.Errorf("instruction %q already provided by plugin %q: %w", key, owner, ErrPluginConflict)}
			}
			if _, exists := existing[key]; exists {
				return &PluginError{Plugin: id, Phase: PluginPhaseCommit, Err: fmt.Errorf("instruction %q conflicts: %w", key, ErrPluginConflict)}
			}
			instructionOwner[key] = id
		}
	}
	return nil
}

func (rt *Runtime) startPluginPlan(ctx context.Context, plan []plannedPlugin) error {
	for i, p := range plan {
		rt.mu.Lock()
		rt.pluginRuns[i].shouldStop = true
		rt.mu.Unlock()
		if p.reg.lifecycle.Start != nil {
			var startErr error
			panicErr := callSafely("plugin Start", func() { startErr = p.reg.lifecycle.Start(ctx) })
			if err := joinPrimary(startErr, panicErr); err != nil {
				return &PluginError{Plugin: p.provider.Definition.ID, Phase: PluginPhaseStart, Err: err}
			}
		}
	}
	return nil
}

func validateOpenSource(def PluginDefinition) error {
	if strings.TrimSpace(def.Provenance.Repository) == "" {
		return fmt.Errorf("plugin must declare source repository")
	}
	if err := validateProvenanceURL(def.Provenance.Repository, false); err != nil {
		return fmt.Errorf("plugin repository %q must be absolute https URL", def.Provenance.Repository)
	}
	if def.Provenance.Homepage != "" {
		if err := validateProvenanceURL(def.Provenance.Homepage, true); err != nil {
			return fmt.Errorf("plugin homepage %q must be absolute https URL", def.Provenance.Homepage)
		}
	}
	if strings.TrimSpace(def.Provenance.License) == "" {
		return fmt.Errorf("plugin must declare SPDX license")
	}
	return nil
}

func validateProvenanceURL(value string, allowFragment bool) error {
	if value != strings.TrimSpace(value) || !strings.HasPrefix(value, "https://") {
		return fmt.Errorf("invalid HTTPS URL")
	}
	rest := strings.TrimPrefix(value, "https://")
	authority := rest
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	if authority == "" || strings.Contains(authority, "@") || strings.Contains(value, "\\") || !allowFragment && strings.Contains(value, "#") {
		return fmt.Errorf("invalid HTTPS URL")
	}
	for _, char := range value {
		if char <= ' ' || char == 0x7f {
			return fmt.Errorf("invalid HTTPS URL")
		}
	}
	return nil
}
