package plugin

import (
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
)

func TestFormatReviewPlanGroupsChangesByPlugin(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	plan := ResolutionPlan{
		Reviews: []AuthorityReview{
			{
				PluginID: "github.com/wago-org/wasi",
				Request: project.AuthorityRequest{
					Name: "host.arguments.read", Mode: project.AuthorityRequired,
					Reason: "expose the guest argv",
				},
				Change: "new",
			},
			{
				PluginID: "github.com/wago-org/wasi",
				Request: project.AuthorityRequest{
					Name: "host.import.define", Mode: project.AuthorityRequired,
					Reason: "define WASI host functions", Scope: project.AuthorityScope{Modules: []string{"wasi_snapshot_preview1"}},
				},
				Change: "new",
			},
		},
		ContractReviews: []ContractReview{
			{
				PluginID:  "github.com/wago-org/wasi",
				Request:   project.ContractRequirement{ID: "github.com/wago-org/stdio", Major: 1, Mode: "optional"},
				Available: []string{"github.com/wago-org/stdio-native"},
				Change:    "changed",
			},
			{
				PluginID: "github.com/acme/clock",
				Request:  project.ContractRequirement{ID: "github.com/acme/time", Major: 1, Mode: "required"},
				Previous: []string{"github.com/acme/old-clock"}, Proposed: []string{"github.com/acme/new-clock"},
				Available: []string{"github.com/acme/new-clock"}, Change: "changed",
			},
		},
	}

	want := `Plugin security
  Plugins run native code inside this Wago process.
  Authority grants constrain Wago interfaces; they are not an OS sandbox.

github.com/wago-org/wasi
  Authorities
    host.arguments.read  required · new
      expose the guest argv
    host.import.define  required · new
      define WASI host functions  modules=wasi_snapshot_preview1
  Contracts
    github.com/wago-org/stdio@1  optional · changed
      available: github.com/wago-org/stdio-native
      binding: none -> none

github.com/acme/clock
  Contracts
    github.com/acme/time@1  required · changed
      available: github.com/acme/new-clock
      binding: github.com/acme/old-clock -> github.com/acme/new-clock
`
	if got := formatReviewPlan(plan); got != want {
		t.Fatalf("review plan:\n%s\nwant:\n%s", got, want)
	}
}
