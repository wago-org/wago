package plugin

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
)

func TestAuthorityReviewSelectorLabelsEditablePluginRequirementAndCancel(t *testing.T) {
	required := AuthorityReview{
		PluginID: "github.com/wago-org/wasi",
		Request:  project.AuthorityRequest{Name: "host.arguments.read", Mode: project.AuthorityRequired, Reason: "read argv"},
		Change:   "new",
	}
	optional := AuthorityReview{
		PluginID: "github.com/wago-org/wasi",
		Request:  project.AuthorityRequest{Name: "host.environment.read", Mode: project.AuthorityOptional, Reason: "read environment"},
		Change:   "new",
	}
	selector, selections := authorityReviewSelector([]AuthorityReview{required, optional}, map[string]bool{
		authorityKey(required.PluginID, required.Request.Name): true,
	})

	if len(selector.Items) != 3 || len(selections) != 2 {
		t.Fatalf("selector rows=%d selections=%d", len(selector.Items), len(selections))
	}
	if selector.Title != "Permissions for wago-org/wasi" {
		t.Fatalf("selector title = %q", selector.Title)
	}
	if !selector.Items[0].On || selector.Items[0].Fixed || !strings.Contains(selector.Items[0].Description, required.Request.Reason) {
		t.Fatalf("required row = %#v", selector.Items[0])
	}
	if selector.Items[0].Label != "host.arguments.read (required)" || selector.Items[0].Children[0].Label != "wasi (required)" {
		t.Fatalf("required labels = %#v", selector.Items[0])
	}
	if selector.Items[1].On || selector.Items[1].Fixed || !strings.Contains(selector.Items[1].Description, optional.Request.Reason) {
		t.Fatalf("optional row = %#v", selector.Items[1])
	}
	if !selector.Items[2].Cancel || selector.Items[2].Label != "Cancel installation" || selector.Items[2].Description != "make no changes" {
		t.Fatalf("cancel row = %#v", selector.Items[2])
	}
	if !selector.DisableRejectShortcut || selector.MaxVisibleRows != 5 || !selector.WrapNavigation {
		t.Fatal("authority review still enables the hidden reject-all shortcut")
	}
}

func TestAuthorityReviewSelectorDeduplicatesAuthorities(t *testing.T) {
	reviews := []AuthorityReview{
		{PluginID: "github.com/acme/one", Request: project.AuthorityRequest{Name: "host.arguments.read", Mode: project.AuthorityRequired}},
		{PluginID: "github.com/acme/two", Request: project.AuthorityRequest{Name: "host.arguments.read", Mode: project.AuthorityRequired}},
		{PluginID: "github.com/acme/two", Request: project.AuthorityRequest{Name: "instance.manage", Mode: project.AuthorityOptional}},
	}
	selector, selections := authorityReviewSelector(reviews, map[string]bool{})
	if len(selections) != 2 || len(selections[0].keys) != 2 || selector.Items[0].Fixed {
		t.Fatalf("authority selections = %#v", selections)
	}
	if got := selector.Items[0].Description; !strings.Contains(got, "github.com/acme/one") || !strings.Contains(got, "github.com/acme/two") {
		t.Fatalf("deduplicated row = %#v", selector.Items[0])
	}
}

func TestAuthorityReviewSelectorEditsAuthorityByPackage(t *testing.T) {
	reviews := []AuthorityReview{
		{PluginID: "github.com/wago-org/wasi/p1", Request: project.AuthorityRequest{Name: "host.arguments.read", Mode: project.AuthorityRequired}},
		{PluginID: "github.com/wago-org/wasi/p2", Request: project.AuthorityRequest{Name: "host.arguments.read", Mode: project.AuthorityOptional}},
	}
	optionalKey := authorityKey(reviews[1].PluginID, reviews[1].Request.Name)
	selector, selections := authorityReviewSelector(reviews, map[string]bool{optionalKey: true})
	if len(selector.Items[0].Children) != 2 || selector.Items[0].Children[0].Label != "wasi/p1 (required)" || selector.Items[0].Children[1].Label != "wasi/p2" {
		t.Fatalf("package rows = %#v", selector.Items[0].Children)
	}
	if selector.Items[0].Children[0].Fixed || !selector.Items[0].Children[1].On {
		t.Fatalf("package grant state = %#v", selector.Items[0].Children)
	}
	selector.Items[0].Children[0].On = false
	selector.Items[0].Children[1].On = false
	choices := map[string]bool{optionalKey: true}
	applyAuthoritySelectionChoices(&selector.MultiSelect, selections, choices)
	if choices[optionalKey] || choices[authorityKey(reviews[0].PluginID, reviews[0].Request.Name)] {
		t.Fatalf("package authority was not disabled: %v", choices)
	}
}

func TestAuthorityReviewSelectorShowsUsedByAndLimit(t *testing.T) {
	review := AuthorityReview{
		PluginID: "github.com/wago-org/component-model",
		Direct:   true,
		Request: project.AuthorityRequest{
			Name: "core.instance.instantiate", Mode: project.AuthorityOptional,
			Reason: "instantiate the component graph", Scope: project.AuthorityScope{MaxInstances: 64, MaxMemoryBytes: 20 << 30},
		},
	}
	selector, _ := authorityReviewSelector([]AuthorityReview{review}, map[string]bool{})
	if selector.Title != "Permissions for wago-org/component-model" {
		t.Fatalf("title = %q", selector.Title)
	}
	for _, want := range []string{"instantiate the component graph", "used by: component-model", "limit: 64 instances · 20 GiB memory"} {
		if !strings.Contains(selector.Items[0].Description, want) {
			t.Fatalf("authority description missing %q: %q", want, selector.Items[0].Description)
		}
	}
}

func TestSelectGrantPluginExpandsGitHubShorthand(t *testing.T) {
	lock := project.NewLockDocument()
	lock.Plugins["github.com/wago-org/wasi"] = project.LockEntry{}
	got, err := selectGrantPlugin("wago-org/wasi", lock)
	if err != nil {
		t.Fatal(err)
	}
	if got != "github.com/wago-org/wasi" {
		t.Fatalf("selectGrantPlugin shorthand = %q", got)
	}
}

func TestGrantPluginPickerListsInstalledPlugins(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	lock := project.NewLockDocument()
	lock.Plugins["github.com/wago-org/wasi"] = project.LockEntry{RequestedAuthorities: []project.AuthorityRequest{{Name: "host.arguments.read"}}}
	lock.Plugins["github.com/acme/clock"] = project.LockEntry{RequestedAuthorities: []project.AuthorityRequest{{Name: "host.arguments.read"}, {Name: "host.environment.read"}}}
	picker := grantPluginPicker(lock)
	frame := picker.Frame()
	for _, want := range []string{"Choose plugin grants", "github.com/acme/clock", "wasi", "2 requested authorities", "1 requested authority"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("picker missing %q:\n%s", want, frame)
		}
	}
}

func TestGrantPluginTargetsUseDependenciesWhenRootHasNoAuthorities(t *testing.T) {
	lock := project.NewLockDocument()
	lock.Plugins["github.com/wago-org/wasi"] = project.LockEntry{Dependencies: map[string]string{
		"github.com/wago-org/wasi/p1": "v0.0.0",
		"github.com/wago-org/wasi/p2": "v0.0.0",
	}}
	lock.Plugins["github.com/wago-org/wasi/p1"] = project.LockEntry{RequestedAuthorities: []project.AuthorityRequest{{Name: "host.arguments.read", Mode: project.AuthorityRequired, Reason: "argv"}}}
	lock.Plugins["github.com/wago-org/wasi/p2"] = project.LockEntry{RequestedAuthorities: []project.AuthorityRequest{{Name: "host.environment.read", Mode: project.AuthorityOptional, Reason: "environment"}}}
	targets, err := grantPluginTargets("github.com/wago-org/wasi", lock)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(targets, ","); got != "github.com/wago-org/wasi/p1,github.com/wago-org/wasi/p2" {
		t.Fatalf("grant targets = %q", got)
	}
}

func TestUnmetPluginRequirementWarningsAreNonBlocking(t *testing.T) {
	lock := project.NewLockDocument()
	lock.Plugins["github.com/wago-org/wasi/p1"] = project.LockEntry{RequestedAuthorities: []project.AuthorityRequest{{Name: "host.arguments.read", Mode: project.AuthorityRequired, Reason: "argv"}}}
	warnings := unmetPluginRequirementWarnings(lock)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "wasi/p1 may not work") || !strings.Contains(warnings[0], "host.arguments.read") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestAuthorityRejectionChoicesUseYesAndNo(t *testing.T) {
	items := authorityExitItems()
	if len(items) != 2 || items[0].Label != "No" || items[0].Value != "continue" || items[0].Description != "" {
		t.Fatalf("back choice = %#v", items)
	}
	if items[1].Label != "Yes" || items[1].Value != "cancel" || items[1].Description != "" {
		t.Fatalf("exit choice = %#v", items)
	}
}
