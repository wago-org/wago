package railspec

import (
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
)

func TestTargetCostModelPreservesExactNativeIdentity(t *testing.T) {
	target := corecompiler.Target{
		GOARCH: "amd64", Mode: corecompiler.TargetNative, TuningModel: "test-family",
		FeatureBits: [4]uint64{(1 << corecompiler.TargetFeatureAMD64AVX2) | (1 << corecompiler.TargetFeatureAMD64BMI2)},
	}
	model, err := TargetCostModel(target)
	if err != nil {
		t.Fatal(err)
	}
	if model.Name != "test-family" || model.MaximumVectorBits != 256 || model.NativeFeatures != target.FeatureBits {
		t.Fatalf("model = %#v", model)
	}
	if cost, ok := model.Cost(RuleAMD64DivFixed); !ok || cost.Latency != 16 || cost.Uops != 4 {
		t.Fatalf("divide cost = %#v, %t", cost, ok)
	}
	if _, ok := model.Cost(RuleARM64Imm12); ok {
		t.Fatal("cross-target rule cost accepted")
	}
}

func TestTargetCostModelRepresentsSVEAsScalable(t *testing.T) {
	target := corecompiler.Target{GOARCH: "arm64", Mode: corecompiler.TargetNative, FeatureBits: [4]uint64{1 << corecompiler.TargetFeatureARM64SVE2}}
	model, err := TargetCostModel(target)
	if err != nil {
		t.Fatal(err)
	}
	if model.PreferredVectorBits != 128 || model.MaximumVectorBits != 0 {
		t.Fatalf("SVE policy = preferred %d max %d", model.PreferredVectorBits, model.MaximumVectorBits)
	}
}

func TestTargetCostModelAppliesOptimizationObjectiveToPriorityOnly(t *testing.T) {
	target := corecompiler.Target{GOARCH: "amd64", Mode: corecompiler.TargetNative}
	speed, err := TargetCostModelForObjective(target, corecompiler.ObjectiveSpeed)
	if err != nil {
		t.Fatal(err)
	}
	size, err := TargetCostModelForObjective(target, corecompiler.ObjectiveSize)
	if err != nil {
		t.Fatal(err)
	}
	factual, ok := size.Cost(RuleAMD64DivFixed)
	if !ok {
		t.Fatal("size model omitted divide rule")
	}
	sizePriority, _ := size.PriorityCost(RuleAMD64DivFixed)
	speedPriority, _ := speed.PriorityCost(RuleAMD64DivFixed)
	if size.Objective != corecompiler.ObjectiveSize || sizePriority.Latency != factual.NativeBytes || sizePriority.Uops != factual.NativeBytes {
		t.Fatalf("size priority = %#v factual=%#v model=%#v", sizePriority, factual, size)
	}
	if speedPriority != factual {
		t.Fatalf("speed priority changed factual cost: %#v != %#v", speedPriority, factual)
	}
	if _, err := TargetCostModelForObjective(target, corecompiler.OptimizationObjective(99)); err == nil {
		t.Fatal("invalid objective accepted")
	}
}

func TestTargetCostModelAppliesOnlyMeasuredAppleM4Costs(t *testing.T) {
	generic, err := TargetCostModel(corecompiler.Target{GOARCH: "arm64", Mode: corecompiler.TargetCompatibility, TuningModel: "generic"})
	if err != nil {
		t.Fatal(err)
	}
	m4, err := TargetCostModel(corecompiler.Target{GOARCH: "arm64", Mode: corecompiler.TargetNative, TuningModel: "apple-m4"})
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := TargetCostModel(corecompiler.Target{GOARCH: "arm64", Mode: corecompiler.TargetNative, TuningModel: "apple-m5"})
	if err != nil {
		t.Fatal(err)
	}
	genericLoad, _ := generic.Cost(RuleFoldedMemoryAddress)
	m4Load, _ := m4.Cost(RuleFoldedMemoryAddress)
	unknownLoad, _ := unknown.Cost(RuleFoldedMemoryAddress)
	if genericLoad.Latency != 4 || m4Load.Latency != 3 || unknownLoad != genericLoad {
		t.Fatalf("folded-memory costs: generic=%#v m4=%#v unknown=%#v", genericLoad, m4Load, unknownLoad)
	}
	for id := RuleID(1); int(id) < len(Rules); id++ {
		if id == RuleFoldedMemoryAddress || Rules[id].Targets&TargetARM64 == 0 {
			continue
		}
		genericCost, genericOK := generic.Cost(id)
		m4Cost, m4OK := m4.Cost(id)
		if genericOK != m4OK || genericCost != m4Cost {
			t.Fatalf("unmeasured rule %d changed: generic=%#v/%t m4=%#v/%t", id, genericCost, genericOK, m4Cost, m4OK)
		}
	}
}
