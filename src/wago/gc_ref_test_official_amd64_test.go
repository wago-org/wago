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
	gate := pin.Class.gateReason()
	if product, ok := stagedGCStructExecutionProduct(data); ok && (product == stagedGCStructRefTestConcrete || product == stagedGCStructRefTestAbstract) {
		gate = ""
	}
	return stagedGCRefTestLeaderDelta{
		Filename: pin.Filename, CommandLine: pin.CommandLine, SourceLine: pin.SourceLine,
		Size: pin.Size, SHA256: pin.SHA256, Class: pin.Class.String(), Gate: gate,
		TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m), Opcodes: opcodes,
		Actions: append([]string(nil), pin.Actions...),
	}, pin, nil
}

func compileStagedGCRefTestAccounting(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	if product, ok := stagedGCStructExecutionProduct(data); ok && (product == stagedGCStructRefTestConcrete || product == stagedGCStructRefTestAbstract) {
		features.GCStructProducts = true
		if product == stagedGCStructRefTestAbstract {
			features.GCArrayProducts = true
			features.GCI31Products = true
		}
	}
	return compileWithFrontendFeatures(cfg, data, features)
}

func replayStagedGCRefTestScript(t *testing.T, tmp string, script stagedSpecScript) (stagedSpecCounts, []stagedGCRefTestLeaderDelta, map[string]int) {
	t.Helper()
	var counts stagedSpecCounts
	gates := map[string]int{}
	var latest []byte
	var current *stagedGCRefTestLeaderPin
	var currentInstance *Instance
	var currentExtern uint64
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
			if currentInstance != nil {
				_ = currentInstance.Close()
				currentInstance = nil
				currentExtern = 0
			}
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
				if pin.Class != stagedGCRefTestConcrete && pin.Class != stagedGCRefTestAbstract {
					_ = c.Close()
					counts.Failures++
					t.Errorf("gc/ref_test.wast:%d unexpectedly compiled unknown %s", cmd.Line, pin.Filename)
					continue
				}
				if blob, marshalErr := marshalCompiled(c); marshalErr == nil {
					t.Logf("gc/ref_test %s product: wasm=%d code=%d codec=%d", pin.Class, len(latest), len(c.Code), len(blob))
				} else {
					counts.Failures++
					t.Errorf("gc/ref_test.wast:%d %s codec measurement: %v", cmd.Line, pin.Class, marshalErr)
					_ = c.Close()
					continue
				}
				if pin.Class == stagedGCRefTestConcrete {
					if proofErr := verifyStagedGCRefTestConcreteProduct(c); proofErr != nil {
						_ = c.Close()
						counts.Failures++
						t.Errorf("gc/ref_test.wast:%d concrete lifecycle proof: %v", cmd.Line, proofErr)
						continue
					}
				}
				in, instantiateErr := instantiateCore(c, InstantiateOptions{})
				_ = c.Close()
				if instantiateErr != nil {
					counts.Failures++
					t.Errorf("gc/ref_test.wast:%d instantiate %s: %v", cmd.Line, pin.Filename, instantiateErr)
					continue
				}
				if pin.Class == stagedGCRefTestAbstract {
					ref, refErr := in.NewExternRef("0")
					if refErr != nil {
						_ = in.Close()
						counts.Failures++
						t.Errorf("gc/ref_test.wast:%d create extern fixture: %v", cmd.Line, refErr)
						continue
					}
					currentExtern = ValueExternRef(ref).Bits()
				}
				currentInstance = in
				counts.ModulesPassed++
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
			if currentInstance != nil {
				args := make([]uint64, len(cmd.Action.Args))
				validArgs := true
				for i, arg := range cmd.Action.Args {
					switch arg.Type {
					case "i32":
						args[i], validArgs = stagedSpecScalar(arg)
					case "externref":
						args[i] = currentExtern
					default:
						validArgs = false
					}
					if !validArgs {
						break
					}
				}
				if !validArgs {
					counts.Failures++
					t.Errorf("gc/ref_test.wast:%d has unsupported arguments %+v", cmd.Line, cmd.Action.Args)
					continue
				}
				got, callErr := currentInstance.Invoke(cmd.Action.Field, args...)
				if callErr != nil {
					counts.Failures++
					t.Errorf("gc/ref_test.wast:%d %s = %v, %v", cmd.Line, cmd.Action.Field, got, callErr)
					continue
				}
				if cmd.Type == "action" {
					if len(got) != 0 {
						counts.Failures++
						t.Errorf("gc/ref_test.wast:%d %s = %v, want empty action return", cmd.Line, cmd.Action.Field, got)
					}
					continue
				}
				if cmd.Type != "assert_return" || len(got) != len(cmd.Expected) {
					counts.Failures++
					t.Errorf("gc/ref_test.wast:%d executable action has unsupported shape %s got=%v expected=%v", cmd.Line, cmd.Type, got, cmd.Expected)
					continue
				}
				matched := true
				for i := range got {
					if !stagedSpecMatch(got[i], cmd.Expected[i]) {
						matched = false
						break
					}
				}
				if !matched {
					counts.Failures++
					t.Errorf("gc/ref_test.wast:%d %s = %v, want %v", cmd.Line, cmd.Action.Field, got, cmd.Expected)
					continue
				}
				counts.AssertionsPassed++
				continue
			}
			counts.BlockedCommands++
		default:
			counts.Failures++
			t.Errorf("gc/ref_test.wast:%d unhandled command %q", cmd.Line, cmd.Type)
		}
	}
	if currentInstance != nil {
		_ = currentInstance.Close()
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

func verifyStagedGCRefTestConcreteProduct(c *Compiled) error {
	in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}})
	if err != nil {
		return err
	}
	state := in.existingGCRefTestTableState()
	if state == nil || state.Count != 20 || state.CanonicalType == nil {
		_ = in.Close()
		return fmt.Errorf("table state = %+v, want 20 slots and canonical types", state)
	}
	for i := 0; i < 10; i++ {
		for _, name := range []string{"test-sub", "test-canon"} {
			got, callErr := in.Invoke(name)
			if callErr != nil || len(got) != 0 {
				_ = in.Close()
				return fmt.Errorf("Tiny %s iteration %d = %v, %v", name, i, got, callErr)
			}
		}
	}
	if err := in.gc.CollectFull(nil); err != nil {
		_ = in.Close()
		return err
	}
	if live := in.gc.Stats().LiveObjects; live != 8 {
		_ = in.Close()
		return fmt.Errorf("Tiny live objects after full collection = %d, want 8 table roots", live)
	}
	if err := in.Close(); err != nil {
		return err
	}
	blob, err := marshalCompiled(c)
	if err != nil {
		return err
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		return err
	}
	defer loaded.Close()
	if loaded.stagedGCStructProduct() != 0 || loaded.usesGCStructHelpers() || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
		return fmt.Errorf("codec inherited concrete ref.test admission")
	}
	if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
		return fmt.Errorf("codec-loaded concrete instantiate = %v", err)
	}
	if _, err := Capture(c, SnapshotOptions{}); err == nil {
		return fmt.Errorf("snapshot admitted concrete ref.test product")
	}
	return nil
}

func TestStagedOfficialGCRefTestAccounting(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/ref_test", &script)
	counts, leaders, gateCounts := replayStagedGCRefTestScript(t, tmp, script)
	if counts.Commands != 73 || counts.ModulesPassed != 2 || counts.AssertionsPassed != 68 || counts.ExpectedFeatureRejects != 0 || counts.BlockedCommands != 0 || counts.ExpectedInvalid != 0 || counts.ExpectedMalformed != 0 || counts.Failures != 0 || counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 {
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
