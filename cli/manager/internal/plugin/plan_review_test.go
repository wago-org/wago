package plugin

import (
	"reflect"
	"strings"
	"testing"

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
