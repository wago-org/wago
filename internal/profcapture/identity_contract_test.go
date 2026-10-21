package profcapture

import "testing"

func TestWorkloadHashIncludesApplicationAndSemanticContracts(t *testing.T) {
	workload := Workload{
		Hash:     "module",
		Contract: "application-result-contract",
		semantic: []semanticCase{{ID: "catalog-result-contract"}},
	}
	original := workloadHash(workload)
	applicationChange := workload
	applicationChange.Contract = "different-application-result-contract"
	if workloadHash(applicationChange) == original {
		t.Fatal("workload identity ignored the application contract")
	}
	semanticChange := workload
	semanticChange.semantic = []semanticCase{{ID: "different-catalog-result-contract"}}
	if workloadHash(semanticChange) == original {
		t.Fatal("workload identity ignored the semantic contract")
	}
	if workloadHash(workload) != original {
		t.Fatal("unchanged workload identity is unstable")
	}
}
