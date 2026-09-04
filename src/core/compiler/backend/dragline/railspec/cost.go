package railspec

import (
	"fmt"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
)

type RuleCost struct {
	NativeBytes uint16
	Latency     uint16
	Uops        uint16
}

// CostModel is the compact target policy consumed by selection and scheduling.
// Compatibility and initially-unknown CPU families use the verified generated
// costs; future measured family tables override individual RuleCosts.
type CostModel struct {
	Name                string
	Target              TargetMask
	Objective           corecompiler.OptimizationObjective
	RuleCosts           [len(Rules)]RuleCost
	GPRCount            uint8
	PreferredVectorBits uint16
	MaximumVectorBits   uint16
	NativeFeatures      [4]uint64
}

func GenericCostModel(target TargetMask) (CostModel, error) {
	model := CostModel{Name: "generic", Target: target, PreferredVectorBits: 128, MaximumVectorBits: 128}
	switch target {
	case TargetAMD64:
		model.GPRCount = 16
	case TargetARM64:
		model.GPRCount = 31
	default:
		return CostModel{}, fmt.Errorf("railspec: unsupported cost target %#x", uint8(target))
	}
	for id, rule := range Rules {
		model.RuleCosts[id] = RuleCost{NativeBytes: uint16(rule.NativeBytes), Latency: uint16(rule.Latency), Uops: uint16(rule.Uops)}
	}
	return model, model.Validate()
}

// TargetCostModel resolves a compatibility or native target identity. Native
// feature widths are represented in policy, but generated scalar rule costs
// remain generic until a measured CPU-family table is available.
func TargetCostModel(target corecompiler.Target) (CostModel, error) {
	return TargetCostModelForObjective(target, corecompiler.ObjectiveSpeed)
}

// TargetCostModelForObjective retains factual rule costs while making the
// caller's speed/balanced/size scheduling policy explicit and replayable.
func TargetCostModelForObjective(target corecompiler.Target, objective corecompiler.OptimizationObjective) (CostModel, error) {
	if !objective.Valid() {
		return CostModel{}, fmt.Errorf("railspec: invalid optimization objective %d", objective)
	}
	var mask TargetMask
	switch target.GOARCH {
	case "amd64":
		mask = TargetAMD64
	case "arm64":
		mask = TargetARM64
	default:
		return CostModel{}, fmt.Errorf("railspec: unsupported target architecture %s", target.GOARCH)
	}
	model, err := GenericCostModel(mask)
	if err != nil {
		return CostModel{}, err
	}
	model.Name = target.TuningModel
	if model.Name == "" {
		model.Name = "generic"
	}
	model.NativeFeatures = target.FeatureBits
	model.Objective = objective
	applyMeasuredFamilyCosts(&model)
	if target.Mode == corecompiler.TargetNative {
		switch mask {
		case TargetAMD64:
			if target.HasFeature(corecompiler.TargetFeatureAMD64AVX512) {
				model.MaximumVectorBits = 512
			} else if target.HasFeature(corecompiler.TargetFeatureAMD64AVX2) {
				model.MaximumVectorBits = 256
			}
		case TargetARM64:
			if target.HasFeature(corecompiler.TargetFeatureARM64SVE2) || target.HasFeature(corecompiler.TargetFeatureARM64SVE) {
				// SVE vector length is runtime-defined. Zero deliberately means
				// scalable rather than guessing a host width.
				model.MaximumVectorBits = 0
			}
		}
	}
	return model, model.Validate()
}

// applyMeasuredFamilyCosts overrides only costs with a checked-in, repeatable
// native calibration. Unknown families deliberately retain generated generic
// costs. The Apple M4 dependent pointer-load chain measures 3.00 ADD-latency
// units on the native host; see bench/cmd/arm64cost and the dated calibration
// report in docs.
func applyMeasuredFamilyCosts(model *CostModel) {
	if model == nil {
		return
	}
	switch {
	case model.Target == TargetARM64 && model.Name == "apple-m4":
		cost := model.RuleCosts[RuleFoldedMemoryAddress]
		cost.Latency = 3
		model.RuleCosts[RuleFoldedMemoryAddress] = cost
	}
}

// PriorityCost maps factual rule measurements onto the requested scheduling
// objective. NativeBytes remains unchanged for final size accounting.
func (m CostModel) PriorityCost(id RuleID) (RuleCost, bool) {
	cost, ok := m.Cost(id)
	if !ok {
		return RuleCost{}, false
	}
	switch m.Objective {
	case corecompiler.ObjectiveBalanced:
		cost.Latency = saturatingCostSum(cost.Latency, cost.NativeBytes)
		cost.Uops = saturatingCostSum(cost.Uops, cost.NativeBytes)
	case corecompiler.ObjectiveSize:
		cost.Latency = cost.NativeBytes
		cost.Uops = cost.NativeBytes
	}
	return cost, true
}

func saturatingCostSum(a, b uint16) uint16 {
	sum := uint32(a) + uint32(b)
	if sum > uint32(^uint16(0)) {
		return ^uint16(0)
	}
	return uint16(sum)
}

func (m CostModel) Validate() error {
	if m.Name == "" || m.Target != TargetAMD64 && m.Target != TargetARM64 || m.GPRCount == 0 || m.PreferredVectorBits == 0 || !m.Objective.Valid() {
		return fmt.Errorf("railspec: malformed cost model")
	}
	for id := RuleID(1); int(id) < len(Rules); id++ {
		rule := Rules[id]
		cost := m.RuleCosts[id]
		if rule.Targets&m.Target != 0 && (cost.NativeBytes == 0 || cost.Latency == 0 || cost.Uops == 0) {
			return fmt.Errorf("railspec: cost model %q omits rule %d", m.Name, id)
		}
	}
	return nil
}

func (m CostModel) Cost(id RuleID) (RuleCost, bool) {
	if int(id) >= len(m.RuleCosts) || !VerifyRule(id, m.Target) {
		return RuleCost{}, false
	}
	return m.RuleCosts[id], true
}
