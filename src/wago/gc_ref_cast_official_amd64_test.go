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
)

const (
	stagedGCRefCastDeltaPath         = "tests/spec-v3-staged-gc-ref-cast.json"
	stagedGCRefCastOfficialExecution = true
)

type stagedGCRefCastClass uint8

const (
	stagedGCRefCastAbstract stagedGCRefCastClass = iota + 1
	stagedGCRefCastConcrete
)

func (c stagedGCRefCastClass) String() string {
	switch c {
	case stagedGCRefCastAbstract:
		return "abstract"
	case stagedGCRefCastConcrete:
		return "concrete"
	default:
		return "unknown"
	}
}

func (c stagedGCRefCastClass) gateReason() string {
	switch c {
	case stagedGCRefCastAbstract:
		return "abstract null/i31/struct/array/foreign-any reference-cast product"
	case stagedGCRefCastConcrete:
		return "concrete declared-super/canonical reference-cast product"
	default:
		return "unknown gc/ref_cast product"
	}
}

type stagedGCRefCastLeaderPin struct {
	Filename    string
	CommandLine int
	SourceLine  int
	Size        int
	SHA256      string
	Class       stagedGCRefCastClass
	Actions     []string
}

func stagedGCRefCastAbstractActions() []string {
	out := []string{"action:init"}
	appendGroup := func(field string, outcomes ...string) {
		for _, outcome := range outcomes {
			out = append(out, outcome+":"+field)
		}
	}
	appendGroup("ref_cast_non_null", "trap:null reference", "return", "return", "return", "return", "trap:null reference", "trap:null reference", "trap:null reference")
	appendGroup("ref_cast_null", "return", "trap:cast failure", "trap:cast failure", "trap:cast failure", "trap:cast failure", "return", "return", "return")
	appendGroup("ref_cast_i31", "trap:cast failure", "return", "trap:cast failure", "trap:cast failure", "trap:cast failure", "trap:cast failure", "trap:cast failure", "trap:cast failure")
	appendGroup("ref_cast_struct", "trap:cast failure", "trap:cast failure", "return", "trap:cast failure", "trap:cast failure", "trap:cast failure", "trap:cast failure", "trap:cast failure")
	appendGroup("ref_cast_array", "trap:cast failure", "trap:cast failure", "trap:cast failure", "return", "trap:cast failure", "trap:cast failure", "trap:cast failure", "trap:cast failure")
	return out
}

var stagedGCRefCastLeaderPins = []stagedGCRefCastLeaderPin{
	{
		Filename: "ref_cast.0.wasm", CommandLine: 27, SourceLine: 3, Size: 380,
		SHA256: "c85556089bf1a39cb3082f7de916c00eaa2482253cf126d8a9fc09ab970eed4b",
		Class:  stagedGCRefCastAbstract, Actions: stagedGCRefCastAbstractActions(),
	},
	{
		Filename: "ref_cast.1.wasm", CommandLine: 103, SourceLine: 99, Size: 512,
		SHA256: "65f1f33b335ca62d90ad089a74f8a29ea3163f9a3a2f53096bdeac9e4b86f4a6",
		Class:  stagedGCRefCastConcrete, Actions: []string{"action:test-sub", "action:test-canon"},
	},
}

type stagedGCRefCastLeaderDelta struct {
	Filename    string                      `json:"filename"`
	CommandLine int                         `json:"command_line"`
	SourceLine  int                         `json:"source_line"`
	Size        int                         `json:"size"`
	SHA256      string                      `json:"sha256"`
	Class       string                      `json:"class"`
	Gate        string                      `json:"gate"`
	TypeGraph   string                      `json:"type_graph"`
	StateGraph  string                      `json:"state_graph"`
	Opcodes     []stagedGCStructOpcodeCount `json:"opcodes,omitempty"`
	Actions     []string                    `json:"actions"`
}

type stagedGCRefCastDelta struct {
	Schema        int                             `json:"schema"`
	SuiteRevision string                          `json:"suite_revision"`
	File          string                          `json:"file"`
	Leaders       []stagedGCRefCastLeaderDelta    `json:"leaders"`
	Gates         []stagedTypedReferenceGateCount `json:"gates"`
	Counts        stagedSpecCounts                `json:"counts"`
}

func stagedGCRefCastLeaderPinFor(data []byte, line int) (stagedGCRefCastLeaderPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedGCRefCastLeaderPins {
		if pin.CommandLine == line && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedGCRefCastLeaderPin{}, false
}

func stagedGCRefCastLeaderDeltaFor(data []byte, line int) (stagedGCRefCastLeaderDelta, stagedGCRefCastLeaderPin, error) {
	pin, ok := stagedGCRefCastLeaderPinFor(data, line)
	if !ok {
		return stagedGCRefCastLeaderDelta{}, stagedGCRefCastLeaderPin{}, fmt.Errorf("unknown gc/ref_cast binary at command line %d (size=%d)", line, len(data))
	}
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return stagedGCRefCastLeaderDelta{}, stagedGCRefCastLeaderPin{}, err
	}
	opcodes, err := stagedGCStructOpcodeInventory(m)
	if err != nil {
		return stagedGCRefCastLeaderDelta{}, stagedGCRefCastLeaderPin{}, err
	}
	gate := pin.Class.gateReason()
	if product, ok := stagedGCStructExecutionProduct(data); stagedGCRefCastOfficialExecution && ok && ((pin.Class == stagedGCRefCastAbstract && product == stagedGCStructRefCastAbstract) || (pin.Class == stagedGCRefCastConcrete && product == stagedGCStructRefCastConcrete)) {
		gate = ""
	}
	return stagedGCRefCastLeaderDelta{
		Filename: pin.Filename, CommandLine: pin.CommandLine, SourceLine: pin.SourceLine,
		Size: pin.Size, SHA256: pin.SHA256, Class: pin.Class.String(), Gate: gate,
		TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m), Opcodes: opcodes,
		Actions: append([]string(nil), pin.Actions...),
	}, pin, nil
}

func compileStagedGCRefCastAccounting(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	if stagedGCRefCastOfficialExecution {
		features.GCStructProducts = true
		features.GCI31Products = true
		if product, ok := stagedGCStructExecutionProduct(data); ok && product == stagedGCStructRefCastAbstract {
			features.GCArrayProducts = true
		}
	}
	return compileWithFrontendFeatures(cfg, data, features)
}

func stagedGCRefCastActionKey(cmd stagedSpecCommand) string {
	if cmd.Type == "assert_trap" {
		return "trap:" + cmd.Text + ":" + cmd.Action.Field
	}
	kind := "action"
	if cmd.Type == "assert_return" {
		kind = "return"
	}
	return kind + ":" + cmd.Action.Field
}

func replayStagedGCRefCastScript(t *testing.T, tmp string, script stagedSpecScript) (stagedSpecCounts, []stagedGCRefCastLeaderDelta, map[string]int) {
	t.Helper()
	var counts stagedSpecCounts
	gates := map[string]int{}
	var latest []byte
	var current *stagedGCRefCastLeaderPin
	var currentInstance *Instance
	var currentExtern uint64
	seenPins := map[string]bool{}
	seenActions := map[string][]string{}
	leaders := make([]stagedGCRefCastLeaderDelta, 0, len(stagedGCRefCastLeaderPins))

	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module_definition":
			if currentInstance != nil {
				_ = currentInstance.Close()
				currentInstance = nil
				currentExtern = 0
			}
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("gc/ref_cast.wast:%d read definition: %v", cmd.Line, err)
				latest, current = nil, nil
				continue
			}
			latest, current = data, nil
		case "module_instance":
			leader, pin, err := stagedGCRefCastLeaderDeltaFor(latest, cmd.Line)
			if err != nil {
				counts.Failures++
				t.Error(err)
				continue
			}
			if seenPins[pin.Filename] {
				counts.Failures++
				t.Errorf("gc/ref_cast.wast:%d duplicate leader %s", cmd.Line, pin.Filename)
				continue
			}
			seenPins[pin.Filename] = true
			leaders = append(leaders, leader)
			current = &pin
			c, compileErr := compileStagedGCRefCastAccounting(latest)
			if compileErr == nil {
				product := c.stagedGCStructProduct()
				want := stagedGCStructRefCastAbstract
				if pin.Class == stagedGCRefCastConcrete {
					want = stagedGCStructRefCastConcrete
				}
				if product != want {
					_ = c.Close()
					counts.Failures++
					t.Errorf("gc/ref_cast.wast:%d compiled as product %s, want %s", cmd.Line, product, want)
					continue
				}
				in, instantiateErr := instantiateCore(c, InstantiateOptions{})
				_ = c.Close()
				if instantiateErr != nil {
					counts.Failures++
					t.Errorf("gc/ref_cast.wast:%d instantiate %s: %v", cmd.Line, pin.Filename, instantiateErr)
					continue
				}
				if pin.Class == stagedGCRefCastAbstract {
					ref, refErr := in.NewExternRef("0")
					if refErr != nil {
						_ = in.Close()
						counts.Failures++
						t.Errorf("gc/ref_cast.wast:%d create extern fixture: %v", cmd.Line, refErr)
						continue
					}
					currentExtern = ValueExternRef(ref).Bits()
				}
				currentInstance = in
				counts.ModulesPassed++
				continue
			}
			if !strings.Contains(compileErr.Error(), "outside the exact pinned product set") && !strings.Contains(compileErr.Error(), "gc disabled") {
				counts.Failures++
				counts.UnexpectedCompileRejects++
				t.Errorf("gc/ref_cast.wast:%d changed compile gate: %v", cmd.Line, compileErr)
				continue
			}
			counts.ExpectedFeatureRejects++
			gates[pin.Class.gateReason()]++
		case "assert_return", "assert_trap", "action":
			if current == nil {
				counts.Failures++
				t.Errorf("gc/ref_cast.wast:%d action has no classified leader", cmd.Line)
				continue
			}
			seenActions[current.Filename] = append(seenActions[current.Filename], stagedGCRefCastActionKey(cmd))
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
				case "externref":
					args[i] = currentExtern
				default:
					valid = false
				}
				if !valid {
					break
				}
			}
			if !valid {
				counts.Failures++
				t.Errorf("gc/ref_cast.wast:%d unsupported arguments %+v", cmd.Line, cmd.Action.Args)
				continue
			}
			got, callErr := currentInstance.Invoke(cmd.Action.Field, args...)
			switch cmd.Type {
			case "assert_trap":
				if callErr == nil || !strings.Contains(callErr.Error(), cmd.Text) {
					counts.Failures++
					t.Errorf("gc/ref_cast.wast:%d %s = %v, %v; want trap %q", cmd.Line, cmd.Action.Field, got, callErr, cmd.Text)
					continue
				}
				counts.AssertionsPassed++
			case "assert_return":
				if callErr != nil || len(got) != len(cmd.Expected) {
					counts.Failures++
					t.Errorf("gc/ref_cast.wast:%d %s = %v, %v; want empty return", cmd.Line, cmd.Action.Field, got, callErr)
					continue
				}
				counts.AssertionsPassed++
			case "action":
				if callErr != nil || len(got) != 0 {
					counts.Failures++
					t.Errorf("gc/ref_cast.wast:%d %s = %v, %v; want empty action result", cmd.Line, cmd.Action.Field, got, callErr)
				}
			}
		default:
			counts.Failures++
			t.Errorf("gc/ref_cast.wast:%d unhandled command %q", cmd.Line, cmd.Type)
		}
	}
	if currentInstance != nil {
		_ = currentInstance.Close()
	}
	if len(seenPins) != len(stagedGCRefCastLeaderPins) {
		counts.Failures++
		t.Errorf("gc/ref_cast leader coverage = %d, want %d", len(seenPins), len(stagedGCRefCastLeaderPins))
	}
	for _, pin := range stagedGCRefCastLeaderPins {
		if !reflect.DeepEqual(seenActions[pin.Filename], pin.Actions) {
			counts.Failures++
			t.Errorf("%s action inventory = %v, want %v", pin.Filename, seenActions[pin.Filename], pin.Actions)
		}
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i].CommandLine < leaders[j].CommandLine })
	return counts, leaders, gates
}

func TestStagedOfficialGCRefCastAccounting(t *testing.T) {
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/ref_cast", &script)
	counts, leaders, gateCounts := replayStagedGCRefCastScript(t, tmp, script)
	if counts.Commands != 47 || counts.ModulesPassed != 2 || counts.AssertionsPassed != 40 || counts.ExpectedFeatureRejects != 0 || counts.BlockedCommands != 0 || counts.ExpectedInvalid != 0 || counts.ExpectedMalformed != 0 || counts.Failures != 0 || counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 {
		t.Fatalf("staged gc/ref_cast accounting has hidden or changed gaps: %+v", counts)
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
	delta := stagedGCRefCastDelta{Schema: 2, SuiteRevision: stagedRelease3Revision, File: "gc/ref_cast", Leaders: leaders, Gates: gates, Counts: counts}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedGCRefCastDeltaPath)
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
		t.Fatalf("read %s: %v", stagedGCRefCastDeltaPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("staged gc/ref_cast accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact leaders and actions\n%s", got)
	}
}

func TestStagedGCRefCastLeaderPinsRejectUnknowns(t *testing.T) {
	if _, ok := stagedGCRefCastLeaderPinFor([]byte("not wasm"), stagedGCRefCastLeaderPins[0].CommandLine); ok {
		t.Fatal("unknown gc/ref_cast binary matched a leader pin")
	}
	for _, pin := range stagedGCRefCastLeaderPins {
		if pin.Class.gateReason() == "" {
			t.Fatalf("%s has empty gate reason", pin.Filename)
		}
	}
}
