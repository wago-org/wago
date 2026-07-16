//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

const stagedGCRefTestDeltaPath = "tests/spec-v3-staged-gc-ref-test.json"

type stagedGCRefTestClass uint8

const (
	stagedGCRefTestAbstract stagedGCRefTestClass = iota + 1
	stagedGCRefTestConcrete
)

func (c stagedGCRefTestClass) String() string {
	switch c {
	case stagedGCRefTestAbstract:
		return "abstract"
	case stagedGCRefTestConcrete:
		return "concrete"
	default:
		return "unknown"
	}
}

func (c stagedGCRefTestClass) gateReason() string {
	switch c {
	case stagedGCRefTestAbstract:
		return "abstract null/i31/struct/array/func/extern dynamic reference-test product"
	case stagedGCRefTestConcrete:
		return "concrete struct-subtyping dynamic reference-test product"
	default:
		return "unknown gc/ref_test product"
	}
}

type stagedGCRefTestLeaderPin struct {
	Filename    string
	CommandLine int
	SourceLine  int
	Size        int
	SHA256      string
	Class       stagedGCRefTestClass
	Actions     []string
}

var stagedGCRefTestLeaderPins = []stagedGCRefTestLeaderPin{
	{
		Filename: "ref_test.0.wasm", CommandLine: 43, SourceLine: 3, Size: 626,
		SHA256: "526d5c1b457f847daf51141a7d63aba11d20415b7ef2a13f593e06f680a41403",
		Class:  stagedGCRefTestAbstract,
		Actions: append([]string{"action:init"}, stagedGCRefTestRepeatedActions(
			[]string{"ref_test_null_data", "ref_test_any", "ref_test_eq", "ref_test_i31", "ref_test_struct", "ref_test_array"}, 8,
			[]string{"ref_test_null_func", "ref_test_func"}, 3,
			[]string{"ref_test_null_extern", "ref_test_extern"}, 6,
		)...),
	},
	{
		Filename: "ref_test.1.wasm", CommandLine: 174, SourceLine: 182, Size: 976,
		SHA256:  "7a71f9662207799b262ccbc7909f4e9492c04f7173f84f29be69905d925f6426",
		Class:   stagedGCRefTestConcrete,
		Actions: []string{"return:test-sub", "return:test-canon"},
	},
}

func stagedGCRefTestRepeatedActions(groups ...any) []string {
	var out []string
	for i := 0; i < len(groups); i += 2 {
		names := groups[i].([]string)
		count := groups[i+1].(int)
		for _, name := range names {
			for range count {
				out = append(out, "return:"+name)
			}
		}
	}
	return out
}

type stagedGCRefTestLeaderDelta struct {
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

type stagedGCRefTestDelta struct {
	Schema        int                             `json:"schema"`
	SuiteRevision string                          `json:"suite_revision"`
	File          string                          `json:"file"`
	Leaders       []stagedGCRefTestLeaderDelta    `json:"leaders"`
	Gates         []stagedTypedReferenceGateCount `json:"gates"`
	Counts        stagedSpecCounts                `json:"counts"`
}

func stagedGCRefTestLeaderPinFor(data []byte, line int) (stagedGCRefTestLeaderPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedGCRefTestLeaderPins {
		if pin.CommandLine == line && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedGCRefTestLeaderPin{}, false
}

func stagedGCRefTestLeaderDeltaFor(data []byte, line int) (stagedGCRefTestLeaderDelta, stagedGCRefTestLeaderPin, error) {
	pin, ok := stagedGCRefTestLeaderPinFor(data, line)
	if !ok {
		return stagedGCRefTestLeaderDelta{}, stagedGCRefTestLeaderPin{}, fmt.Errorf("unknown gc/ref_test binary at command line %d (size=%d)", line, len(data))
	}
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return stagedGCRefTestLeaderDelta{}, stagedGCRefTestLeaderPin{}, err
	}
	opcodes, err := stagedGCStructOpcodeInventory(m)
	if err != nil {
		return stagedGCRefTestLeaderDelta{}, stagedGCRefTestLeaderPin{}, err
	}
	return stagedGCRefTestLeaderDelta{
		Filename: pin.Filename, CommandLine: pin.CommandLine, SourceLine: pin.SourceLine,
		Size: pin.Size, SHA256: pin.SHA256, Class: pin.Class.String(), Gate: pin.Class.gateReason(),
		TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m), Opcodes: opcodes,
		Actions: append([]string(nil), pin.Actions...),
	}, pin, nil
}

func compileStagedGCRefTestAccounting(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func replayStagedGCRefTestScript(t *testing.T, tmp string, script stagedSpecScript) (stagedSpecCounts, []stagedGCRefTestLeaderDelta, map[string]int) {
	t.Helper()
	var counts stagedSpecCounts
	gates := map[string]int{}
	var latest []byte
	var current *stagedGCRefTestLeaderPin
	seenPins := map[string]bool{}
	seenActions := map[string][]string{}
	leaders := make([]stagedGCRefTestLeaderDelta, 0, len(stagedGCRefTestLeaderPins))

	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("gc/ref_test.wast:%d read definition: %v", cmd.Line, err)
				latest, current = nil, nil
				continue
			}
			latest, current = data, nil
		case "module_instance":
			leader, pin, err := stagedGCRefTestLeaderDeltaFor(latest, cmd.Line)
			if err != nil {
				counts.Failures++
				t.Error(err)
				continue
			}
			if seenPins[pin.Filename] {
				counts.Failures++
				t.Errorf("gc/ref_test.wast:%d duplicate leader %s", cmd.Line, pin.Filename)
				continue
			}
			seenPins[pin.Filename] = true
			leaders = append(leaders, leader)
			current = &pin
			c, compileErr := compileStagedGCRefTestAccounting(latest)
			if compileErr == nil {
				_ = c.Close()
				counts.Failures++
				t.Errorf("gc/ref_test.wast:%d unexpectedly compiled gated %s", cmd.Line, pin.Filename)
				continue
			}
			if strings.Contains(compileErr.Error(), "validate:") {
				counts.Failures++
				counts.UnexpectedCompileRejects++
				t.Errorf("gc/ref_test.wast:%d valid leader failed validation: %v", cmd.Line, compileErr)
				continue
			}
			counts.ExpectedFeatureRejects++
			gates[pin.Class.gateReason()]++
			t.Logf("gc/ref_test.wast:%d gated %s: %v", cmd.Line, pin.Filename, compileErr)
		case "assert_return", "assert_trap", "action":
			if current == nil {
				counts.Failures++
				t.Errorf("gc/ref_test.wast:%d action has no classified leader", cmd.Line)
				continue
			}
			kind := "action"
			if cmd.Type == "assert_return" {
				kind = "return"
			} else if cmd.Type == "assert_trap" {
				kind = "trap"
			}
			seenActions[current.Filename] = append(seenActions[current.Filename], kind+":"+cmd.Action.Field)
			counts.BlockedCommands++
		default:
			counts.Failures++
			t.Errorf("gc/ref_test.wast:%d unhandled command %q", cmd.Line, cmd.Type)
		}
	}
	if len(seenPins) != len(stagedGCRefTestLeaderPins) {
		counts.Failures++
		t.Errorf("gc/ref_test leader coverage = %d, want %d", len(seenPins), len(stagedGCRefTestLeaderPins))
	}
	for _, pin := range stagedGCRefTestLeaderPins {
		if !reflect.DeepEqual(seenActions[pin.Filename], pin.Actions) {
			counts.Failures++
			t.Errorf("%s action inventory = %v, want %v", pin.Filename, seenActions[pin.Filename], pin.Actions)
		}
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i].CommandLine < leaders[j].CommandLine })
	return counts, leaders, gates
}

func TestStagedOfficialGCRefTestAccounting(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/ref_test", &script)
	counts, leaders, gateCounts := replayStagedGCRefTestScript(t, tmp, script)
	if counts.Commands != 73 || counts.ModulesPassed != 0 || counts.AssertionsPassed != 0 || counts.ExpectedFeatureRejects != 2 || counts.BlockedCommands != 69 || counts.ExpectedInvalid != 0 || counts.ExpectedMalformed != 0 || counts.Failures != 0 || counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 {
		t.Fatalf("staged gc/ref_test accounting has hidden or changed gaps: %+v", counts)
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
	delta := stagedGCRefTestDelta{Schema: 2, SuiteRevision: stagedRelease3Revision, File: "gc/ref_test", Leaders: leaders, Gates: gates, Counts: counts}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedGCRefTestDeltaPath)
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
		t.Fatalf("read %s: %v", stagedGCRefTestDeltaPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("staged gc/ref_test accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact leaders and gates\n%s", got)
	}
}

func TestStagedGCRefTestLeaderPinsRejectUnknowns(t *testing.T) {
	if _, ok := stagedGCRefTestLeaderPinFor([]byte("not wasm"), 43); ok {
		t.Fatal("unknown gc/ref_test binary matched a leader pin")
	}
	for _, pin := range stagedGCRefTestLeaderPins {
		if pin.Class.gateReason() == "" {
			t.Fatalf("%s has empty gate reason", pin.Filename)
		}
	}
}
