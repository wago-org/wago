package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/profile"
)

const ReplayVersion = 4

// ReplayArtifact captures one failed function against the exact original
// module and compiler contracts that produced the failure.
type ReplayArtifact struct {
	Version uint32 `json:"version"`
	Engine  Engine `json:"engine"`

	ModuleHash [32]byte `json:"module_hash"`
	Module     []byte   `json:"module"`

	Function uint32 `json:"function"`
	Stage    string `json:"stage"`
	Error    string `json:"error"`

	RuntimeABI               uint32                `json:"runtime_abi"`
	Target                   Target                `json:"target"`
	TargetFingerprint        [32]byte              `json:"target_fingerprint"`
	Objective                OptimizationObjective `json:"objective"`
	Bounds                   BoundsMode            `json:"bounds"`
	ConfigurationFingerprint [32]byte              `json:"configuration_fingerprint"`
	Profile                  *profile.Module       `json:"profile,omitempty"`
	ProfileHash              [32]byte              `json:"profile_hash"`
	HostEffects              []HostEffectBinding   `json:"host_effects,omitempty"`
	SelectedFunctions        []uint32              `json:"selected_functions,omitempty"`
}

// NewReplayArtifact creates a replay whose hashes are derived from its exact
// payload rather than supplied by the caller.
func NewReplayArtifact(engine Engine, input Input, function uint32, stage, message string) ReplayArtifact {
	replay := ReplayArtifact{
		Version: ReplayVersion, Engine: engine,
		ModuleHash: sha256.Sum256(input.Source), Module: append([]byte(nil), input.Source...),
		Function: function, Stage: stage, Error: message,
		RuntimeABI: input.Runtime.ABIRevision, Target: input.Target,
		TargetFingerprint: input.Target.Fingerprint(),
		Objective:         input.Objective, Bounds: input.Bounds,
		ConfigurationFingerprint: input.ConfigurationFingerprint,
		HostEffects:              append([]HostEffectBinding(nil), input.HostEffects...),
		SelectedFunctions:        append([]uint32(nil), input.SelectedFunctions...),
	}
	if input.Profile != nil {
		cloned := profile.Clone(*input.Profile)
		replay.Profile = &cloned
		replay.ProfileHash, _ = profile.Hash(cloned)
	}
	return replay
}

func (r ReplayArtifact) Validate() error {
	if r.Version != ReplayVersion {
		return fmt.Errorf("compiler replay: version %d unsupported (want %d)", r.Version, ReplayVersion)
	}
	if !r.Engine.Valid() {
		return fmt.Errorf("compiler replay: invalid engine %d", r.Engine)
	}
	if len(r.Module) == 0 {
		return fmt.Errorf("compiler replay: source module is empty")
	}
	if got := sha256.Sum256(r.Module); got != r.ModuleHash {
		return fmt.Errorf("compiler replay: module hash mismatch")
	}
	if got := r.Target.Fingerprint(); got != r.TargetFingerprint {
		return fmt.Errorf("compiler replay: target fingerprint mismatch")
	}
	if !r.Objective.Valid() || !r.Bounds.Valid() {
		return fmt.Errorf("compiler replay: invalid objective or bounds policy")
	}
	if r.Profile == nil {
		if r.ProfileHash != [32]byte{} {
			return fmt.Errorf("compiler replay: profile hash without profile")
		}
	} else {
		if err := r.Profile.Validate(); err != nil {
			return fmt.Errorf("compiler replay: %w", err)
		}
		got, err := profile.Hash(*r.Profile)
		if err != nil || got != r.ProfileHash {
			return fmt.Errorf("compiler replay: profile hash mismatch")
		}
		if r.Profile.ModuleHash != r.ModuleHash {
			return fmt.Errorf("compiler replay: profile module hash mismatch")
		}
	}
	for i, binding := range r.HostEffects {
		if !binding.Declared {
			continue
		}
		if err := binding.Contract.Validate(); err != nil {
			return fmt.Errorf("compiler replay: host effect binding %d: %w", i, err)
		}
	}
	if !slices.IsSorted(r.SelectedFunctions) {
		return fmt.Errorf("compiler replay: selected functions are not sorted")
	}
	for index := 1; index < len(r.SelectedFunctions); index++ {
		if r.SelectedFunctions[index-1] == r.SelectedFunctions[index] {
			return fmt.Errorf("compiler replay: selected function %d is repeated", r.SelectedFunctions[index])
		}
	}
	if r.Stage == "" {
		return fmt.Errorf("compiler replay: stage is empty")
	}
	if r.Error == "" {
		return fmt.Errorf("compiler replay: error is empty")
	}
	return nil
}

// MarshalReplay encodes a replay deterministically.
func MarshalReplay(r ReplayArtifact) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(r)
}

// UnmarshalReplay strictly decodes one replay document.
func UnmarshalReplay(data []byte) (ReplayArtifact, error) {
	var r ReplayArtifact
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return ReplayArtifact{}, fmt.Errorf("compiler replay: decode: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		if err == nil {
			return ReplayArtifact{}, fmt.Errorf("compiler replay: trailing JSON value")
		}
		return ReplayArtifact{}, fmt.Errorf("compiler replay: trailing data: %w", err)
	}
	if err := r.Validate(); err != nil {
		return ReplayArtifact{}, err
	}
	return r, nil
}
