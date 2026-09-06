package plugin

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/internal/tui"
)

type authoritySelection struct {
	keys []string
}

func authorityReviewSelector(reviews []AuthorityReview, choices map[string]bool) (*tui.PermissionForm, []authoritySelection) {
	byAuthority := map[string][]AuthorityReview{}
	for _, review := range reviews {
		byAuthority[review.Request.Name] = append(byAuthority[review.Request.Name], review)
	}
	authorities := make([]string, 0, len(byAuthority))
	for authority := range byAuthority {
		authorities = append(authorities, authority)
	}
	sort.Strings(authorities)

	items := make([]tui.SelectItem, 0, len(authorities)+1)
	selections := make([]authoritySelection, 0, len(authorities))
	for _, authority := range authorities {
		group := byAuthority[authority]
		sort.Slice(group, func(i, j int) bool { return group[i].PluginID < group[j].PluginID })
		selection := authoritySelection{}
		requesters := make([]string, 0, len(group))
		children := make([]tui.SelectItem, 0, len(group))
		pluginRequires := true
		allSelected, anySelected := true, false
		for _, review := range group {
			key := authorityKey(review.PluginID, review.Request.Name)
			selection.keys = append(selection.keys, key)
			requesters = append(requesters, shortPluginID(review.PluginID))
			required := review.Request.Mode == project.AuthorityRequired
			pluginRequires = pluginRequires && required
			selected := required || choices[key]
			allSelected = allSelected && selected
			anySelected = anySelected || selected
			children = append(children, tui.SelectItem{
				Label: authorityLabel(shortPluginID(review.PluginID), required), On: selected,
				Description: authorityPackageDetail(review),
			})
		}
		sort.Strings(requesters)
		requesters = compactStrings(requesters)
		description := "used by: " + strings.Join(requesters, " · ")
		if reason := firstAuthorityReason(group); reason != "" {
			description = reason + "\n" + description
		}
		if scope := sharedAuthorityScope(group); scope != "" {
			description += "\n" + scope
		}
		items = append(items, tui.SelectItem{
			Label: authorityLabel(authority, pluginRequires), Description: description, On: allSelected, Partial: anySelected && !allSelected, Children: children,
		})
		selections = append(selections, selection)
	}
	items = append(items, tui.SelectItem{Label: "Cancel installation", Description: "make no changes", Cancel: true})
	return tui.NewPermissionForm(authorityReviewTitle(reviews), items), selections
}

func authorityLabel(name string, required bool) string {
	if required {
		return name + " (required)"
	}
	return name
}

func authorityReviewTitle(reviews []AuthorityReview) string {
	direct := map[string]bool{}
	all := map[string]bool{}
	for _, review := range reviews {
		id := strings.TrimPrefix(review.PluginID, "github.com/")
		all[id] = true
		if review.Direct {
			direct[id] = true
		}
	}
	if len(direct) == 1 {
		for id := range direct {
			return "Permissions for " + id
		}
	}
	if len(all) == 1 {
		for id := range all {
			return "Permissions for " + id
		}
	}
	return "Permissions"
}

func authorityPackageDetail(review AuthorityReview) string {
	if scope := sharedAuthorityScope([]AuthorityReview{review}); scope != "" {
		return scope
	}
	if review.Request.Mode == project.AuthorityRequired {
		return "required"
	}
	return "optional"
}

func reviewAuthorityChoices(reviews []AuthorityReview, choices map[string]bool) error {
	return reviewAuthorityChoicesWithTitle(reviews, choices, "")
}

func reviewAuthorityChoicesWithTitle(reviews []AuthorityReview, choices map[string]bool, title string) error {
	selector, selections := authorityReviewSelector(reviews, choices)
	if title != "" {
		selector.Title = title
	}
	submitted, cancelled := tui.Run(selector)
	if !submitted || cancelled || selector.Rejected() {
		return fmt.Errorf("authority review cancelled; no changes were made")
	}
	applyAuthoritySelectionChoices(&selector.MultiSelect, selections, choices)
	return nil
}

func applyAuthoritySelectionChoices(selector *tui.MultiSelect, selections []authoritySelection, choices map[string]bool) {
	for index, selection := range selections {
		for child, key := range selection.keys {
			choices[key] = selector.Items[index].Children[child].On
		}
	}
}

func authorityExitItems() []tui.Item {
	return []tui.Item{
		{Label: "No", Value: "continue"},
		{Label: "Yes", Value: "cancel"},
	}
}

func shortPluginID(id string) string {
	return strings.TrimPrefix(id, "github.com/wago-org/")
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func firstAuthorityReason(reviews []AuthorityReview) string {
	for _, review := range reviews {
		if review.Request.Reason != "" {
			return review.Request.Reason
		}
	}
	return ""
}

func sharedAuthorityScope(reviews []AuthorityReview) string {
	if len(reviews) != 1 {
		return ""
	}
	scope := reviews[0].Request.Scope
	var details []string
	if len(scope.Modules) != 0 {
		details = append(details, "modules: "+strings.Join(scope.Modules, ", "))
	}
	if scope.MaxInstances != 0 || scope.MaxMemoryBytes != 0 {
		details = append(details, "limit: "+formatAuthorityLimits(scope.MaxInstances, scope.MaxMemoryBytes))
	}
	return strings.Join(details, " · ")
}

func formatReviewPlan(plan ResolutionPlan) string {
	var output strings.Builder
	authorities := map[string]bool{}
	plugins := map[string]bool{}
	var maxInstances uint32
	var maxMemory uint64
	for _, review := range plan.Reviews {
		authorities[review.Request.Name] = true
		if review.Request.Scope.MaxInstances > maxInstances {
			maxInstances = review.Request.Scope.MaxInstances
		}
		if review.Request.Scope.MaxMemoryBytes > maxMemory {
			maxMemory = review.Request.Scope.MaxMemoryBytes
		}
	}
	for id := range plan.Lock.Plugins {
		plugins[id] = true
	}
	fmt.Fprintf(&output, "%s\n", bold("Security review"))
	fmt.Fprintln(&output, "  Native code       Yes")
	fmt.Fprintln(&output, "  OS sandbox        No")
	fmt.Fprintf(&output, "  Plugins           %d\n", len(plugins))
	fmt.Fprintf(&output, "  Authorities       %d distinct · %d requests\n", len(authorities), len(plan.Reviews))
	if maxInstances != 0 || maxMemory != 0 {
		fmt.Fprintf(&output, "  Limits            %s\n", formatAuthorityLimits(maxInstances, maxMemory))
	}
	fmt.Fprintln(&output, "  Plugins run native code in Wago; grants restrict Wago APIs, not the OS.")
	if len(plan.ContractReviews) != 0 {
		fmt.Fprintf(&output, "\n%s\n", bold("Contract"))
		for _, review := range plan.ContractReviews {
			fmt.Fprintf(&output, "  %s@%d → %s\n", shortPluginID(review.Request.ID), review.Request.Major, joinedOrNone(shortPluginIDs(review.Proposed)))
		}
	}
	return output.String()
}

func formatAuthorityLimits(instances uint32, memory uint64) string {
	var values []string
	if instances != 0 {
		values = append(values, fmt.Sprintf("%d instances", instances))
	}
	if memory != 0 {
		values = append(values, formatAuthorityBytes(memory)+" memory")
	}
	return strings.Join(values, " · ")
}

func formatAuthorityBytes(value uint64) string {
	for _, unit := range []struct {
		name  string
		value uint64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}} {
		if value >= unit.value && value%unit.value == 0 {
			return fmt.Sprintf("%d %s", value/unit.value, unit.name)
		}
	}
	return fmt.Sprintf("%d B", value)
}

func shortPluginIDs(ids []string) []string {
	short := make([]string, len(ids))
	for index, id := range ids {
		short[index] = shortPluginID(id)
	}
	return short
}

func joinedOrNone(values []string) string {
	if value := strings.Join(values, ", "); value != "" {
		return value
	}
	return "none"
}

func reviewContractChoices(plan *ResolutionPlan) error {
	for index := range plan.ContractReviews {
		review := &plan.ContractReviews[index]
		var values []string
		switch review.Request.Mode {
		case "required":
			if len(review.Available) <= 1 {
				continue
			}
			values = append(values, review.Available...)
		case "optional":
			values = append(values, "")
			values = append(values, review.Available...)
		case "many":
			continue
		default:
			return fmt.Errorf("plugin %s has unknown contract mode %q", review.PluginID, review.Request.Mode)
		}
		items := make([]tui.Item, 0, len(values))
		cursor := 0
		for valueIndex, value := range values {
			label, description := value, "bind this provider"
			if value == "" {
				label, description = "No provider", "leave this optional contract absent"
			}
			items = append(items, tui.Item{Label: label, Description: description, Value: value + "\x00"})
			if len(review.Proposed) == 0 && value == "" || len(review.Proposed) == 1 && review.Proposed[0] == value {
				cursor = valueIndex
			}
		}
		picker := tui.NewPicker(fmt.Sprintf("Bind %s contract %s@%d", review.PluginID, review.Request.ID, review.Request.Major), items)
		picker.SetCursor(cursor)
		submitted, cancelled := tui.Run(picker)
		if !submitted || cancelled {
			return fmt.Errorf("contract review cancelled; no changes were made")
		}
		selected := strings.TrimSuffix(picker.Selected(), "\x00")
		if selected == "" {
			review.Proposed = []string{}
		} else {
			review.Proposed = []string{selected}
		}
		entry := plan.Lock.Plugins[review.PluginID]
		for bindingIndex := range entry.Bindings {
			binding := &entry.Bindings[bindingIndex]
			if binding.ID == review.Request.ID && binding.Major == review.Request.Major {
				binding.Providers = append([]string(nil), review.Proposed...)
				break
			}
		}
		plan.Lock.Plugins[review.PluginID] = entry
	}
	return nil
}

func authorityKey(pluginID, authority string) string { return pluginID + "\x00" + authority }

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func pkgGrant(name string, useGlobal bool, requested []string, grantAll, denyAll bool, scopes map[string]map[string]project.AuthorityScope) {
	selection := capturePluginRuntime()
	src, err := selection.depsSource(useGlobal)
	if err != nil {
		fatal("plugin grant: %v", err)
	}
	ctx := context.Background()
	var warnings []string
	err = withPluginMutationLock(ctx, src, func(mutation *project.Mutation) error {
		manifest, readErr := mutation.ReadManifest()
		if readErr != nil {
			return readErr
		}
		lock, readErr := mutation.ReadLock()
		if readErr != nil {
			return fmt.Errorf("%w; remove %s, then rerun `wago add` to rebuild the reviewed lock graph", readErr, project.LockPath(src))
		}
		id, resolveErr := selectGrantPlugin(name, lock)
		if resolveErr != nil {
			return resolveErr
		}
		targets, targetErr := grantPluginTargets(id, lock)
		if targetErr != nil {
			return targetErr
		}
		reviewed, editErr := reviewAuthorityGrantTargets(lock, targets, requested, grantAll, denyAll, scopes, "Permissions for "+strings.TrimPrefix(id, "github.com/"))
		if editErr != nil {
			return editErr
		}
		lock, warnings = reviewed.Lock, reviewed.Warnings
		buildDir, err := selection.buildDirFor(useGlobal)
		if err != nil {
			return err
		}
		return stageAndPublishLockedState(mutation, src, buildDir, manifest, lock, false, selection.config())
	})
	if err != nil {
		fatal("plugin grant: %v", err)
	}
	for _, warning := range warnings {
		fmt.Printf("warning: %s\n", warning)
	}
	fmt.Printf("%s updated reviewed authority grants\n", cyan("✓"))
}

func printPluginPlanWarnings(warnings []string) {
	for _, warning := range warnings {
		fmt.Printf("warning: %s\n", warning)
	}
}

func unmetPluginRequirementWarnings(lock project.LockDocument) []string {
	ids := make([]string, 0, len(lock.Plugins))
	for id := range lock.Plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var warnings []string
	for _, id := range ids {
		entry := lock.Plugins[id]
		granted := map[string]bool{}
		for _, grant := range entry.Grants {
			granted[grant.Name] = true
		}
		var missing []string
		for _, request := range entry.RequestedAuthorities {
			if request.Mode == project.AuthorityRequired && !granted[request.Name] {
				missing = append(missing, request.Name)
			}
		}
		if len(missing) != 0 {
			warnings = append(warnings, fmt.Sprintf("%s may not work: plugin requires %s, but %s disabled", shortPluginID(id), strings.Join(missing, ", "), pluralGrant(len(missing))))
		}
	}
	return warnings
}

func pluralGrant(count int) string {
	if count == 1 {
		return "its grant is"
	}
	return "their grants are"
}

func editAuthorityGrants(lock project.LockDocument, id string, requested []string, grantAll, denyAll bool, scopes map[string]map[string]project.AuthorityScope) (project.LockDocument, error) {
	return editAuthorityGrantTargets(lock, []string{id}, requested, grantAll, denyAll, scopes, "")
}

func editAuthorityGrantTargets(lock project.LockDocument, ids, requested []string, grantAll, denyAll bool, scopes map[string]map[string]project.AuthorityScope, title string) (project.LockDocument, error) {
	reviewed, err := reviewAuthorityGrantTargets(lock, ids, requested, grantAll, denyAll, scopes, title)
	return reviewed.Lock, err
}

func reviewAuthorityGrantTargets(lock project.LockDocument, ids, requested []string, grantAll, denyAll bool, scopes map[string]map[string]project.AuthorityScope, title string) (reviewedPluginPlan, error) {
	targets := make(map[string]bool, len(ids))
	for _, id := range ids {
		if _, ok := lock.Plugins[id]; !ok {
			return reviewedPluginPlan{}, fmt.Errorf("plugin %q is not in the lock graph", id)
		}
		targets[id] = true
	}
	for pluginID := range scopes {
		if !targets[pluginID] {
			return reviewedPluginPlan{}, fmt.Errorf("plugin grant cannot apply --scopes entry that targets plugin %s", pluginID)
		}
	}
	allowed := map[string]project.AuthorityRequest{}
	for id := range targets {
		for _, request := range lock.Plugins[id].RequestedAuthorities {
			allowed[request.Name] = request
		}
	}
	for _, authority := range requested {
		if _, ok := allowed[authority]; !ok {
			return reviewedPluginPlan{}, fmt.Errorf("selected plugins do not request authority %q", authority)
		}
	}
	choices := map[string]bool{}
	reviews := []AuthorityReview{}
	for id := range targets {
		entry := lock.Plugins[id]
		granted := map[string]bool{}
		for _, grant := range entry.Grants {
			granted[grant.Name] = true
		}
		for _, request := range entry.RequestedAuthorities {
			key := authorityKey(id, request.Name)
			choices[key] = granted[request.Name]
			switch {
			case grantAll:
				choices[key] = true
			case denyAll:
				choices[key] = false
			case requested != nil:
				choices[key] = containsString(requested, request.Name)
			}
			reviews = append(reviews, AuthorityReview{PluginID: id, Request: request})
		}
	}
	if requested == nil && !grantAll && !denyAll && len(scopes) == 0 && !automation.NoInput() {
		if err := reviewAuthorityChoicesWithTitle(reviews, choices, title); err != nil {
			return reviewedPluginPlan{}, err
		}
	}
	return (pluginPlanReview{
		lock: lock, reviews: reviews, choices: choices, scopes: scopes, targets: targets,
		applyAuthorities: true, resetSelectedScopes: grantAll,
	}).finish()
}

func grantPluginTargets(id string, lock project.LockDocument) ([]string, error) {
	entry, ok := lock.Plugins[id]
	if !ok {
		return nil, fmt.Errorf("plugin %q is not in the lock graph", id)
	}
	if len(entry.RequestedAuthorities) != 0 {
		return []string{id}, nil
	}
	seen, targets := map[string]bool{}, []string{}
	var visit func(string)
	visit = func(current string) {
		if seen[current] {
			return
		}
		seen[current] = true
		entry := lock.Plugins[current]
		if len(entry.RequestedAuthorities) != 0 {
			targets = append(targets, current)
		}
		dependencies := make([]string, 0, len(entry.Dependencies))
		for dependency := range entry.Dependencies {
			dependencies = append(dependencies, dependency)
		}
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			if _, ok := lock.Plugins[dependency]; ok {
				visit(dependency)
			}
		}
	}
	visit(id)
	if len(targets) == 0 {
		return nil, fmt.Errorf("plugin %q and its installed dependencies do not request any authorities", id)
	}
	return targets, nil
}

func applyAuthorityScopeOverrides(lock *project.LockDocument, overrides map[string]map[string]project.AuthorityScope) error {
	if len(overrides) == 0 {
		return nil
	}
	pluginIDs := make([]string, 0, len(overrides))
	for pluginID := range overrides {
		pluginIDs = append(pluginIDs, pluginID)
	}
	sort.Strings(pluginIDs)
	for _, pluginID := range pluginIDs {
		entry, ok := lock.Plugins[pluginID]
		if !ok {
			return fmt.Errorf("--scopes targets plugin %q, which is not in the resolved lock graph", pluginID)
		}
		requests := map[string]project.AuthorityRequest{}
		for _, request := range entry.RequestedAuthorities {
			requests[request.Name] = request
		}
		grants := map[string]int{}
		for index, grant := range entry.Grants {
			grants[grant.Name] = index
		}
		authorities := make([]string, 0, len(overrides[pluginID]))
		for authority := range overrides[pluginID] {
			authorities = append(authorities, authority)
		}
		sort.Strings(authorities)
		for _, authority := range authorities {
			request, ok := requests[authority]
			if !ok {
				return fmt.Errorf("plugin %s does not request authority %q", pluginID, authority)
			}
			index, granted := grants[authority]
			if !granted {
				return fmt.Errorf("plugin %s authority %q is not granted; select it with --allow or --allow-all before setting its scope", pluginID, authority)
			}
			scope, err := validateAuthorityScopeOverride(request, overrides[pluginID][authority])
			if err != nil {
				return fmt.Errorf("plugin %s authority %q: %w", pluginID, authority, err)
			}
			entry.Grants[index].Scope = scope
		}
		lock.Plugins[pluginID] = entry
	}
	return nil
}

func validateAuthorityScopeOverride(request project.AuthorityRequest, scope project.AuthorityScope) (project.AuthorityScope, error) {
	switch {
	case len(request.Scope.Modules) != 0:
		if len(scope.Modules) == 0 || scope.MaxInstances != 0 || scope.MaxMemoryBytes != 0 {
			return project.AuthorityScope{}, fmt.Errorf("requires at least one exact module and no instance or memory limits")
		}
		allowed, seen := map[string]bool{}, map[string]bool{}
		for _, module := range request.Scope.Modules {
			allowed[module] = true
		}
		for _, module := range scope.Modules {
			if module == "" || module != strings.TrimSpace(module) || strings.Contains(module, "*") || seen[module] {
				return project.AuthorityScope{}, fmt.Errorf("contains invalid or duplicate exact module %q", module)
			}
			if !allowed[module] {
				return project.AuthorityScope{}, fmt.Errorf("module %q is outside the requested scope", module)
			}
			seen[module] = true
		}
		canonical := append([]string(nil), scope.Modules...)
		sort.Strings(canonical)
		return project.AuthorityScope{Modules: canonical}, nil
	case request.Scope.MaxInstances != 0 || request.Scope.MaxMemoryBytes != 0:
		if len(scope.Modules) != 0 || scope.MaxInstances == 0 || scope.MaxMemoryBytes == 0 {
			return project.AuthorityScope{}, fmt.Errorf("requires positive maxInstances and maxMemoryBytes and no modules")
		}
		if scope.MaxInstances > request.Scope.MaxInstances || scope.MaxMemoryBytes > request.Scope.MaxMemoryBytes {
			return project.AuthorityScope{}, fmt.Errorf("limits are outside the requested scope maxInstances=%d maxMemoryBytes=%d", request.Scope.MaxInstances, request.Scope.MaxMemoryBytes)
		}
		return scope, nil
	default:
		return project.AuthorityScope{}, fmt.Errorf("does not have a configurable scope")
	}
}

func selectGrantPlugin(name string, lock project.LockDocument) (string, error) {
	name = project.ExpandGitHubPluginID(name)
	if name != "" {
		if err := project.ValidatePluginID(name); err != nil {
			return "", err
		}
		if _, ok := lock.Plugins[name]; !ok {
			return "", fmt.Errorf("plugin %q is not in the lock graph", name)
		}
		return name, nil
	}
	if automation.NoInput() {
		return "", fmt.Errorf("plugin grant requires a plugin ID with --no-input")
	}
	if len(lock.Plugins) == 0 {
		return "", fmt.Errorf("no installed plugins have reviewed authority grants")
	}
	picker := grantPluginPicker(lock)
	submitted, cancelled := tui.Run(picker)
	if !submitted || cancelled {
		return "", fmt.Errorf("plugin grant cancelled; no changes were made")
	}
	return picker.Selected(), nil
}

func grantPluginPicker(lock project.LockDocument) *tui.Picker {
	ids := make([]string, 0, len(lock.Plugins))
	for id := range lock.Plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	items := make([]tui.Item, 0, len(ids))
	for _, id := range ids {
		entry := lock.Plugins[id]
		word := "authorities"
		if len(entry.RequestedAuthorities) == 1 {
			word = "authority"
		}
		items = append(items, tui.Item{
			Label: shortPluginID(id), Value: id,
			Description: fmt.Sprintf("%d requested %s", len(entry.RequestedAuthorities), word),
		})
	}
	return tui.NewPicker("Choose plugin grants", items)
}
