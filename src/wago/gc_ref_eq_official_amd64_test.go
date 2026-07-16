//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

const stagedGCRefEqDeltaPath = "tests/spec-v3-staged-gc-ref-eq.json"
const stagedGCRefEqGate = "eqref table identity with rooted struct/array allocation"

type stagedGCRefEqLeaderPin struct {
	Filename    string
	CommandLine int
	SourceLine  int
	Size        int
	SHA256      string
	Actions     []string
}

var stagedGCRefEqLeader = stagedGCRefEqLeaderPin{
	Filename: "ref_eq.0.wasm", CommandLine: 16, SourceLine: 1, Size: 197,
	SHA256:  "46b2bd3e4597ba5a871472aa14f5777df18b722b7f3283ba1fc946f4791a3adb",
	Actions: append([]string{"action:init"}, stagedGCRefEqRepeatedActions("eq", 81)...),
}

type stagedGCRefEqInvalidPin struct {
	Filename    string `json:"filename"`
	CommandLine int    `json:"command_line"`
	SourceLine  int    `json:"source_line"`
	Size        int    `json:"size"`
	SHA256      string `json:"sha256"`
	Error       string `json:"error"`
}

var stagedGCRefEqInvalidPins = []stagedGCRefEqInvalidPin{
	{Filename: "ref_eq.1.wasm", CommandLine: 99, SourceLine: 108, Size: 60, SHA256: "d138d4e6e67efbe1e34854caa92237179b715422ca0d44dee9671fdd2228a2f2", Error: "type mismatch"},
	{Filename: "ref_eq.2.wasm", CommandLine: 108, SourceLine: 117, Size: 59, SHA256: "7dc8d28783ffb9c99c53fe8410b065af6516a879704607b144fbc6bb658e05bf", Error: "type mismatch"},
	{Filename: "ref_eq.3.wasm", CommandLine: 117, SourceLine: 126, Size: 60, SHA256: "181c61bfdfba706e366b6611af8ec8bbc5a8a641f2fb705459eec8adb72e6afb", Error: "type mismatch"},
	{Filename: "ref_eq.4.wasm", CommandLine: 126, SourceLine: 135, Size: 59, SHA256: "bed8ec7cbae1c0636774739fedc20475d65b1894c37983b59db974f2a1b36f33", Error: "type mismatch"},
	{Filename: "ref_eq.5.wasm", CommandLine: 135, SourceLine: 144, Size: 60, SHA256: "cb1ccf86d2eb6e21ad47bdd564e375ea1ac2fffaf42515f19e22841ab3cd85b9", Error: "type mismatch"},
	{Filename: "ref_eq.6.wasm", CommandLine: 144, SourceLine: 153, Size: 59, SHA256: "62d029f5bdb1b77e1ccebb1c24322e22c28e07879219ed10934bad24de2d5ac3", Error: "type mismatch"},
}

func stagedGCRefEqRepeatedActions(name string, count int) []string {
	out := make([]string, count)
	for i := range out {
		out[i] = "return:" + name
	}
	return out
}

type stagedGCRefEqLeaderDelta struct {
	Filename    string                      `json:"filename"`
	CommandLine int                         `json:"command_line"`
	SourceLine  int                         `json:"source_line"`
	Size        int                         `json:"size"`
	SHA256      string                      `json:"sha256"`
	Gate        string                      `json:"gate"`
	TypeGraph   string                      `json:"type_graph"`
	StateGraph  string                      `json:"state_graph"`
	Opcodes     []stagedGCStructOpcodeCount `json:"opcodes,omitempty"`
	Actions     []string                    `json:"actions"`
}

type stagedGCRefEqDelta struct {
	Schema        int                             `json:"schema"`
	SuiteRevision string                          `json:"suite_revision"`
	File          string                          `json:"file"`
	Leaders       []stagedGCRefEqLeaderDelta      `json:"leaders"`
	Invalids      []stagedGCRefEqInvalidPin       `json:"invalids"`
	Gates         []stagedTypedReferenceGateCount `json:"gates"`
	Counts        stagedSpecCounts                `json:"counts"`
}

func stagedGCRefEqLeaderPinFor(data []byte, line int) (stagedGCRefEqLeaderPin, bool) {
	pin := stagedGCRefEqLeader
	return pin, line == pin.CommandLine && len(data) == pin.Size && fmt.Sprintf("%x", sha256.Sum256(data)) == pin.SHA256
}

func stagedGCRefEqInvalidPinFor(data []byte, line int) (stagedGCRefEqInvalidPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedGCRefEqInvalidPins {
		if pin.CommandLine == line && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedGCRefEqInvalidPin{}, false
}

func stagedGCRefEqLeaderDeltaFor(data []byte, line int) (stagedGCRefEqLeaderDelta, stagedGCRefEqLeaderPin, error) {
	pin, ok := stagedGCRefEqLeaderPinFor(data, line)
	if !ok {
		return stagedGCRefEqLeaderDelta{}, stagedGCRefEqLeaderPin{}, fmt.Errorf("unknown gc/ref_eq binary at command line %d (size=%d)", line, len(data))
	}
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return stagedGCRefEqLeaderDelta{}, stagedGCRefEqLeaderPin{}, err
	}
	opcodes, err := stagedGCStructOpcodeInventory(m)
	if err != nil {
		return stagedGCRefEqLeaderDelta{}, stagedGCRefEqLeaderPin{}, err
	}
	gate := stagedGCRefEqGate
	return stagedGCRefEqLeaderDelta{
		Filename: pin.Filename, CommandLine: pin.CommandLine, SourceLine: pin.SourceLine,
		Size: pin.Size, SHA256: pin.SHA256, Gate: gate,
		TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m), Opcodes: opcodes,
		Actions: append([]string(nil), pin.Actions...),
	}, pin, nil
}

func compileStagedGCRefEqAccounting(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func replayStagedGCRefEqScript(t *testing.T, tmp string, script stagedSpecScript) (stagedSpecCounts, []stagedGCRefEqLeaderDelta, map[string]int) {
	t.Helper()
	var counts stagedSpecCounts
	gates := map[string]int{}
	var latest []byte
	var current *stagedGCRefEqLeaderPin
	var currentInstance *Instance
	var seenActions []string
	var leaders []stagedGCRefEqLeaderDelta
	seenInvalid := map[string]bool{}

	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module_definition":
			if currentInstance != nil {
				_ = currentInstance.Close()
				currentInstance = nil
			}
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("gc/ref_eq.wast:%d read definition: %v", cmd.Line, err)
				latest, current = nil, nil
				continue
			}
			latest, current = data, nil
		case "module_instance":
			leader, pin, err := stagedGCRefEqLeaderDeltaFor(latest, cmd.Line)
			if err != nil {
				counts.Failures++
				t.Error(err)
				continue
			}
			leaders = append(leaders, leader)
			current = &pin
			c, compileErr := compileStagedGCRefEqAccounting(latest)
			if compileErr == nil {
				in, instantiateErr := instantiateCore(c, InstantiateOptions{})
				_ = c.Close()
				if instantiateErr != nil {
					counts.Failures++
					t.Errorf("gc/ref_eq.wast:%d instantiate exact product: %v", cmd.Line, instantiateErr)
					continue
				}
				currentInstance = in
				counts.ModulesPassed++
				continue
			}
			if strings.Contains(compileErr.Error(), "validate:") {
				counts.Failures++
				counts.UnexpectedCompileRejects++
				t.Errorf("gc/ref_eq.wast:%d valid leader failed validation: %v", cmd.Line, compileErr)
				continue
			}
			counts.ExpectedFeatureRejects++
			gates[stagedGCRefEqGate]++
		case "action", "assert_return":
			if current == nil {
				counts.Failures++
				t.Errorf("gc/ref_eq.wast:%d action has no classified leader", cmd.Line)
				continue
			}
			kind := "action"
			if cmd.Type == "assert_return" {
				kind = "return"
			}
			seenActions = append(seenActions, kind+":"+cmd.Action.Field)
			if currentInstance == nil {
				counts.BlockedCommands++
				continue
			}
			args := make([]uint64, len(cmd.Action.Args))
			valid := true
			for i, arg := range cmd.Action.Args {
				args[i], valid = stagedSpecScalar(arg)
				if !valid {
					break
				}
			}
			if !valid {
				counts.Failures++
				t.Errorf("gc/ref_eq.wast:%d unsupported arguments %+v", cmd.Line, cmd.Action.Args)
				continue
			}
			got, callErr := currentInstance.Invoke(cmd.Action.Field, args...)
			if callErr != nil {
				counts.Failures++
				t.Errorf("gc/ref_eq.wast:%d %s = %v, %v", cmd.Line, cmd.Action.Field, got, callErr)
				continue
			}
			if cmd.Type == "action" {
				if len(got) != 0 {
					counts.Failures++
					t.Errorf("gc/ref_eq.wast:%d %s = %v, want empty action result", cmd.Line, cmd.Action.Field, got)
				}
				continue
			}
			if len(got) != len(cmd.Expected) || len(got) != 1 || !stagedSpecMatch(got[0], cmd.Expected[0]) {
				counts.Failures++
				t.Errorf("gc/ref_eq.wast:%d %s = %v, want %v", cmd.Line, cmd.Action.Field, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		case "assert_invalid":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("gc/ref_eq.wast:%d read invalid module: %v", cmd.Line, err)
				continue
			}
			pin, ok := stagedGCRefEqInvalidPinFor(data, cmd.Line)
			if !ok {
				counts.Failures++
				t.Errorf("gc/ref_eq.wast:%d unknown invalid binary size=%d", cmd.Line, len(data))
				continue
			}
			m, decodeErr := corewasm.DecodeModule(data)
			if decodeErr != nil {
				counts.Failures++
				t.Errorf("gc/ref_eq.wast:%d invalid binary failed decode: %v", cmd.Line, decodeErr)
				continue
			}
			validationErr := corewasm.ValidateModule(m)
			var verr *corewasm.ValidationError
			if !errors.As(validationErr, &verr) || verr.Code != corewasm.ErrTypeMismatch {
				counts.Failures++
				t.Errorf("gc/ref_eq.wast:%d validation = %v, want %s", cmd.Line, validationErr, pin.Error)
				continue
			}
			seenInvalid[pin.Filename] = true
			counts.ExpectedInvalid++
		default:
			counts.Failures++
			t.Errorf("gc/ref_eq.wast:%d unhandled command %q", cmd.Line, cmd.Type)
		}
	}
	if currentInstance != nil {
		_ = currentInstance.Close()
	}
	if len(leaders) != 1 {
		counts.Failures++
		t.Errorf("gc/ref_eq leader coverage = %d, want 1", len(leaders))
	}
	if len(seenInvalid) != len(stagedGCRefEqInvalidPins) {
		counts.Failures++
		t.Errorf("gc/ref_eq invalid coverage = %d, want %d", len(seenInvalid), len(stagedGCRefEqInvalidPins))
	}
	if !reflect.DeepEqual(seenActions, stagedGCRefEqLeader.Actions) {
		counts.Failures++
		t.Errorf("gc/ref_eq action inventory = %v, want %v", seenActions, stagedGCRefEqLeader.Actions)
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i].CommandLine < leaders[j].CommandLine })
	return counts, leaders, gates
}

func TestStagedOfficialGCRefEqAccounting(t *testing.T) {
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/ref_eq", &script)
	counts, leaders, gateCounts := replayStagedGCRefEqScript(t, tmp, script)
	if counts.Commands != 90 || counts.ModulesPassed != 0 || counts.AssertionsPassed != 0 || counts.ExpectedFeatureRejects != 1 || counts.BlockedCommands != 82 || counts.ExpectedInvalid != 6 || counts.ExpectedMalformed != 0 || counts.Failures != 0 || counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 {
		t.Fatalf("staged gc/ref_eq accounting has hidden or changed gaps: %+v", counts)
	}
	gateNames := make([]string, 0, len(gateCounts))
	for name := range gateCounts {
		gateNames = append(gateNames, name)
	}
	sort.Strings(gateNames)
	gates := make([]stagedTypedReferenceGateCount, 0, len(gateNames))
	for _, name := range gateNames {
		gates = append(gates, stagedTypedReferenceGateCount{Family: "gc", Reason: name, Count: gateCounts[name]})
	}
	delta := stagedGCRefEqDelta{Schema: 2, SuiteRevision: stagedRelease3Revision, File: "gc/ref_eq", Leaders: leaders, Invalids: stagedGCRefEqInvalidPins, Gates: gates, Counts: counts}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedGCRefEqDeltaPath)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("WAGO_UPDATE_STAGED_SPEC") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", stagedGCRefEqDeltaPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("staged gc/ref_eq accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact leader and invalid obligations\n%s", got)
	}
}

func TestStagedGCRefEqPinsRejectUnknowns(t *testing.T) {
	if _, ok := stagedGCRefEqLeaderPinFor([]byte("not wasm"), stagedGCRefEqLeader.CommandLine); ok {
		t.Fatal("unknown gc/ref_eq binary matched the leader pin")
	}
	if _, ok := stagedGCRefEqInvalidPinFor([]byte("not wasm"), stagedGCRefEqInvalidPins[0].CommandLine); ok {
		t.Fatal("unknown gc/ref_eq invalid binary matched a pin")
	}
	if stagedGCRefEqGate == "" {
		t.Fatal("gc/ref_eq gate reason is empty")
	}
}
