package railspec

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGeneratedRulesVerifyTargetsAndNearMisses(t *testing.T) {
	for id := RuleID(1); int(id) < len(Rules); id++ {
		rule := Rules[id]
		if rule.ID != id || !rule.Verified || rule.Targets == 0 || rule.NativeBytes == 0 || rule.Latency == 0 || rule.Uops == 0 || rule.FormCount == 0 || int(rule.FormCount) > len(rule.Forms) {
			t.Fatalf("rule %d = %#v", id, rule)
		}
		for _, form := range rule.Forms[:rule.FormCount] {
			if form == FormInvalid {
				t.Fatalf("rule %d has invalid form: %#v", id, rule)
			}
		}
	}
	if got := SelectRule(TargetAMD64, wasm.InstrI64Add, true, 12, false); got != RuleAMD64Imm32 {
		t.Fatalf("AMD64 add rule = %d", got)
	}
	if got := SelectRule(TargetAMD64, wasm.InstrI64Add, true, 1<<40, false); got != RuleGenericRegister {
		t.Fatalf("AMD64 large immediate rule = %d", got)
	}
	if got := SelectRule(TargetARM64, wasm.InstrI64Add, true, 4096, false); got != RuleGenericRegister {
		t.Fatalf("ARM64 near-miss immediate rule = %d", got)
	}
	if VerifyRule(RuleAMD64Imm32, TargetARM64) {
		t.Fatal("AMD64-only rule verified for ARM64")
	}
}

func TestCoreRuleContractMatchesGeneratedTable(t *testing.T) {
	data, err := os.ReadFile("core_rules.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Version uint32 `json:"version"`
		Rules   []struct {
			NativeBytes uint8    `json:"native_bytes"`
			Latency     uint8    `json:"latency"`
			Uops        uint8    `json:"uops"`
			Forms       []string `json:"forms"`
			Verified    bool     `json:"verified"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if contract.Version != 1 || len(contract.Rules)+1 != len(Rules) {
		t.Fatalf("contract version=%d rules=%d generated=%d", contract.Version, len(contract.Rules), len(Rules)-1)
	}
	for index, source := range contract.Rules {
		generated := Rules[index+1]
		if source.NativeBytes != generated.NativeBytes || source.Latency != generated.Latency || source.Uops != generated.Uops || len(source.Forms) != int(generated.FormCount) || source.Verified != generated.Verified {
			t.Fatalf("contract rule %d does not match generated row: source=%#v generated=%#v", index, source, generated)
		}
	}
}
