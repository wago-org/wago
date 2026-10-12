//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/internal/jitprofile"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/optimization"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type ImportBinding = shared.ImportBinding
type KnobInfo = optimization.Info
type OptimizationSnapshot = optimization.Snapshot

// CompileOptions preserves the common compiler contract. Unsupported semantic
// options fail before staging an image; conservative optimization hints may use
// the fully checked serial path. ForceFallback remains a genuine RV64 diagnostic.
type CompileOptions struct {
	SourceMaps, UnwindMaps, Profile bool
	BitCountFeatures                uint8
	Optimizations                   map[string]bool
	OptimizationSnapshot            OptimizationSnapshot
	OptimizationDeltas              map[string]bool
	CompactNative                   bool
	Workers                         int
	DeferCodeMapping                bool
	ElideBoundsChecks               bool
	NoBoundsFacts                   bool
	ImportBindings                  []ImportBinding
	SyncHostCalls                   bool
	SyncHostSlots                   int
	Interruptible                   bool
	// ForeignTableCalls requires a runtime that authenticates and retains all
	// canonical table targets through entry, host resume and cold decoration.
	// Entries use a synchronous root control frame and full signature transport.
	// InvocationContext requires the RV64 public 136-byte context and Engine/
	// activation-owned views. Legacy private contexts must leave it false.
	// NativeTailCalls requires the invocation-bound proper-tail factory runtime.
	// It is an internal qualification contract until every target family is ready.
	NativeTailCalls bool
	// NativeReferenceCalls permits ordinary call_ref and reference control under
	// retained canonical owners. Call transport and control witnesses are distinct.
	NativeReferenceCalls bool
	// NativeReferenceFunctions permits closed modules with internal reference
	// parameters/results and numeric exports. It requires sparse physical owners;
	// the image witness is selected only when an actual function uses this ABI.
	NativeReferenceFunctions        bool
	InvocationContext               bool
	ForeignTableCalls               bool
	MemoryPressureAt                int
	MemoryPressure                  func()
	GCTypeSubtypingRefTest          bool
	GCStructHelpers, GCArrayHelpers bool
	GCFrameRoots                    *shared.GCModuleFrameRootPlan
	Codegen                         codegen.Options
	// Stats collects CompileModuleWith's finalized local-function sizes with
	// wago_codegenstats or wago_profile. Success replaces the sink; failure
	// leaves it unchanged. The isolated private pilot does not collect stats.
	Stats               *ModuleStats
	CollectInlineReport bool
	CustomInstructions  map[uint32]railcore.CustomInstruction
	ForceFallback       bool
	// I31CoreSource is an exact-wire provenance witness supplied by the public
	// source frontend for a bounded i31 product. It never grants lowering by
	// itself: CompileModuleWith independently rechecks the decoded module before
	// selecting the pilot.
	I31CoreSource bool
	// GCRefEqSource is the exact-wire witness for the allocating official
	// gc/ref_eq checkpoint. It is useful only when the decoded module, helper
	// options and collector metadata independently match that product.
	GCRefEqSource bool
	// GCRefTestTableSource is the exact-wire witness for the bounded rooted
	// object-table allocation and indexed ref.test checkpoint.
	GCRefTestTableSource bool
	// GCRefTestConcreteSource is the exact-wire witness for the official
	// concrete struct-subtyping ref.test checkpoint.
	GCRefTestConcreteSource bool
	// GCRefTestAbstractSource is the exact-wire witness for the official mixed
	// abstract anyref/funcref/externref ref.test checkpoint.
	GCRefTestAbstractSource bool
	// GCExternSource is the exact-wire witness for the official bounded
	// anyref/externref conversion and rooted object-table checkpoint.
	GCExternSource bool
	// GCRefCastAbstractSource is the exact-wire witness for the official
	// abstract anyref ref.cast checkpoint.
	GCRefCastAbstractSource bool
	// GCRefCastConcreteSource is the exact-wire witness for the official
	// concrete struct-subtyping ref.cast checkpoint.
	GCRefCastConcreteSource bool
	// GCBrOnCastAbstractSource and GCBrOnCastFailAbstractSource are exact-wire
	// witnesses for the two official abstract branch-cast products.
	GCBrOnCastAbstractSource     bool
	GCBrOnCastFailAbstractSource bool
	// GCBrOnCastConcreteSource and GCBrOnCastFailConcreteSource are exact-wire
	// witnesses for the two official concrete subtype branch-cast products.
	GCBrOnCastConcreteSource     bool
	GCBrOnCastFailConcreteSource bool
	// GCBrOnCastNullabilitySource and GCBrOnCastFailNullabilitySource are
	// exact-wire witnesses for the validation-only nullability/control products.
	GCBrOnCastNullabilitySource     bool
	GCBrOnCastFailNullabilitySource bool
	// GCTypeSubtypingDeclarationsSource identifies the exact-wire witness for
	// one of the initial declaration-only gc/type-subtyping modules.
	GCTypeSubtypingDeclarationsSource uint8
	// GCTypeSubtypingRecursiveFunctionsSource identifies one of the two exact
	// recursive-function gc/type-subtyping products.
	GCTypeSubtypingRecursiveFunctionsSource uint8
	// GCTypeSubtypingRefFuncGlobalsSource identifies one of the six exact
	// immutable ref.func-global gc/type-subtyping products.
	GCTypeSubtypingRefFuncGlobalsSource uint8
	// GCTypeSubtypingRuntimeCallCastSource is the exact-wire witness for the
	// official owned-table call_indirect/ref.cast subtype execution product.
	GCTypeSubtypingRuntimeCallCastSource bool
	// GCTypeSubtypingRuntimeFinalityCallCastSource is the exact-wire witness for
	// the official final/nonfinal call_indirect/ref.cast product.
	GCTypeSubtypingRuntimeFinalityCallCastSource bool
	// GCTypeSubtypingRuntimeTypedTableCallSource is the exact-wire witness for
	// the official indexed-function typed-table call_indirect product.
	GCTypeSubtypingRuntimeTypedTableCallSource bool
	// GCTypeSubtypingRefTestSingleSource identifies an exact official
	// bounded recursive-function ref.test product.
	GCTypeSubtypingRefTestSingleSource uint8
	// GCTypeSubtypingLinkSource identifies the exact official .29 provider or
	// .30 consumer in the first recursive-function linking cluster.
	GCTypeSubtypingLinkSource uint8
	// GCTypeSubtypingFinalityLinkSource identifies the exact official .34
	// provider or one of the .35/.36 inverse-finality consumers.
	GCTypeSubtypingFinalityLinkSource uint8
	// GCTypeSubtypingStructLinkSource identifies the exact official .37
	// provider or .38 structurally compatible consumer.
	GCTypeSubtypingStructLinkSource uint8
	// GCTypeSubtypingStructProjectionSource identifies the exact official
	// .39 provider or .40 projected-struct compatible consumer.
	GCTypeSubtypingStructProjectionSource uint8
	// GCTypeSubtypingStructMismatchSource identifies the exact official .41
	// provider or .42 structurally incompatible consumer.
	GCTypeSubtypingStructMismatchSource uint8
	// GCTypeSubtypingIndependentStructSource identifies the exact official .43
	// provider or .44 independently declared compatible consumer.
	GCTypeSubtypingIndependentStructSource uint8
	// GCTypeSubtypingExtendedProjectionSource identifies the exact official .45
	// provider or .46 projected-recursion compatible consumer.
	GCTypeSubtypingExtendedProjectionSource uint8
	// GCTypeSubtypingDuplicateRecursiveSource identifies the exact official .47
	// provider or .48 duplicate-recursion compatible consumer.
	GCTypeSubtypingDuplicateRecursiveSource uint8
	// GCTypeSubtypingExtendedRecursiveSource identifies the exact official .49
	// provider or .50 extended-recursion compatible consumer.
	GCTypeSubtypingExtendedRecursiveSource uint8
	// GCTypeSubtypingCrossGroupMismatchSource identifies the exact official .51
	// provider or .52 cross-group incompatible consumer.
	GCTypeSubtypingCrossGroupMismatchSource uint8
	// GCTypeSubtypingTransitiveMismatchSource identifies the exact official .53
	// provider or .54 transitive-group incompatible consumer.
	GCTypeSubtypingTransitiveMismatchSource uint8
	// GCStructGetSource is the exact-wire witness for the bounded allocating
	// struct.new_default/struct.get checkpoint.
	GCStructGetSource bool
	// GCStructMutationSource is the exact-wire witness for the bounded numeric
	// struct.new_default/struct.get/struct.set checkpoint.
	GCStructMutationSource bool
	// GCStructNullSource is the exact-wire witness for the official
	// struct.get/struct.set null-dereference checkpoint.
	GCStructNullSource bool
	// GCStructPackedSource is the exact-wire witness for the official packed
	// i8/i16 struct global/get/set checkpoint.
	GCStructPackedSource bool
	// GCStructBasicSource is the exact-wire witness for the official basic
	// f32 struct allocation/get/set checkpoint.
	GCStructBasicSource bool
	// GCStructGlobalsSource is the exact-wire witness for the bounded
	// function-free pair of rooted numeric struct globals.
	GCStructGlobalsSource bool
	// GCArrayLocalSource is the exact-wire witness for the bounded numeric
	// array.new_default/get/set/len checkpoint.
	GCArrayLocalSource bool
	// GCArrayUniformSource is the exact-wire witness for the bounded numeric
	// array.new/get/set/len checkpoint.
	GCArrayUniformSource bool
	// GCArrayFixedLocalSource is the exact-wire witness for the bounded numeric
	// array.new_fixed/get/set/len checkpoint.
	GCArrayFixedLocalSource bool
	// GCArrayFixedOfficialSource is the exact-wire witness for the official
	// numeric array.new_fixed/get/set/len checkpoint.
	GCArrayFixedOfficialSource bool
	// GCArrayDefaultOfficialSource is the exact-wire witness for the official
	// numeric array.new_default/get/set/len checkpoint.
	GCArrayDefaultOfficialSource bool
	// GCArrayPackedDataSource is the exact-wire witness for the packed-i8
	// array.new_data/get_s/get_u/set/len/data.drop checkpoint.
	GCArrayPackedDataSource bool
	// GCArrayReferenceSource is the exact-wire witness for the official
	// array.new_elem/get/set/len/elem.drop checkpoint.
	GCArrayReferenceSource bool
	// GCArrayInitElemSource is the exact-wire witness for the official
	// funcref array.init_elem/elem.drop checkpoint.
	GCArrayInitElemSource bool
	// GCArrayInitDataSource is the exact-wire witness for the bounded official
	// packed-i8/i16 and wide-i32/i64 array.init_data checkpoints.
	GCArrayInitDataSource bool
	// GCArrayFillSource is the exact-wire witness for the official packed-i8
	// array.fill checkpoint.
	GCArrayFillSource bool
	// GCArrayCopySource is the exact-wire witness for the official packed-i8
	// array.copy checkpoint.
	GCArrayCopySource bool
	production        bool
	publicStarts      bool
	hostImports       bool
	// Selected by bounded closed-module admission, or private decoded tests.
	// Legacy staging and isolated instantiation never publish this ABI.
	referenceFunctions bool
	// Public TailCall remains gated until the whole instruction/target family.
	tailModule     *wasm.Module
	localTailCalls bool
	// Source production and private decoded adapters collect symbolic import sites.
	importTailCalls  bool
	dynamicTailCalls bool // declared-type indirect/reference source qualification
	// Each actual private dynamic tail kind needs its own qualified emitted
	// plan, including kinds whose instructions all occur in unreachable code.
	referenceTailInstructions    bool
	indirectTailInstructions     bool // actual private owned-table proper tails
	referenceCallInstructions    bool
	referenceControlInstructions bool
	// Selected only when the exact source and decoded witnesses agree for a
	// qualified non-allocating i31 module.
	i31Core                           bool
	gcRefEq                           bool
	gcRefTestTable                    bool
	gcRefTestConcrete                 bool
	gcRefTestAbstract                 bool
	gcBrOnCastNullability             bool
	gcTypeSubtypingDeclarations       bool
	gcTypeSubtypingRecursiveFunctions bool
	gcTypeSubtypingRefFuncGlobals     bool
	gcTypeSubtypingRuntimeCallCast    bool
	gcTypeSubtypingFinalityCallCast   bool
	gcTypeSubtypingTypedTableCall     bool
	gcTypeSubtypingRefTestSingle      bool
	gcTypeSubtypingRefTestVariant     uint8
	gcTypeSubtypingLink               uint8
	gcTypeSubtypingFinalityLink       uint8
	gcTypeSubtypingStructLink         uint8
	gcTypeSubtypingStructProjection   uint8
	gcTypeSubtypingStructMismatch     uint8
	gcTypeSubtypingIndependentStruct  uint8
	gcTypeSubtypingExtendedProjection uint8
	gcTypeSubtypingDuplicateRecursive uint8
	gcTypeSubtypingExtendedRecursive  uint8
	gcTypeSubtypingCrossGroupMismatch uint8
	gcTypeSubtypingTransitiveMismatch uint8
	gcExtern                          bool
	gcRefCastAbstract                 bool
	gcStructGet                       bool
	gcStructMutation                  bool
	gcStructNull                      bool
	gcStructPacked                    bool
	gcStructBasic                     bool
	gcStructGlobals                   bool
	gcArrayLocal                      bool
	gcArrayUniform                    bool
	gcArrayFixedLocal                 bool
	gcArrayFixedOfficial              bool
	gcArrayPackedData                 bool
	gcArrayReference                  bool
	gcArrayInitElem                   bool
	gcArrayInitData                   bool
	gcArrayFill                       bool
	gcArrayCopy                       bool
	tailPlans                         *[]tailImportPlan
	tailSiteBase                      uint32
}

// CodegenStats reports finalized local-function CodeBytes and FrameBytes.
// FuncIdx is the local definition index, excluding imported functions. CodeBytes
// includes the framed wrapper and cold traps, but not inter-function alignment
// or separately instantiated import adapters. GCCodeBytes.Total is the same
// measured size. All other fields are currently unavailable and remain zero/nil;
// in particular, zero BoundsChecks/Spills/Reloads is not a measured count.
// Profile/SourceMaps add sparse finalized metadata in wago_profile builds;
// operation counters, CodeSites, inline reports and unwind remain unavailable.
type CodegenStats struct {
	FuncIdx, CodeBytes, FrameBytes int
	Name                           string
	Spills, Reloads, BoundsChecks  int
	Calls, Peephole                map[string]int
	FinalizerFallback              string
	GCCodeBytes                    shared.GCNativeCodeBytes
}

// ModuleStats owns a bounded snapshot of this compile's finalized functions.
// Profile/SourceMaps are opt-in; reusing a sink replaces all old attribution.
type ModuleStats struct {
	CodeSites      []shared.NativeCodeSite
	UnwindRanges   []jitprofile.UnwindRange
	SourceRanges   []shared.NativeSourceRange
	SourceFrames   []shared.NativeInlineFrame
	ProfileRegions []jitprofile.Region
	Funcs          []*CodegenStats
}

var optimizationBindings = optimization.NewBindings("riscv64")

func OptKnobs() []KnobInfo { return optimizationBindings.Infos() }
func OptKnobSnapshot() ([]KnobInfo, OptimizationSnapshot) {
	return optimizationBindings.Snapshot()
}
func CurrentOptKnobSnapshot() OptimizationSnapshot { return optimizationBindings.CurrentSnapshot() }
func SetOptKnob(name string, on bool) bool         { return optimizationBindings.Set(name, on) }
