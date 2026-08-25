package plugin

import (
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
)

func TestFormatReviewPlanSummarizesSecurityAndContracts(t *testing.T) {
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
	plan.Lock = project.NewLockDocument()
	plan.Lock.Plugins["github.com/wago-org/wasi"] = project.LockEntry{}

	want := `Security review
  Native code       Yes
  OS sandbox        No
  Plugins           1
  Authorities       2 distinct · 2 requests
  Plugins run native code in Wago; grants restrict Wago APIs, not the OS.

Contract
  stdio@1 → none
  github.com/acme/time@1 → github.com/acme/new-clock
`
	if got := formatReviewPlan(plan); got != want {
		t.Fatalf("review plan:\n%s\nwant:\n%s", got, want)
	}
}
