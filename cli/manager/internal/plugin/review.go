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

func reviewResolution(plan ResolutionPlan, options pkgOpts) (project.LockDocument, error) {
	if len(plan.Reviews) == 0 && len(plan.ContractReviews) == 0 && len(options.scopes) == 0 {
		return plan.Lock, nil
	}
	keys, optionalKeys := map[string]bool{}, map[string]bool{}
	requestedNames := map[string]bool{}
	for _, review := range plan.Reviews {
		key := authorityKey(review.PluginID, review.Request.Name)
		keys[key] = review.Request.Mode == project.AuthorityRequired || review.Previous != nil
		if review.Request.Mode == project.AuthorityOptional {
			optionalKeys[key] = true
		}
		requestedNames[review.Request.Name] = true
	}
	explicit := options.authorities != nil || options.grantAll || options.denyAll
	switch {
	case options.grantAll:
		for key := range optionalKeys {
			keys[key] = true
		}
	case options.denyAll:
		for key := range optionalKeys {
			keys[key] = false
		}
	case options.authorities != nil:
		for _, name := range options.authorities {
			if !requestedNames[name] {
				return project.LockDocument{}, fmt.Errorf("no reviewed plugin requests authority %q", name)
			}
		}
		for _, review := range plan.Reviews {
			if review.Request.Mode == project.AuthorityOptional {
				keys[authorityKey(review.PluginID, review.Request.Name)] = containsString(options.authorities, review.Request.Name)
			}
		}
	}
	if automation.NoInput() {
		hasOptionalReview := len(optionalKeys) != 0
		if len(plan.Reviews) != 0 && hasOptionalReview && !explicit {
			return project.LockDocument{}, fmt.Errorf("--no-input requires --allow, --allow-all, or --deny-all for optional authority review")
		}
		if len(plan.Reviews) != 0 && !hasOptionalReview && !explicit && len(options.scopes) == 0 {
			return project.LockDocument{}, fmt.Errorf("--no-input requires --allow, --allow-all, --deny-all, or --scopes for authority review")
		}
		if len(plan.ContractReviews) != 0 && !options.acceptContracts {
			return project.LockDocument{}, fmt.Errorf("--no-input requires --accept-contracts for exact contract-binding review")
		}
	}
	interactiveAuthorities := len(plan.Reviews) != 0 && !explicit && !automation.NoInput()
	interactiveContracts := len(plan.ContractReviews) != 0 && !options.acceptContracts && !automation.NoInput()
	if interactiveAuthorities || interactiveContracts {
		fmt.Printf("\n%s\n", formatReviewPlan(plan))
		if interactiveContracts {
			if err := reviewContractChoices(&plan); err != nil {
				return project.LockDocument{}, err
			}
		}
		if interactiveAuthorities {
			if err := reviewAuthorityChoices(plan.Reviews, keys); err != nil {
				return project.LockDocument{}, err
			}
		} else if interactiveContracts {
			selector := &tui.MultiSelect{Title: "Approve this reviewed plugin plan", Prompt: "enter accept · space decline · esc cancel", Items: []tui.SelectItem{{Label: "Apply this plan", Description: "publish the staged manifest, lock graph, and runtime together", On: true}}}
			_, cancelled := tui.Run(selector)
			if cancelled || !selector.Items[0].On {
				return project.LockDocument{}, fmt.Errorf("plugin review cancelled; no changes were made")
			}
		}
	}
	if len(plan.Reviews) != 0 {
		applyReviewedAuthorities(&plan.Lock, keys)
	}
	if err := applyAuthorityScopeOverrides(&plan.Lock, options.scopes); err != nil {
		return project.LockDocument{}, err
	}
	if err := project.ValidateLock(plan.Lock); err != nil {
		return project.LockDocument{}, err
	}
	return plan.Lock, nil
}

func authorityReviewSelector(reviews []AuthorityReview, choices map[string]bool) (*tui.MultiSelect, []string, map[string]bool) {
	pluginIDs := map[string]bool{}
	for _, review := range reviews {
		pluginIDs[review.PluginID] = true
	}
	showPluginID := len(pluginIDs) > 1
	title := "Authority grants"
	if len(pluginIDs) == 1 {
		for pluginID := range pluginIDs {
			title += " · " + pluginID
		}
	}
	items := make([]tui.SelectItem, 0, len(reviews)+1)
	itemKeys := make([]string, 0, len(reviews))
	requiredKeys := map[string]bool{}
	for _, review := range reviews {
		key := authorityKey(review.PluginID, review.Request.Name)
		label := review.Request.Name
		description := "(" + string(review.Request.Mode) + ")"
		if review.Request.Reason != "" {
			description += " " + review.Request.Reason
		}
		if review.Request.Mode == project.AuthorityRequired {
			description += "; deselecting cancels installation"
			requiredKeys[key] = true
		}
		if scope := projectScopeSuffix(review.Request.Scope); scope != "" {
			description += scope
		}
		group := ""
		if showPluginID {
			group = review.PluginID
		}
		items = append(items, tui.SelectItem{
			Label: label, Description: description, On: choices[key], Group: group,
			ConfirmOff: review.Request.Mode == project.AuthorityRequired,
		})
		itemKeys = append(itemKeys, key)
	}
	items = append(items, tui.SelectItem{
		Label: "Reject all", Description: "cancel installation and make no changes", Reject: true,
	})
	return &tui.MultiSelect{
		Title: title, Prompt: "space toggle · enter confirm · r reject all · esc cancel", Items: items,
	}, itemKeys, requiredKeys
}

func reviewAuthorityChoices(reviews []AuthorityReview, choices map[string]bool) error {
	selector, itemKeys, requiredKeys := authorityReviewSelector(reviews, choices)
	for {
		submitted, cancelled := tui.Run(selector)
		if !submitted || cancelled {
			return fmt.Errorf("authority review cancelled; no changes were made")
		}
		missingRequired := requiredAuthorityDeselected(selector, itemKeys, requiredKeys)
		if selector.Rejected() || missingRequired {
			title := "Exit installation?"
			backDescription := "return to authority review"
			cancelDescription := "reject all grants and make no changes"
			if missingRequired && !selector.Rejected() {
				backDescription = "restore required authorities and return to the list"
				cancelDescription = "required authorities cannot be denied"
			}
			selection, ok := tui.Choose(title, authorityRejectionItems(backDescription, cancelDescription))
			if ok && selection == "cancel" {
				return fmt.Errorf("authority review rejected; no changes were made")
			}
			for index, key := range itemKeys {
				if requiredKeys[key] {
					selector.Items[index].On = true
				}
			}
			for index := range selector.Items {
				if selector.Items[index].Reject {
					selector.Items[index].On = false
				}
			}
			continue
		}
		for index, key := range itemKeys {
			choices[key] = selector.Items[index].On
		}
		return nil
	}
}

func authorityRejectionItems(backDescription, cancelDescription string) []tui.Item {
	return []tui.Item{
		{Label: "No", Description: backDescription, Value: "continue"},
		{Label: "Yes", Description: cancelDescription, Value: "cancel"},
	}
}

func requiredAuthorityDeselected(selector *tui.MultiSelect, itemKeys []string, requiredKeys map[string]bool) bool {
	for index, key := range itemKeys {
		if requiredKeys[key] && !selector.Items[index].On {
			return true
		}
	}
	return false
}

type pluginReviewGroup struct {
	id        string
	contracts []ContractReview
}

func formatReviewPlan(plan ResolutionPlan) string {
	groups := make([]pluginReviewGroup, 0)
	indexes := make(map[string]int)
	group := func(id string) *pluginReviewGroup {
		if index, ok := indexes[id]; ok {
			return &groups[index]
		}
		indexes[id] = len(groups)
		groups = append(groups, pluginReviewGroup{id: id})
		return &groups[len(groups)-1]
	}
	for _, review := range plan.ContractReviews {
		current := group(review.PluginID)
		current.contracts = append(current.contracts, review)
	}

	var output strings.Builder
	fmt.Fprintf(&output, "%s\n", bold("Plugin security"))
	fmt.Fprintln(&output, "  Plugins run native code inside this Wago process.")
	fmt.Fprintln(&output, "  Authority grants constrain Wago interfaces; they are not an OS sandbox.")
	for _, group := range groups {
		fmt.Fprintf(&output, "\n%s\n", cyan(group.id))
		if len(group.contracts) != 0 {
			fmt.Fprintf(&output, "  %s\n", bold("Contracts"))
			for _, review := range group.contracts {
				fmt.Fprintf(&output, "    %s@%d  %s\n", review.Request.ID, review.Request.Major, dim(review.Request.Mode+" · "+review.Change))
				fmt.Fprintf(&output, "      %s %s\n", dim("available:"), joinedOrNone(review.Available))
				fmt.Fprintf(&output, "      %s %s -> %s\n", dim("binding:"), joinedOrNone(review.Previous), joinedOrNone(review.Proposed))
			}
		}
	}
	return output.String()
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

func applyReviewedAuthorities(lock *project.LockDocument, choices map[string]bool) {
	for id, entry := range lock.Plugins {
		current := map[string]project.AuthorityGrant{}
		for _, grant := range entry.Grants {
			current[grant.Name] = grant
		}
		entry.Grants = entry.Grants[:0]
		for _, request := range entry.RequestedAuthorities {
			key := authorityKey(id, request.Name)
			if request.Mode != project.AuthorityRequired && !choices[key] {
				continue
			}
			grant, ok := current[request.Name]
			if !ok {
				grant = project.AuthorityGrant{Name: request.Name, Scope: request.Scope}
			}
			entry.Grants = append(entry.Grants, grant)
		}
		sort.Slice(entry.Grants, func(i, j int) bool { return entry.Grants[i].Name < entry.Grants[j].Name })
		lock.Plugins[id] = entry
	}
}

func authorityKey(pluginID, authority string) string { return pluginID + "\x00" + authority }

func projectScopeSuffix(scope project.AuthorityScope) string {
	fields := projectScopeText(scope)
	if fields == "" {
		return ""
	}
	return " [" + fields + "]"
}

func projectScopeText(scope project.AuthorityScope) string {
	var fields []string
	if len(scope.Modules) != 0 {
		fields = append(fields, "modules="+strings.Join(scope.Modules, ","))
	}
	if scope.MaxInstances != 0 {
		fields = append(fields, fmt.Sprintf("maxInstances=%d", scope.MaxInstances))
	}
	if scope.MaxMemoryBytes != 0 {
		fields = append(fields, fmt.Sprintf("maxMemoryBytes=%d", scope.MaxMemoryBytes))
	}
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, "; ")
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func pkgGrant(name string, useGlobal bool, requested []string, grantAll, denyAll bool, scopes map[string]map[string]project.AuthorityScope) {
	src, err := depsSource(useGlobal)
	if err != nil {
		fatal("plugin grant: %v", err)
	}
	ctx := context.Background()
	err = withPluginMutationLock(ctx, src, func(mutation *project.Mutation) error {
		manifest, readErr := mutation.ReadManifest()
		if readErr != nil {
			return readErr
		}
		lock, readErr := mutation.ReadLock()
		if readErr != nil {
			return readErr
		}
		id, resolveErr := selectGrantPlugin(name, lock)
		if resolveErr != nil {
			return resolveErr
		}
		lock, editErr := editAuthorityGrants(lock, id, requested, grantAll, denyAll, scopes)
		if editErr != nil {
			return editErr
		}
		buildDir, err := buildDirFor(useGlobal)
		if err != nil {
			return err
		}
		return stageAndPublishLockedState(mutation, src, buildDir, manifest, lock, false)
	})
	if err != nil {
		fatal("plugin grant: %v", err)
	}
	fmt.Printf("%s updated reviewed authority grants\n", cyan("✓"))
}

func editAuthorityGrants(lock project.LockDocument, id string, requested []string, grantAll, denyAll bool, scopes map[string]map[string]project.AuthorityScope) (project.LockDocument, error) {
	entry, ok := lock.Plugins[id]
	if !ok {
		return project.LockDocument{}, fmt.Errorf("plugin %q is not in the lock graph", id)
	}
	for pluginID := range scopes {
		if pluginID != id {
			return project.LockDocument{}, fmt.Errorf("plugin grant for %s cannot apply --scopes entry that targets plugin %s", id, pluginID)
		}
	}
	allowed := map[string]project.AuthorityRequest{}
	for _, request := range entry.RequestedAuthorities {
		allowed[request.Name] = request
	}
	for _, authority := range requested {
		if _, ok := allowed[authority]; !ok {
			return project.LockDocument{}, fmt.Errorf("plugin %s does not request authority %q", id, authority)
		}
	}
	current := map[string]project.AuthorityGrant{}
	for _, grant := range entry.Grants {
		current[grant.Name] = grant
	}
	entry.Grants = nil
	for _, request := range entry.RequestedAuthorities {
		grant, exists := current[request.Name]
		if !exists || grantAll {
			grant = project.AuthorityGrant{Name: request.Name, Scope: request.Scope}
		}
		selected := request.Mode == project.AuthorityRequired
		switch {
		case grantAll:
			selected = true
		case denyAll:
		case requested != nil:
			selected = selected || containsString(requested, request.Name)
		default:
			selected = selected || exists
		}
		if selected {
			entry.Grants = append(entry.Grants, grant)
		}
	}
	sort.Slice(entry.Grants, func(i, j int) bool { return entry.Grants[i].Name < entry.Grants[j].Name })
	lock.Plugins[id] = entry
	if err := applyAuthorityScopeOverrides(&lock, scopes); err != nil {
		return project.LockDocument{}, err
	}
	if err := project.ValidateLock(lock); err != nil {
		return project.LockDocument{}, err
	}
	return lock, nil
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
	name = strings.TrimSpace(name)
	if name != "" {
		if err := project.ValidatePluginID(name); err != nil {
			return "", err
		}
		if _, ok := lock.Plugins[name]; !ok {
			return "", fmt.Errorf("plugin %q is not in the lock graph", name)
		}
		return name, nil
	}
	return "", fmt.Errorf("plugin grant requires a full plugin ID")
}
