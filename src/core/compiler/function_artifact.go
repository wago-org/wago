package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"io"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/profile"
)

const FunctionArtifactVersion = 3

const maxFunctionArtifactCode = 64 << 20

// FunctionArtifactIdentity contains every known code-generating dependency of
// one independently cacheable original-Wasm function.
type FunctionArtifactIdentity struct {
	Engine              Engine                `json:"engine"`
	Function            uint32                `json:"function"`
	BodyHash            [32]byte              `json:"body_hash"`
	TypeDependencyHash  [32]byte              `json:"type_dependency_hash"`
	RuntimeContractHash [32]byte              `json:"runtime_contract_hash"`
	RuntimeABI          uint32                `json:"runtime_abi"`
	PrivateABIRevision  uint32                `json:"private_abi_revision"`
	TargetFingerprint   [32]byte              `json:"target_fingerprint"`
	Objective           OptimizationObjective `json:"objective"`
	Bounds              BoundsMode            `json:"bounds"`
	RuntimeConfigHash   [32]byte              `json:"runtime_config_hash"`
	ProfileHash         [32]byte              `json:"profile_hash"`
	SpecializationHash  [32]byte              `json:"specialization_hash"`
	CalleeContractHash  [32]byte              `json:"callee_contract_hash"`
	CompilerRevision    [32]byte              `json:"compiler_revision"`
}

// NewFunctionArtifactIdentity derives shared input identities and accepts the
// backend-owned dependency digests explicitly. The function body hash is over
// original validated Wasm bytes, not an engine IR.
func NewFunctionArtifactIdentity(input Input, engine Engine, function uint32, body []byte, typeDependencies, specialization, calleeContracts, compilerRevision [32]byte, privateABI uint32) (FunctionArtifactIdentity, error) {
	identity := FunctionArtifactIdentity{
		Engine: engine, Function: function, BodyHash: sha256.Sum256(body), TypeDependencyHash: typeDependencies,
		RuntimeContractHash: input.Runtime.Fingerprint(), RuntimeABI: input.Runtime.ABIRevision, PrivateABIRevision: privateABI,
		TargetFingerprint: input.Target.Fingerprint(), Objective: input.Objective, Bounds: input.Bounds,
		RuntimeConfigHash: input.ConfigurationFingerprint, SpecializationHash: specialization, CalleeContractHash: calleeContracts, CompilerRevision: compilerRevision,
	}
	if input.Profile != nil {
		profileHash, err := profile.Hash(*input.Profile)
		if err != nil {
			return FunctionArtifactIdentity{}, err
		}
		identity.ProfileHash = profileHash
	}
	if err := identity.Validate(); err != nil {
		return FunctionArtifactIdentity{}, err
	}
	return identity, nil
}

func (i FunctionArtifactIdentity) Validate() error {
	if !i.Engine.Valid() || i.RuntimeABI == 0 || i.PrivateABIRevision == 0 || !i.Objective.Valid() || !i.Bounds.Valid() {
		return fmt.Errorf("compiler function artifact: invalid identity")
	}
	if i.TargetFingerprint == [32]byte{} || i.RuntimeConfigHash == [32]byte{} || i.CompilerRevision == [32]byte{} {
		return fmt.Errorf("compiler function artifact: incomplete identity")
	}
	if i.RuntimeContractHash != (RuntimeContract{ABIRevision: i.RuntimeABI}).Fingerprint() {
		return fmt.Errorf("compiler function artifact: runtime contract hash mismatch")
	}
	return nil
}

func (i FunctionArtifactIdentity) Fingerprint() [32]byte {
	h := sha256.New()
	h.Write([]byte("wago-function-artifact-identity-v1\x00"))
	h.Write([]byte{byte(i.Engine), byte(i.Objective), byte(i.Bounds)})
	writeU32Hash(h, i.Function)
	writeU32Hash(h, i.RuntimeABI)
	writeU32Hash(h, i.PrivateABIRevision)
	for _, digest := range [][32]byte{i.BodyHash, i.TypeDependencyHash, i.RuntimeContractHash, i.TargetFingerprint, i.RuntimeConfigHash, i.ProfileHash, i.SpecializationHash, i.CalleeContractHash, i.CompilerRevision} {
		h.Write(digest[:])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func writeU32Hash(h hash.Hash, value uint32) {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], value)
	h.Write(encoded[:])
}

type RelocationKind uint8

const (
	RelocationCall RelocationKind = iota + 1
	RelocationConstant
	RelocationStub
)

type FunctionRelocation struct {
	Offset uint32         `json:"offset"`
	Target uint32         `json:"target"`
	Addend int64          `json:"addend"`
	Kind   RelocationKind `json:"kind"`
}

type FunctionTrap struct {
	Offset     uint32 `json:"offset"`
	WasmOffset uint32 `json:"wasm_offset"`
	Code       uint16 `json:"code"`
}

type RootLocation struct {
	Index int32 `json:"index"`
	Kind  uint8 `json:"kind"`
	Bank  uint8 `json:"bank"`
}

const (
	RootLocationStack uint8 = 1
	RootBankCollector uint8 = 1
)

type FunctionSafepoint struct {
	Offset      uint32 `json:"offset"`
	ID          uint32 `json:"id,omitempty"`
	RootStart   uint32 `json:"root_start"`
	StackAdjust uint32 `json:"stack_adjust,omitempty"`
	RootCount   uint16 `json:"root_count"`
}

type FunctionSourceMap struct {
	NativeOffset uint32 `json:"native_offset"`
	WasmOffset   uint32 `json:"wasm_offset"`
}

type FunctionReference struct {
	Index uint32 `json:"index"`
	Kind  uint8  `json:"kind"`
}

// FunctionArtifact is a relocatable, independently verifiable machine-code
// product. All variable data lives in flat slabs to keep cache decoding bounded.
type FunctionArtifact struct {
	Version             uint32                   `json:"version"`
	Identity            FunctionArtifactIdentity `json:"identity"`
	IdentityFingerprint [32]byte                 `json:"identity_fingerprint"`
	Code                []byte                   `json:"code"`
	Entry               uint32                   `json:"entry"`
	PrivateEntry        uint32                   `json:"private_entry"`
	ABIClass            uint8                    `json:"abi_class"`
	FrameBytes          uint32                   `json:"frame_bytes"`
	AdapterReturnOffset uint32                   `json:"adapter_return_offset,omitempty"`
	ClobberGPR          uint64                   `json:"clobber_gpr"`
	ClobberFPR          uint64                   `json:"clobber_fpr"`
	RequiredISA         [4]uint64                `json:"required_isa"`
	Relocations         []FunctionRelocation     `json:"relocations,omitempty"`
	Traps               []FunctionTrap           `json:"traps,omitempty"`
	Safepoints          []FunctionSafepoint      `json:"safepoints,omitempty"`
	Roots               []RootLocation           `json:"roots,omitempty"`
	Sources             []FunctionSourceMap      `json:"sources,omitempty"`
	References          []FunctionReference      `json:"references,omitempty"`
}

func NewFunctionArtifact(identity FunctionArtifactIdentity, code []byte) FunctionArtifact {
	return FunctionArtifact{Version: FunctionArtifactVersion, Identity: identity, IdentityFingerprint: identity.Fingerprint(), Code: append([]byte(nil), code...)}
}

func cloneFunctionArtifact(a FunctionArtifact) FunctionArtifact {
	a.Code = append([]byte(nil), a.Code...)
	a.Relocations = append([]FunctionRelocation(nil), a.Relocations...)
	a.Traps = append([]FunctionTrap(nil), a.Traps...)
	a.Safepoints = append([]FunctionSafepoint(nil), a.Safepoints...)
	a.Roots = append([]RootLocation(nil), a.Roots...)
	a.Sources = append([]FunctionSourceMap(nil), a.Sources...)
	a.References = append([]FunctionReference(nil), a.References...)
	return a
}

func (a FunctionArtifact) Validate() error {
	if a.Version != FunctionArtifactVersion {
		return fmt.Errorf("compiler function artifact: version %d unsupported", a.Version)
	}
	if err := a.Identity.Validate(); err != nil {
		return err
	}
	if a.Identity.Fingerprint() != a.IdentityFingerprint {
		return fmt.Errorf("compiler function artifact: identity fingerprint mismatch")
	}
	if len(a.Code) == 0 || len(a.Code) > maxFunctionArtifactCode || int(a.Entry) >= len(a.Code) || int(a.PrivateEntry) >= len(a.Code) {
		return fmt.Errorf("compiler function artifact: invalid code or entries")
	}
	if a.AdapterReturnOffset != 0 && int(a.AdapterReturnOffset) >= len(a.Code) {
		return fmt.Errorf("compiler function artifact: invalid adapter return offset")
	}
	for index, relocation := range a.Relocations {
		if int(relocation.Offset) >= len(a.Code) || relocation.Kind < RelocationCall || relocation.Kind > RelocationStub || index != 0 && a.Relocations[index-1].Offset > relocation.Offset {
			return fmt.Errorf("compiler function artifact: invalid relocation %d", index)
		}
	}
	for index, trap := range a.Traps {
		if int(trap.Offset) >= len(a.Code) || trap.Code == 0 || index != 0 && a.Traps[index-1].Offset >= trap.Offset {
			return fmt.Errorf("compiler function artifact: invalid trap %d", index)
		}
	}
	rootEnd := uint64(0)
	for index, safepoint := range a.Safepoints {
		nextRootEnd := uint64(safepoint.RootStart) + uint64(safepoint.RootCount)
		if int(safepoint.Offset) >= len(a.Code) || uint64(safepoint.RootStart) != rootEnd || nextRootEnd > uint64(len(a.Roots)) || safepoint.StackAdjust&7 != 0 || safepoint.ID > codegen.GCSafepointIDMax || index != 0 && a.Safepoints[index-1].Offset >= safepoint.Offset {
			return fmt.Errorf("compiler function artifact: invalid safepoint %d", index)
		}
		rootEnd = nextRootEnd
	}
	if rootEnd != uint64(len(a.Roots)) {
		return fmt.Errorf("compiler function artifact: root slab is not canonically packed")
	}
	for index, root := range a.Roots {
		if root.Index < 0 || root.Index&7 != 0 || root.Kind != RootLocationStack || root.Bank != RootBankCollector {
			return fmt.Errorf("compiler function artifact: invalid root %d", index)
		}
	}
	for index, source := range a.Sources {
		if int(source.NativeOffset) >= len(a.Code) || index != 0 && a.Sources[index-1].NativeOffset >= source.NativeOffset {
			return fmt.Errorf("compiler function artifact: invalid source mapping %d", index)
		}
	}
	return nil
}

func MarshalFunctionArtifact(a FunctionArtifact) ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(a)
}

func UnmarshalFunctionArtifact(data []byte) (FunctionArtifact, error) {
	var artifact FunctionArtifact
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&artifact); err != nil {
		return FunctionArtifact{}, fmt.Errorf("compiler function artifact: decode: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return FunctionArtifact{}, fmt.Errorf("compiler function artifact: trailing JSON value")
		}
		return FunctionArtifact{}, fmt.Errorf("compiler function artifact: trailing data: %w", err)
	}
	if err := artifact.Validate(); err != nil {
		return FunctionArtifact{}, err
	}
	return artifact, nil
}
