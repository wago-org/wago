package plugin

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
)

func TestAuthorityReviewSelectorShowsFixedRequiredAndCancel(t *testing.T) {
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
	}, 2)

	if len(selector.Items) != 4 || len(selections) != 2 {
		t.Fatalf("selector rows=%d selections=%d", len(selector.Items), len(selections))
	}
	if selector.Title != "Permissions" {
		t.Fatalf("selector title = %q", selector.Title)
	}
	if !selector.Items[0].On || !selector.Items[0].Fixed || !strings.Contains(selector.Items[0].Description, required.Request.Reason) {
		t.Fatalf("required row = %#v", selector.Items[0])
	}
	if selector.Items[1].On || selector.Items[1].Fixed || !strings.Contains(selector.Items[1].Description, optional.Request.Reason) {
		t.Fatalf("optional row = %#v", selector.Items[1])
	}
	if selector.Items[2].Label != "Install 2 plugins" || !selector.Items[2].Action {
		t.Fatalf("install row = %#v", selector.Items[2])
	}
	if !selector.Items[3].Reject || selector.Items[3].Label != "Cancel installation" || selector.Items[3].Description != "make no changes" {
		t.Fatalf("cancel row = %#v", selector.Items[3])
	}
	if selector.DisableRejectShortcut == false {
		t.Fatal("authority review still enables the hidden reject-all shortcut")
	}
}

func TestAuthorityReviewSelectorDeduplicatesAuthorities(t *testing.T) {
	reviews := []AuthorityReview{
		{PluginID: "github.com/acme/one", Request: project.AuthorityRequest{Name: "host.arguments.read", Mode: project.AuthorityRequired}},
		{PluginID: "github.com/acme/two", Request: project.AuthorityRequest{Name: "host.arguments.read", Mode: project.AuthorityRequired}},
		{PluginID: "github.com/acme/two", Request: project.AuthorityRequest{Name: "instance.manage", Mode: project.AuthorityOptional}},
	}
	selector, selections := authorityReviewSelector(reviews, map[string]bool{}, 2)
	if len(selections) != 2 || len(selections[0].keys) != 2 || !selections[0].required {
		t.Fatalf("authority selections = %#v", selections)
	}
	if got := selector.Items[0].Description; !strings.Contains(got, "github.com/acme/one") || !strings.Contains(got, "github.com/acme/two") {
		t.Fatalf("deduplicated row = %#v", selector.Items[0])
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
