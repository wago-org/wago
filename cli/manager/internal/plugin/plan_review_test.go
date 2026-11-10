package plugin

import (
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
)

func TestPluginPlanReviewOwnsChoicesScopesValidationAndWarnings(t *testing.T) {
	lock := scopedGrantLock()
	entry := lock.Plugins["github.com/acme/plugin"]
	reviews := make([]AuthorityReview, 0, len(entry.RequestedAuthorities))
	choices := make(map[string]bool, len(entry.RequestedAuthorities))
	for _, request := range entry.RequestedAuthorities {
		reviews = append(reviews, AuthorityReview{PluginID: "github.com/acme/plugin", Request: request})
		choices[authorityKey("github.com/acme/plugin", request.Name)] = request.Name == "host.import.define"
	}

	got, err := (pluginPlanReview{
		lock: lock, reviews: reviews, choices: choices, applyAuthorities: true,
		scopes: map[string]map[string]project.AuthorityScope{
			"github.com/acme/plugin": {"host.import.define": {Modules: []string{"clock"}}},
		},
	}).finish()
	if err != nil {
		t.Fatal(err)
	}
	wantGrants := []project.AuthorityGrant{{Name: "host.import.define", Scope: project.AuthorityScope{Modules: []string{"clock"}}}}
	if grants := got.Lock.Plugins["github.com/acme/plugin"].Grants; !reflect.DeepEqual(grants, wantGrants) {
		t.Fatalf("grants = %#v, want %#v", grants, wantGrants)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "runtime.close.observe") {
		t.Fatalf("warnings = %#v", got.Warnings)
	}
}

func TestPluginPlanReviewGrantAllRestoresRequestedScope(t *testing.T) {
	lock := scopedGrantLock()
	entry := lock.Plugins["github.com/acme/plugin"]
	entry.Grants[0].Scope.Modules = []string{"clock"}
	lock.Plugins["github.com/acme/plugin"] = entry
	reviews := make([]AuthorityReview, 0, len(entry.RequestedAuthorities))
	choices := make(map[string]bool, len(entry.RequestedAuthorities))
	for _, request := range entry.RequestedAuthorities {
		reviews = append(reviews, AuthorityReview{PluginID: "github.com/acme/plugin", Request: request})
		choices[authorityKey("github.com/acme/plugin", request.Name)] = true
	}

	got, err := (pluginPlanReview{
		lock: lock, reviews: reviews, choices: choices, applyAuthorities: true,
		targets: map[string]bool{"github.com/acme/plugin": true}, resetSelectedScopes: true,
	}).finish()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"clock", "random"}
	if modules := got.Lock.Plugins["github.com/acme/plugin"].Grants[0].Scope.Modules; !reflect.DeepEqual(modules, want) {
		t.Fatalf("modules = %v, want %v", modules, want)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %#v", got.Warnings)
	}
}

func TestAllowAllReviewPreservesExactBindingsAndScopeLimits(t *testing.T) {
	automation.Reset()
	t.Cleanup(automation.Reset)
	t.Setenv("WAGO_NONINTERACTIVE", "")
	const id = "github.com/acme/plugin"
	for _, test := range []struct {
		name            string
		scope           project.AuthorityScope
		missingProvider bool
		wantError       string
	}{
		{name: "narrow scope", scope: project.AuthorityScope{MaxInstances: 2, MaxMemoryBytes: 65536}},
		{name: "widen scope", scope: project.AuthorityScope{MaxInstances: 9, MaxMemoryBytes: 65536}, wantError: "outside the requested scope"},
		{name: "zero limit", scope: project.AuthorityScope{MaxInstances: 0, MaxMemoryBytes: 65536}, wantError: "positive"},
		{name: "missing provider", scope: project.AuthorityScope{MaxInstances: 2, MaxMemoryBytes: 65536}, missingProvider: true, wantError: "does not provide"},
	} {
		t.Run(test.name, func(t *testing.T) {
			lock := scopedGrantLock()
			entry := lock.Plugins[id]
			entry.Grants[0].Scope.Modules = []string{"clock"}
			entry.Contracts.Requires = []project.ContractRequirement{{ID: "github.com/acme/service", Major: 1, Mode: "optional"}}
			entry.Bindings = []project.ContractBinding{{ID: "github.com/acme/service", Major: 1, Providers: []string{}}}
			if test.missingProvider {
				entry.Bindings[0].Providers = []string{"github.com/acme/missing"}
			}
			lock.Plugins[id] = entry
			plan := ResolutionPlan{Lock: lock, ContractReviews: []ContractReview{{
				PluginID: id, Request: entry.Contracts.Requires[0], Proposed: entry.Bindings[0].Providers, Change: "new",
			}}}
			for _, request := range entry.RequestedAuthorities {
				plan.Reviews = append(plan.Reviews, AuthorityReview{PluginID: id, Request: request})
			}
			reviewed, err := reviewResolvedPluginPlan(plan, pkgOpts{
				grantAll: true, acceptContracts: true,
				scopes: map[string]map[string]project.AuthorityScope{id: {"instance.manage": test.scope}},
			})
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := reviewed.Lock.Plugins[id]
			if !reflect.DeepEqual(got.Bindings, entry.Bindings) {
				t.Fatalf("exact proposed bindings changed: %#v", got.Bindings)
			}
			want := []project.AuthorityGrant{
				{Name: "host.import.define", Scope: project.AuthorityScope{Modules: []string{"clock"}}},
				{Name: "instance.manage", Scope: test.scope},
				{Name: "runtime.close.observe"},
			}
			if !reflect.DeepEqual(got.Grants, want) {
				t.Fatalf("grants = %#v, want %#v", got.Grants, want)
			}
		})
	}
}
