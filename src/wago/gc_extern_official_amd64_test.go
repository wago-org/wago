//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

const stagedGCExternDeltaPath = "tests/spec-v3-staged-gc-extern.json"

const stagedGCExternGate = "extern conversion constant globals/table with bounded anyref ingress and result ownership"

type stagedGCExternLeaderPin struct {
	Filename    string
	CommandLine int
	SourceLine  int
	Size        int
	SHA256      string
	Actions     []string
}

var stagedGCExternLeader = stagedGCExternLeaderPin{
	Filename: "extern.0.wasm", CommandLine: 21, SourceLine: 1, Size: 286,
	SHA256: "5ad921ebe511ca9e23c137aef6883113684896f15b8a9726d5d77524d562f823",
	Actions: []string{
		"action:init",
		"return:internalize", "return:internalize",
		"return:externalize", "return:externalize",
		"return:externalize-i", "return:externalize-i", "return:externalize-i",
		"return:externalize-i", "return:externalize-i", "return:externalize-i",
		"return:externalize-ii", "return:externalize-ii", "return:externalize-ii",
		"return:externalize-ii", "return:externalize-ii", "return:externalize-ii",
	},
}

type stagedGCExternLeaderDelta struct {
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

type stagedGCExternDelta struct {
	Schema        int                             `json:"schema"`
	SuiteRevision string                          `json:"suite_revision"`
	File          string                          `json:"file"`
	Leaders       []stagedGCExternLeaderDelta     `json:"leaders"`
	Gates         []stagedTypedReferenceGateCount `json:"gates"`
	Counts        stagedSpecCounts                `json:"counts"`
}

func stagedGCExternLeaderPinFor(data []byte, line int) (stagedGCExternLeaderPin, bool) {
	pin := stagedGCExternLeader
	return pin, line == pin.CommandLine && len(data) == pin.Size && fmt.Sprintf("%x", sha256.Sum256(data)) == pin.SHA256
}

func stagedGCExternLeaderDeltaFor(data []byte, line int) (stagedGCExternLeaderDelta, stagedGCExternLeaderPin, error) {
	pin, ok := stagedGCExternLeaderPinFor(data, line)
	if !ok {
		return stagedGCExternLeaderDelta{}, stagedGCExternLeaderPin{}, fmt.Errorf("unknown gc/extern binary at command line %d (size=%d)", line, len(data))
	}
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return stagedGCExternLeaderDelta{}, stagedGCExternLeaderPin{}, err
	}
	opcodes, err := stagedGCStructOpcodeInventory(m)
	if err != nil {
		return stagedGCExternLeaderDelta{}, stagedGCExternLeaderPin{}, err
	}
	gate := stagedGCExternGate
	if product, ok := stagedGCStructExecutionProduct(data); ok && product == stagedGCStructExtern {
		gate = ""
	}
	return stagedGCExternLeaderDelta{
		Filename: pin.Filename, CommandLine: pin.CommandLine, SourceLine: pin.SourceLine,
		Size: pin.Size, SHA256: pin.SHA256, Gate: gate,
		TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m), Opcodes: opcodes,
		Actions: append([]string(nil), pin.Actions...),
	}, pin, nil
}

func compileStagedGCExternAccounting(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCStructProducts = true
	features.GCArrayProducts = true
	features.GCI31Products = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func stagedGCExternValueString(v stagedSpecValue) (string, bool) {
	var value string
	if err := json.Unmarshal(v.Value, &value); err != nil {
		return "", false
	}
	return value, true
}

func stagedGCExternMatch(in *Instance, tokens map[string]uint64, got uint64, want stagedSpecValue) bool {
	value, ok := stagedGCExternValueString(want)
	switch want.Type {
	case "externref":
		if !ok {
			return false
		}
		if value == "null" {
			return got == 0
		}
		if value == "0" { // ref.extern wildcard in the official source.
			return got != 0
		}
		return got != 0 && got == tokens[value]
	case "ref":
		if want.HeapType == "" {
			if !ok {
				return false
			}
			if value == "null" {
				return got == 0
			}
			return got != 0 && got == tokens[value]
		}
		if got == 0 || in == nil {
			return false
		}
		conversion := in.existingGCExternConversionState()
		if conversion == nil {
			return false
		}
		internal, err := conversion.internalAnyFromPublic(got)
		if err != nil || internal != uint64(uint32(internal)) {
			return false
		}
		ref := gc.Ref(uint32(internal))
		switch want.HeapType {
		case "i31":
			return ref.IsI31()
		case "struct", "array":
			if !ref.IsObj() {
				return false
			}
			typeID, err := in.gc.ObjectType(ref)
			if err != nil || int(typeID) >= len(in.c.Types) {
				return false
			}
			kind := in.c.Types[typeID].Kind
			return (want.HeapType == "struct" && kind == CompositeTypeStruct) || (want.HeapType == "array" && kind == CompositeTypeArray)
		}
	}
	return false
}

func replayStagedGCExternScript(t *testing.T, tmp string, script stagedSpecScript) (stagedSpecCounts, []stagedGCExternLeaderDelta, map[string]int) {
	t.Helper()
	var counts stagedSpecCounts
	gates := map[string]int{}
	var latest []byte
	var current *stagedGCExternLeaderPin
	var seenActions []string
	var leaders []stagedGCExternLeaderDelta
	var currentInstance *Instance
	hostTokens := map[string]uint64{}

	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module_definition":
			if currentInstance != nil {
				_ = currentInstance.Close()
				currentInstance = nil
			}
			clear(hostTokens)
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("gc/extern.wast:%d read definition: %v", cmd.Line, err)
				latest, current = nil, nil
				continue
			}
			latest, current = data, nil
		case "module_instance":
			leader, pin, err := stagedGCExternLeaderDeltaFor(latest, cmd.Line)
			if err != nil {
				counts.Failures++
				t.Error(err)
				continue
			}
			leaders = append(leaders, leader)
			current = &pin
			c, compileErr := compileStagedGCExternAccounting(latest)
			if compileErr == nil {
				if c.stagedGCStructProduct() != stagedGCStructExtern {
					_ = c.Close()
					counts.Failures++
					t.Errorf("gc/extern.wast:%d compiled as product %s", cmd.Line, c.stagedGCStructProduct())
					continue
				}
				in, instantiateErr := instantiateCore(c, InstantiateOptions{})
				_ = c.Close()
				if instantiateErr != nil {
					counts.Failures++
					t.Errorf("gc/extern.wast:%d instantiate exact product: %v", cmd.Line, instantiateErr)
					continue
				}
				for _, identity := range []string{"0", "1", "2"} {
					ref, issueErr := in.NewExternRef(identity)
					if issueErr != nil {
						_ = in.Close()
						counts.Failures++
						t.Errorf("gc/extern.wast:%d issue host identity %s: %v", cmd.Line, identity, issueErr)
						in = nil
						break
					}
					hostTokens[identity] = ValueExternRef(ref).Bits()
				}
				if in == nil {
					continue
				}
				currentInstance = in
				counts.ModulesPassed++
				continue
			}
			if !strings.Contains(compileErr.Error(), "constant expression required: ExternConvertAny") && !strings.Contains(compileErr.Error(), "outside the exact pinned product set") {
				counts.Failures++
				counts.UnexpectedCompileRejects++
				t.Errorf("gc/extern.wast:%d changed compile gate: %v", cmd.Line, compileErr)
				continue
			}
			counts.ExpectedFeatureRejects++
			gates[stagedGCExternGate]++
		case "assert_return", "action":
			if current == nil {
				counts.Failures++
				t.Errorf("gc/extern.wast:%d action has no classified leader", cmd.Line)
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
				switch arg.Type {
				case "i32":
					args[i], valid = stagedSpecScalar(arg)
				case "externref", "ref":
					value, ok := stagedGCExternValueString(arg)
					if !ok {
						valid = false
						break
					}
					if value != "null" {
						args[i], valid = hostTokens[value]
					}
				default:
					valid = false
				}
				if !valid {
					break
				}
			}
			if !valid {
				counts.Failures++
				t.Errorf("gc/extern.wast:%d unsupported arguments %+v", cmd.Line, cmd.Action.Args)
				continue
			}
			got, callErr := currentInstance.Invoke(cmd.Action.Field, args...)
			if callErr != nil {
				counts.Failures++
				t.Errorf("gc/extern.wast:%d %s = %v, %v", cmd.Line, cmd.Action.Field, got, callErr)
				continue
			}
			if cmd.Type == "action" {
				if len(got) != 0 {
					counts.Failures++
					t.Errorf("gc/extern.wast:%d %s = %v, want empty action result", cmd.Line, cmd.Action.Field, got)
				}
				continue
			}
			if len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("gc/extern.wast:%d %s = %v, want %v", cmd.Line, cmd.Action.Field, got, cmd.Expected)
				continue
			}
			matched := true
			for i := range got {
				if !stagedGCExternMatch(currentInstance, hostTokens, got[i], cmd.Expected[i]) {
					matched = false
					break
				}
			}
			if !matched {
				counts.Failures++
				t.Errorf("gc/extern.wast:%d %s = %v, want %v", cmd.Line, cmd.Action.Field, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		default:
			counts.Failures++
			t.Errorf("gc/extern.wast:%d unhandled command %q", cmd.Line, cmd.Type)
		}
	}
	if currentInstance != nil {
		_ = currentInstance.Close()
	}
	if len(leaders) != 1 {
		counts.Failures++
		t.Errorf("gc/extern leader coverage = %d, want 1", len(leaders))
	}
	if !reflect.DeepEqual(seenActions, stagedGCExternLeader.Actions) {
		counts.Failures++
		t.Errorf("gc/extern action inventory = %v, want %v", seenActions, stagedGCExternLeader.Actions)
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i].CommandLine < leaders[j].CommandLine })
	return counts, leaders, gates
}

func TestStagedOfficialGCExternAccounting(t *testing.T) {
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/extern", &script)
	counts, leaders, gateCounts := replayStagedGCExternScript(t, tmp, script)
	if counts.Commands != 19 || counts.ModulesPassed != 1 || counts.AssertionsPassed != 16 || counts.ExpectedFeatureRejects != 0 || counts.BlockedCommands != 0 || counts.ExpectedInvalid != 0 || counts.ExpectedMalformed != 0 || counts.Failures != 0 || counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 {
		t.Fatalf("staged gc/extern accounting has hidden or changed gaps: %+v", counts)
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
	delta := stagedGCExternDelta{Schema: 2, SuiteRevision: stagedRelease3Revision, File: "gc/extern", Leaders: leaders, Gates: gates, Counts: counts}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedGCExternDeltaPath)
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
		t.Fatalf("read %s: %v", stagedGCExternDeltaPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("staged gc/extern accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing the exact leader and gate\n%s", got)
	}
}

func TestStagedGCExternLeaderPinRejectsUnknowns(t *testing.T) {
	if _, ok := stagedGCExternLeaderPinFor([]byte("not wasm"), stagedGCExternLeader.CommandLine); ok {
		t.Fatal("unknown gc/extern binary matched the leader pin")
	}
	if stagedGCExternGate == "" {
		t.Fatal("gc/extern gate reason is empty")
	}
}
