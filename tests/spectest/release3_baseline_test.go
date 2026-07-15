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
		Tool struct {
			Version string `json:"version"`
		} `json:"tool"`
		Result    string `json:"result"`
		Inventory struct {
			RedFiles       int `json:"red_files"`
			GreenFiles     int `json:"green_files"`
			ParserFailures int `json:"parser_failures"`
			ByFamily       map[string]struct {
				RedFiles int `json:"red_files"`
			} `json:"by_family"`
		} `json:"inventory"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Schema != 1 || got.Suite.Commit != "9d36019973201a19f9c9ebb0f10828b2fe2374aa" || got.Suite.WastFiles != 258 {
		t.Fatalf("baseline pin = schema %d commit %s files %d", got.Schema, got.Suite.Commit, got.Suite.WastFiles)
	}
	if got.Tool.Version != "1.0.41" || got.Result != "fail" {
		t.Fatalf("baseline tool/result = WABT %s %s, want pinned red baseline", got.Tool.Version, got.Result)
	}
	if got.Inventory.RedFiles+got.Inventory.GreenFiles != got.Suite.WastFiles || got.Inventory.ParserFailures == 0 {
		t.Fatalf("baseline accounting = red %d green %d parser %d", got.Inventory.RedFiles, got.Inventory.GreenFiles, got.Inventory.ParserFailures)
	}
	for _, family := range []string{
		"extended-constant-expressions", "tail-calls", "typed-function-references",
		"gc", "exception-handling", "multi-memory", "memory64", "table64", "relaxed-simd",
	} {
		if _, ok := got.Inventory.ByFamily[family]; !ok {
			t.Errorf("baseline lacks mandatory family %q", family)
		}
	}
}
