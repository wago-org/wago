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
	if !selector.Items[0].On || !strings.Contains(selector.Items[0].Description, "required") || !strings.Contains(selector.Items[0].Description, required.Request.Reason) || !strings.Contains(selector.Items[0].Description, "cancels installation") {
		t.Fatalf("required row = %#v", selector.Items[0])
	}
	if selector.Items[1].On || !strings.Contains(selector.Items[1].Description, "optional") {
		t.Fatalf("optional row = %#v", selector.Items[1])
	}
	if !selector.Items[2].Reject || selector.Items[2].Label != "Reject all" {
		t.Fatalf("reject row = %#v", selector.Items[2])
	}
	selector.Items[0].On = false
	if !requiredAuthorityDeselected(selector, itemKeys, requiredKeys) {
		t.Fatal("deselecting a required authority did not require cancellation confirmation")
	}
}
