package spectest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCommittedRelease3BaselineIsPinnedAndComplete(t *testing.T) {
	path := filepath.Clean("../../tests/spec-v3-baseline.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Schema int `json:"schema"`
		Suite  struct {
			Commit    string `json:"commit"`
			WastFiles int    `json:"wast_files"`
		} `json:"suite"`
		Tools struct {
			Primary struct {
				Version string `json:"version"`
			} `json:"primary"`
			Fallback struct {
				Version  string `json:"version"`
				Revision string `json:"revision"`
			} `json:"fallback"`
		} `json:"tools"`
		Result    string `json:"result"`
		Inventory struct {
			RedFiles             int `json:"red_files"`
			GreenFiles           int `json:"green_files"`
			ParserFailures       int `json:"parser_failures"`
			InterpreterFallbacks int `json:"interpreter_fallbacks"`
			ByFamily             map[string]struct {
				RedFiles int `json:"red_files"`
			} `json:"by_family"`
		} `json:"inventory"`
		Totals struct {
			Modules struct {
				Passed, Failed, Skipped int
			} `json:"modules"`
			Assertions struct {
				Passed, Failed, Skipped int
			} `json:"assertions"`
			Gaps map[string]int `json:"gaps"`
		} `json:"totals_excluding_parser_failures"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Schema != 2 || got.Suite.Commit != "9d36019973201a19f9c9ebb0f10828b2fe2374aa" || got.Suite.WastFiles != 258 {
		t.Fatalf("baseline pin = schema %d commit %s files %d", got.Schema, got.Suite.Commit, got.Suite.WastFiles)
	}
	if got.Tools.Primary.Version != "1.0.41" || got.Tools.Fallback.Version != "3.0.0" || got.Tools.Fallback.Revision != got.Suite.Commit || got.Result != "pass" {
		t.Fatalf("baseline tools/result = WABT %s interpreter %s@%s %s, want pinned hybrid green baseline", got.Tools.Primary.Version, got.Tools.Fallback.Version, got.Tools.Fallback.Revision, got.Result)
	}
	if got.Inventory.RedFiles != 0 || got.Inventory.GreenFiles != got.Suite.WastFiles || got.Inventory.ParserFailures != 0 || got.Inventory.InterpreterFallbacks != 28 {
		t.Fatalf("baseline accounting = red %d green %d parser %d fallbacks %d", got.Inventory.RedFiles, got.Inventory.GreenFiles, got.Inventory.ParserFailures, got.Inventory.InterpreterFallbacks)
	}
	if got.Totals.Modules.Passed != 2226 || got.Totals.Modules.Failed != 0 || got.Totals.Modules.Skipped != 0 || got.Totals.Assertions.Passed != 58038 || got.Totals.Assertions.Failed != 0 || got.Totals.Assertions.Skipped != 0 {
		t.Fatalf("baseline totals = modules %+v assertions %+v", got.Totals.Modules, got.Totals.Assertions)
	}
	for name, count := range got.Totals.Gaps {
		if count != 0 {
			t.Errorf("baseline gap %s = %d, want 0", name, count)
		}
	}
	for _, family := range []string{
		"extended-constant-expressions", "tail-calls", "typed-function-references",
		"gc", "exception-handling", "multi-memory", "memory64", "table64", "relaxed-simd",
	} {
		entry, ok := got.Inventory.ByFamily[family]
		if !ok {
			t.Errorf("baseline lacks mandatory family %q", family)
		} else if entry.RedFiles != 0 {
			t.Errorf("baseline family %q has %d red files", family, entry.RedFiles)
		}
	}
}
