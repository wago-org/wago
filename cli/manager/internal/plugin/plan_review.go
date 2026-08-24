package plugin

import (
	"fmt"
	"sort"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/internal/tui"
)

// reviewedPluginPlan is the complete result of one Plugin Plan review. The
// caller decides when warnings become visible so staged publication can remain
// atomic while review policy stays in one module.
type reviewedPluginPlan struct {
	Lock     project.LockDocument
	Warnings []string
}

// pluginPlanReview owns the policy shared by resolution review and later grant
// editing: exact authority choices, optional scope narrowing, lock validation,
// and unmet Plugin Requirement warnings. Presentation gathers the choices, then
// crosses this seam once to produce a reviewed Plugin Plan.
type pluginPlanReview struct {
	lock                project.LockDocument
	reviews             []AuthorityReview
	choices             map[string]bool
	scopes              map[string]map[string]project.AuthorityScope
	targets             map[string]bool
	applyAuthorities    bool
	resetSelectedScopes bool
}

func reviewResolvedPluginPlan(plan ResolutionPlan, options pkgOpts) (reviewedPluginPlan, error) {
	if len(plan.Reviews) == 0 && len(plan.ContractReviews) == 0 && len(options.scopes) == 0 {
		return reviewedPluginPlan{Lock: plan.Lock}, nil
	}
	choices, reviewKeys := map[string]bool{}, map[string]bool{}
	requestedNames := map[string]bool{}
	for _, review := range plan.Reviews {
		key := authorityKey(review.PluginID, review.Request.Name)
		choices[key] = review.Request.Mode == project.AuthorityRequired || review.Previous != nil
		reviewKeys[key] = true
		requestedNames[review.Request.Name] = true
	}
	explicit := options.authorities != nil || options.grantAll || options.denyAll
	switch {
	case options.grantAll:
		for key := range reviewKeys {
			choices[key] = true
		}
	case options.denyAll:
		for key := range reviewKeys {
			choices[key] = false
		}
	case options.authorities != nil:
		for _, name := range options.authorities {
			if !requestedNames[name] {
				return reviewedPluginPlan{}, fmt.Errorf("no reviewed plugin requests authority %q", name)
			}
		}
		for _, review := range plan.Reviews {
			choices[authorityKey(review.PluginID, review.Request.Name)] = containsString(options.authorities, review.Request.Name)
		}
	}
	if automation.NoInput() {
		if len(plan.Reviews) != 0 && !explicit {
			return reviewedPluginPlan{}, fmt.Errorf("--no-input requires --allow, --allow-all, or --deny-all for authority review")
		}
		if len(plan.ContractReviews) != 0 && !options.acceptContracts {
			return reviewedPluginPlan{}, fmt.Errorf("--no-input requires --accept-contracts for exact contract-binding review")
		}
	}
	interactiveAuthorities := len(plan.Reviews) != 0 && !explicit && !automation.NoInput()
	interactiveContracts := len(plan.ContractReviews) != 0 && !options.acceptContracts && !automation.NoInput()
	if interactiveAuthorities || interactiveContracts {
		fmt.Printf("\n%s\n", formatReviewPlan(plan))
		if interactiveContracts {
			if err := reviewContractChoices(&plan); err != nil {
				return reviewedPluginPlan{}, err
			}
		}
		if interactiveAuthorities {
			if err := reviewAuthorityChoices(plan.Reviews, choices); err != nil {
				return reviewedPluginPlan{}, err
			}
		} else if interactiveContracts {
			selector := &tui.MultiSelect{Title: "Approve this reviewed plugin plan", Prompt: "enter accept · space decline · esc cancel", Items: []tui.SelectItem{{Label: "Apply this plan", Description: "publish the staged manifest, lock graph, and runtime together", On: true}}}
			_, cancelled := tui.Run(selector)
			if cancelled || !selector.Items[0].On {
				return reviewedPluginPlan{}, fmt.Errorf("plugin review cancelled; no changes were made")
			}
		}
	}
	return (pluginPlanReview{
		lock: plan.Lock, reviews: plan.Reviews, choices: choices, scopes: options.scopes,
		applyAuthorities: len(plan.Reviews) != 0,
	}).finish()
}

func (review pluginPlanReview) finish() (reviewedPluginPlan, error) {
	if review.applyAuthorities {
		applyAuthorityChoices(&review.lock, review.reviews, review.choices, review.targets, review.resetSelectedScopes)
	}
	if err := applyAuthorityScopeOverrides(&review.lock, review.scopes); err != nil {
		return reviewedPluginPlan{}, err
	}
	if err := project.ValidateLock(review.lock); err != nil {
		return reviewedPluginPlan{}, err
	}
	return reviewedPluginPlan{Lock: review.lock, Warnings: unmetPluginRequirementWarnings(review.lock)}, nil
}

func applyAuthorityChoices(lock *project.LockDocument, reviews []AuthorityReview, choices map[string]bool, targets map[string]bool, resetSelectedScopes bool) {
	reviewed := make(map[string]bool, len(reviews))
	for _, review := range reviews {
		reviewed[authorityKey(review.PluginID, review.Request.Name)] = true
	}
	for id, entry := range lock.Plugins {
		if targets != nil && !targets[id] {
			continue
		}
		current := make(map[string]project.AuthorityGrant, len(entry.Grants))
		for _, grant := range entry.Grants {
			current[grant.Name] = grant
		}
		entry.Grants = entry.Grants[:0]
		for _, request := range entry.RequestedAuthorities {
			key := authorityKey(id, request.Name)
			selected, wasReviewed := choices[key]
			if wasReviewed && !selected {
				continue
			}
			grant, exists := current[request.Name]
			if !exists {
				if !reviewed[key] {
					continue
				}
				grant = project.AuthorityGrant{Name: request.Name, Scope: request.Scope}
			} else if wasReviewed && resetSelectedScopes {
				grant.Scope = request.Scope
			}
			entry.Grants = append(entry.Grants, grant)
		}
		sort.Slice(entry.Grants, func(i, j int) bool { return entry.Grants[i].Name < entry.Grants[j].Name })
		lock.Plugins[id] = entry
	}
}
