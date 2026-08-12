//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/optimization"

// The binding inventory is checked against the amd64 Optimization Definitions
// during package initialization. Public sense is always "on = enabled".
var optimizationBindings = optimization.NewBindings("amd64",
	optimization.Bind("bounds-facts", &boundsFactsEnabled),
	optimization.Bind("st-flags", &stFlagsEnabled),
	optimization.Bind("store8-flags", &store8FlagsEnabled),
	optimization.Bind("reg-merge", &regMergeEnabled),
	optimization.Bind("tee-sink", &teeLocalSinkEnabled),
	optimization.Bind("unary-sink", &unaryLocalSinkEnabled),
	optimization.Bind("branch-fold", &branchFoldEnabled),
	optimization.Bind("entry-arg-pins", &entryArgPinsEnabled),
	optimization.Bind("ext-fp-pins", &extendedFPPinsEnabled),
	optimization.Bind("call-next-use", &callNextUseEnabled),
	optimization.Bind("affine-lea", &affineLeaEnabled),
	optimization.Bind("tree-order", &treeOrderEnabled),
	optimization.Bind("assoc-tree", &associativeTreeEnabled),
	optimization.Bind("bmi2-rorx", &bmi2RorxEnabled),
	optimization.Bind("vex-float-mem", &vexFloatMemEnabled),
	optimization.Bind("multi-bounds-cert", &multiBoundsCertEnabled),
	optimization.Bind("addr-zext-elim", &memory32AddrZExtElimEnabled),
	optimization.Bind("immutable-table", &immutableLocalTableEnabled),
	optimization.Bind("immutable-table-type", &immutableTableTypeEnabled),
	optimization.Bind("inline-callfree", &inlineCallFreeHintsEnabled),
	optimization.Bind("store-forward", &linearStoreForwardEnabled),
	optimization.Bind("frame-elide", &smallFrameElideEnabled),
	optimization.Bind("compact-i32-frame", &compactI32FrameEnabled),
	optimization.Bind("tee-spill-elide", &teeSpillElideEnabled),
	optimization.Bind("commute-self-update", &commuteSelfUpdateEnabled),
	optimization.Bind("i64-mask32", &i64Mask32Enabled),
	optimization.Bind("v128-const-cache", &v128ConstCacheEnabled),
	optimization.Bind("v128-pins", &v128LocalPinsEnabled),
	optimization.Bind("v128-sink", &v128LocalSinkEnabled),
	optimization.Bind("reg-abi", &regABIEnabled),
	optimization.Bind("inline", &inlineEnabled),
	optimization.Bind("inline-loop-callees", &inlineLoopCallees),
	optimization.Bind("loop-precheck", &loopPrecheckEnabled),
	optimization.BindInverted("stack-fence", &noStackFence),
	optimization.BindInverted("stack-reg", &noStackReg),
)

type KnobInfo = optimization.Info
type OptimizationSnapshot = optimization.Snapshot

func OptKnobs() []KnobInfo { return optimizationBindings.Infos() }

func OptKnobSnapshot() ([]KnobInfo, OptimizationSnapshot) { return optimizationBindings.Snapshot() }

func CurrentOptKnobSnapshot() OptimizationSnapshot { return optimizationBindings.CurrentSnapshot() }

func SetOptKnob(name string, on bool) bool { return optimizationBindings.Set(name, on) }
