// Package compiler defines the stable boundary between Wago's compiler engines
// and the runtime-facing compilation pipeline.
package compiler

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"runtime"
	"strings"

	"github.com/wago-org/wago/src/core/codeimage"
	compilerprofile "github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
	"golang.org/x/sys/cpu"
)

// Engine identifies an independent compiler pipeline.
type Engine uint8

const (
	EngineRailshot Engine = iota
	EngineDragline
)

func (e Engine) String() string {
	switch e {
	case EngineRailshot:
		return "railshot"
	case EngineDragline:
		return "dragline"
	default:
		return fmt.Sprintf("CompilerEngine(%d)", uint8(e))
	}
}

// Valid reports whether e names a compiler engine understood by this build.
func (e Engine) Valid() bool { return e == EngineRailshot || e == EngineDragline }

// RuntimeContract contains stable runtime facts visible to every compiler engine.
type RuntimeContract struct {
	ABIRevision uint32
}

// HostImport identifies one imported function without depending on declaration
// order. Module and Name are the exact strings in the Wasm import section.
type HostImport struct {
	Module string `json:"module"`
	Name   string `json:"name"`
}

// HostHeapMask identifies runtime state an imported host function may observe
// or modify. A declared contract is trusted compiler input: omitting an effect
// can make code motion unsound.
type HostHeapMask uint16

const (
	HostHeapLinearMemory HostHeapMask = 1 << iota
	HostHeapTable
	HostHeapGlobal
	HostHeapGCHeader
	HostHeapGCStruct
	HostHeapGCArray
	HostHeapImportState
	HostHeapRuntimeState
	HostHeapUnknown
)

const hostHeapMaskAll = HostHeapLinearMemory | HostHeapTable | HostHeapGlobal |
	HostHeapGCHeader | HostHeapGCStruct | HostHeapGCArray | HostHeapImportState |
	HostHeapRuntimeState | HostHeapUnknown

// HostEffectFlags identifies non-aliasing effects of an imported host call.
// Calls are always kept in source order; these flags describe the additional
// barriers the call requires.
type HostEffectFlags uint16

const (
	HostEffectMayGrow HostEffectFlags = 1 << iota
	HostEffectMayAllocate
	HostEffectMayCollect
	HostEffectMayReenter
	HostEffectMayThrow
)

const hostEffectFlagsAll = HostEffectMayGrow | HostEffectMayAllocate |
	HostEffectMayCollect | HostEffectMayReenter | HostEffectMayThrow

// HostEffectContract is an explicit, trusted summary for one host import.
type HostEffectContract struct {
	Reads  HostHeapMask    `json:"reads"`
	Writes HostHeapMask    `json:"writes"`
	Flags  HostEffectFlags `json:"flags"`
}

// Validate rejects bits that no compiler version understands.
func (c HostEffectContract) Validate() error {
	if c.Reads&^hostHeapMaskAll != 0 || c.Writes&^hostHeapMaskAll != 0 || c.Flags&^hostEffectFlagsAll != 0 {
		return fmt.Errorf("compiler: invalid host effect contract")
	}
	return nil
}

// HostEffectBinding is the normalized function-import-order representation.
// Declared distinguishes an explicitly pure contract from an absent contract.
type HostEffectBinding struct {
	Contract HostEffectContract `json:"contract"`
	Declared bool               `json:"declared"`
}

// Fingerprint returns a stable identity for the runtime-visible compiler
// contract. Extend this encoding whenever RuntimeContract gains a codegen fact.
func (c RuntimeContract) Fingerprint() [32]byte {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], c.ABIRevision)
	h := sha256.New()
	h.Write([]byte("wago-runtime-contract-v1\x00"))
	h.Write(encoded[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// OptimizationObjective selects the primary code-quality tradeoff. Speed is
// the zero/default value to preserve the current runtime policy.
type OptimizationObjective uint8

const (
	ObjectiveSpeed OptimizationObjective = iota
	ObjectiveBalanced
	ObjectiveSize
)

func (o OptimizationObjective) String() string {
	switch o {
	case ObjectiveSpeed:
		return "speed"
	case ObjectiveBalanced:
		return "balanced"
	case ObjectiveSize:
		return "size"
	default:
		return fmt.Sprintf("OptimizationObjective(%d)", uint8(o))
	}
}

func (o OptimizationObjective) Valid() bool { return o <= ObjectiveSize }

// BoundsMode is the backend-neutral linear-memory enforcement identity.
type BoundsMode uint8

const (
	BoundsExplicit BoundsMode = iota
	BoundsSignals
)

func (m BoundsMode) Valid() bool { return m == BoundsExplicit || m == BoundsSignals }

// TargetMode describes how target-specific the generated code may be.
type TargetMode uint8

const (
	TargetCompatibility TargetMode = iota
	TargetNative
	TargetExplicit
	TargetFatNative
)

func (m TargetMode) String() string {
	switch m {
	case TargetCompatibility:
		return "compat"
	case TargetNative:
		return "native"
	case TargetExplicit:
		return "explicit"
	case TargetFatNative:
		return "fat-native"
	default:
		return fmt.Sprintf("TargetMode(%d)", uint8(m))
	}
}

// TargetFeature is one stable bit in a target artifact identity. Values are
// architecture-qualified so an identity is unambiguous without interpreting
// the bits using process state.
type TargetFeature uint16

const (
	TargetFeatureAMD64BMI2 TargetFeature = iota
	TargetFeatureAMD64AVX2
	TargetFeatureAMD64AVX512
	TargetFeatureAMD64ERMS
	TargetFeatureAMD64FMA
	TargetFeatureARM64AES
	TargetFeatureARM64ATOMICS
	TargetFeatureARM64CRC32
	TargetFeatureARM64SVE
	TargetFeatureARM64SVE2
	// APX and MOPS are retained at the end so existing serialized target
	// identities keep the meaning of every earlier bit.
	TargetFeatureAMD64APX
	TargetFeatureARM64MOPS
)

// Target identifies the machine-code product requested from an engine.
type Target struct {
	GOOS        string
	GOARCH      string
	Mode        TargetMode
	CPUModel    string
	TuningModel string
	FeatureBits [4]uint64
}

// HasFeature reports whether a feature is present in this exact target
// identity. Compatibility targets deliberately have no optional feature bits.
func (t Target) HasFeature(feature TargetFeature) bool {
	word, bit := uint16(feature)/64, uint16(feature)%64
	return int(word) < len(t.FeatureBits) && t.FeatureBits[word]&(uint64(1)<<bit) != 0
}

func (t *Target) setFeature(feature TargetFeature, enabled bool) {
	word, bit := uint16(feature)/64, uint16(feature)%64
	if int(word) >= len(t.FeatureBits) {
		return
	}
	if enabled {
		t.FeatureBits[word] |= uint64(1) << bit
	}
}

// HostTarget resolves a compatibility or native identity for the current
// process. Native identities contain the tracked optional feature set visible
// to the OS plus a canonical host CPU identity. Tuning stays architecture
// generic unless the model belongs to a family with an explicit policy.
func HostTarget(mode TargetMode) (Target, error) {
	target := Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Mode: mode}
	switch mode {
	case TargetCompatibility:
		target.CPUModel = "generic"
		target.TuningModel = "generic"
	case TargetNative:
		target.CPUModel, target.TuningModel = classifyHostCPU(hostCPUBrand(), runtime.GOARCH)
		switch runtime.GOARCH {
		case "amd64":
			target.setFeature(TargetFeatureAMD64BMI2, cpu.X86.HasBMI2)
			target.setFeature(TargetFeatureAMD64AVX2, cpu.X86.HasAVX2)
			target.setFeature(TargetFeatureAMD64AVX512, cpu.X86.HasAVX512)
			target.setFeature(TargetFeatureAMD64ERMS, cpu.X86.HasERMS)
			target.setFeature(TargetFeatureAMD64FMA, cpu.X86.HasFMA)
		case "arm64":
			target.setFeature(TargetFeatureARM64AES, cpu.ARM64.HasAES)
			target.setFeature(TargetFeatureARM64ATOMICS, cpu.ARM64.HasATOMICS)
			target.setFeature(TargetFeatureARM64CRC32, cpu.ARM64.HasCRC32)
			target.setFeature(TargetFeatureARM64SVE, cpu.ARM64.HasSVE)
			target.setFeature(TargetFeatureARM64SVE2, cpu.ARM64.HasSVE2)
			target.setFeature(TargetFeatureARM64MOPS, hostHasARM64MOPS())
		default:
			return Target{}, fmt.Errorf("compiler target: native mode is unsupported on %s", runtime.GOARCH)
		}
	default:
		return Target{}, fmt.Errorf("compiler target: %s requires an explicit target configuration", mode)
	}
	return target, nil
}

func classifyHostCPU(brand, arch string) (model, tuning string) {
	model = canonicalCPUModel(brand)
	if model == "" {
		model = "host-" + arch
	}
	tuning = "generic-" + arch
	fields := strings.Fields(strings.ToLower(brand))
	if len(fields) >= 2 && fields[0] == "apple" {
		generation := fields[1]
		if len(generation) >= 2 && generation[0] == 'm' {
			end := 1
			for end < len(generation) && generation[end] >= '0' && generation[end] <= '9' {
				end++
			}
			if end > 1 {
				tuning = "apple-" + generation[:end]
			}
		}
	}
	return model, tuning
}

func canonicalCPUModel(brand string) string {
	brand = strings.ToLower(strings.TrimSpace(brand))
	var out strings.Builder
	dash := false
	for _, r := range brand {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && out.Len() != 0 {
				out.WriteByte('-')
			}
			out.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	return out.String()
}

// Fingerprint returns the stable cache and replay identity of a target. The
// encoding is deliberately independent of Go struct layout and process state.
func (t Target) Fingerprint() [32]byte {
	h := sha256.New()
	h.Write([]byte("wago-compiler-target-v1\x00"))
	writeFingerprintString(h, t.GOOS)
	writeFingerprintString(h, t.GOARCH)
	h.Write([]byte{byte(t.Mode)})
	writeFingerprintString(h, t.CPUModel)
	writeFingerprintString(h, t.TuningModel)
	var word [8]byte
	for _, bits := range t.FeatureBits {
		binary.LittleEndian.PutUint64(word[:], bits)
		h.Write(word[:])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func writeFingerprintString(h hash.Hash, value string) {
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(value)))
	h.Write(length[:])
	h.Write([]byte(value))
}

// Input is immutable validated input shared by sibling compiler engines.
type Input struct {
	Module *wasm.Module
	// FunctionWorkers is the already-resolved bounded module worker count.
	// Backends must treat zero like one for direct callers constructing Input.
	FunctionWorkers int
	// Source is the exact original Wasm module when available. Backends may use
	// it for reproducible diagnostics and replay artifacts, never as alternate
	// unvalidated compiler input.
	Source                   []byte
	Runtime                  RuntimeContract
	Target                   Target
	Objective                OptimizationObjective
	Bounds                   BoundsMode
	ConfigurationFingerprint [32]byte
	// Profile is an immutable backend-neutral snapshot keyed to Source and
	// original Wasm locations. The router validates its module hash.
	Profile *compilerprofile.Module
	// HostEffects is empty or has one entry per imported function in Wasm
	// function-index order. Undeclared entries retain conservative effects.
	HostEffects []HostEffectBinding
	// SelectedFunctions is an optional sorted set of original-Wasm function
	// indexes for a compact native clone. It must contain only local functions
	// and be closed over direct local calls. Nil compiles the complete module.
	SelectedFunctions []uint32
}

// Output is the runtime-facing native-code product shared by compiler engines.
type Output struct {
	Engine Engine

	CodeImage codeimage.Image
	Code      []byte

	Entry          []int
	InternalEntry  []int
	DirectPrepared []uint64

	GCCallsites            []GCFrameCallsite
	GCRoots                []uint32
	GCSafepoints           []GCFrameSafepoint
	GCSafepointRoots       []uint32
	GCAdapterReturnOffsets []uint32

	RequiresBMI2      bool
	RequiresAVX2      bool
	RequiresAVX512    bool
	RequiresARM64MOPS bool
}

// GCFrameSafepoint identifies the exact roots visible while a parked runtime
// helper may allocate and collect. IDs are module-global and encoded into the
// helper dispatch payload.
type GCFrameSafepoint struct {
	ID         uint32
	FrameBytes uint32
	RootStart  uint32
	RootCount  uint16
}

// GCFrameCallsite is one module-relative native return PC and its exact
// caller-frame root vector. StackAdjust removes temporary call-wrapper saves
// before FrameBytes is used to walk to the caller's caller.
type GCFrameCallsite struct {
	ReturnOffset uint32
	FrameBytes   uint32
	StackAdjust  uint32
	RootStart    uint32
	RootCount    uint16
}

// Backend is one complete, independent compiler engine.
type Backend interface {
	Compile(Input) (Output, error)
}

// BackendFunc adapts a function to Backend.
type BackendFunc func(Input) (Output, error)

func (f BackendFunc) Compile(input Input) (Output, error) { return f(input) }

// Router selects exactly one sibling engine. It never delegates between them.
type Router struct {
	Railshot Backend
	Dragline Backend
}

// Compile invokes only engine and stamps its identity on the returned output.
func (r Router) Compile(engine Engine, input Input) (Output, error) {
	if !engine.Valid() {
		return Output{}, fmt.Errorf("compiler: unknown engine %d", uint8(engine))
	}
	if input.Module == nil {
		return Output{}, fmt.Errorf("compiler %s: nil validated module", engine)
	}
	if input.Runtime.ABIRevision != runtimeabi.Revision {
		return Output{}, fmt.Errorf("compiler %s: runtime ABI revision %d unsupported (want %d)", engine, input.Runtime.ABIRevision, runtimeabi.Revision)
	}
	if !input.Objective.Valid() || !input.Bounds.Valid() {
		return Output{}, fmt.Errorf("compiler %s: invalid objective or bounds policy", engine)
	}
	if input.Profile != nil {
		if err := input.Profile.Validate(); err != nil {
			return Output{}, fmt.Errorf("compiler %s: %w", engine, err)
		}
		if len(input.Source) == 0 || input.Profile.ModuleHash != sha256.Sum256(input.Source) {
			return Output{}, fmt.Errorf("compiler %s: profile module hash does not match source", engine)
		}
	}
	if len(input.HostEffects) != 0 && len(input.HostEffects) != input.Module.ImportedFuncCount() {
		return Output{}, fmt.Errorf("compiler %s: host effect binding count %d, want %d", engine, len(input.HostEffects), input.Module.ImportedFuncCount())
	}
	for i, binding := range input.HostEffects {
		if !binding.Declared {
			continue
		}
		if err := binding.Contract.Validate(); err != nil {
			return Output{}, fmt.Errorf("compiler %s: host effect binding %d: %w", engine, i, err)
		}
	}
	var backend Backend
	switch engine {
	case EngineRailshot:
		backend = r.Railshot
	case EngineDragline:
		backend = r.Dragline
	}
	if backend == nil {
		return Output{}, fmt.Errorf("compiler %s: backend is not installed", engine)
	}
	output, err := backend.Compile(input)
	if err != nil {
		return Output{}, fmt.Errorf("compiler %s: %w", engine, err)
	}
	output.Engine = engine
	return output, nil
}
