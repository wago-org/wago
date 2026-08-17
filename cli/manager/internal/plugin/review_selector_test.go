package plugin

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
)

func TestAuthorityReviewSelectorShowsRequiredAndRejectAll(t *testing.T) {
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
	selector, itemKeys, requiredKeys := authorityReviewSelector([]AuthorityReview{required, optional}, map[string]bool{
		authorityKey(required.PluginID, required.Request.Name): true,
	})

	if len(selector.Items) != 3 || len(itemKeys) != 2 || len(requiredKeys) != 1 {
		t.Fatalf("selector rows=%d item keys=%d required keys=%d", len(selector.Items), len(itemKeys), len(requiredKeys))
	}
	if !strings.Contains(selector.Title, required.PluginID) {
		t.Fatalf("selector title = %q", selector.Title)
	}
	if !selector.Items[0].On || !selector.Items[0].ConfirmOff || !strings.Contains(selector.Items[0].Description, "required") || !strings.Contains(selector.Items[0].Description, required.Request.Reason) {
		t.Fatalf("required row = %#v", selector.Items[0])
	}
	if strings.Contains(selector.Items[0].Description, "cancel") || strings.Contains(selector.Items[0].Description, "deselect") {
		t.Fatalf("required row repeats cancellation behavior: %q", selector.Items[0].Description)
	}
	if selector.Items[1].On || !strings.Contains(selector.Items[1].Description, "optional") {
		t.Fatalf("optional row = %#v", selector.Items[1])
	}
	if strings.Contains(selector.Items[0].Description, "—") || !strings.HasPrefix(selector.Items[0].Description, "(required)") {
		t.Fatalf("required metadata = %q", selector.Items[0].Description)
	}
	if !selector.Items[2].Reject || selector.Items[2].Label != "Reject all" || selector.Items[2].Description != "cancel installation" {
		t.Fatalf("reject row = %#v", selector.Items[2])
	}
	selector.Items[0].On = false
	if !requiredAuthorityDeselected(selector, itemKeys, requiredKeys) {
		t.Fatal("deselecting a required authority did not require cancellation confirmation")
	}
}

func TestAuthorityReviewSelectorSectionsMultiplePlugins(t *testing.T) {
	reviews := []AuthorityReview{
		{PluginID: "github.com/acme/one", Request: project.AuthorityRequest{Name: "host.arguments.read", Mode: project.AuthorityRequired}},
		{PluginID: "github.com/acme/one", Request: project.AuthorityRequest{Name: "host.import.define", Mode: project.AuthorityRequired}},
		{PluginID: "github.com/acme/two", Request: project.AuthorityRequest{Name: "instance.manage", Mode: project.AuthorityOptional}},
	}
	selector, _, _ := authorityReviewSelector(reviews, map[string]bool{})
	if selector.Title != "Authority grants" {
		t.Fatalf("selector title = %q", selector.Title)
	}
	for index, want := range []string{"github.com/acme/one", "github.com/acme/one", "github.com/acme/two"} {
		if selector.Items[index].Group != want {
			t.Fatalf("row %d group = %q, want %q", index, selector.Items[index].Group, want)
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

func TestAuthorityRejectionChoicesUseYesAndNo(t *testing.T) {
	items := authorityExitItems()
	if len(items) != 2 || items[0].Label != "No" || items[0].Value != "continue" || items[0].Description != "" {
		t.Fatalf("back choice = %#v", items)
	}
	if items[1].Label != "Yes" || items[1].Value != "cancel" || items[1].Description != "" {
		t.Fatalf("exit choice = %#v", items)
	}
}
