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
	"strconv"
	"strings"
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

const stagedGCArrayInitDeltaPath = "tests/spec-v3-staged-gc-array-init.json"

var stagedGCArrayInitOfficialExecution = map[string]bool{
	"gc/array_init_data": false,
	"gc/array_init_elem": false,
}

type stagedGCArrayInitLeaderPin struct {
	Base         string
	Filename     string
	CommandLine  int
	SourceLine   int
	Size         int
	SHA256       string
	Class        string
	ControlGraph string
	Actions      []string
}

var stagedGCArrayInitLeaderPins = []stagedGCArrayInitLeaderPin{
	{
		Base: "gc/array_init_data", Filename: "array_init_data.2.wasm", CommandLine: 48, SourceLine: 31, Size: 335,
		SHA256: "c17da56ed5c65083ee20023738cc5d9a36d1e301d2f06f23e2645d6ec8a9ca77", Class: "packed-i8-i16-data-init",
		ControlGraph: "preflight null, destination element range, and source byte range before mutation; i8 consumes one byte per element, i16 consumes two little-endian bytes; drop preserves zero-length success",
		Actions: []string{
			"trap:array_init_data-null()", "trap:array_init_data(13,0,0)", "trap:array_init_data(0,13,0)",
			"trap:array_init_data(0,0,13)", "trap:array_init_data(0,0,13)", "trap:array_init_data_i16(0,0,7)",
			"return:array_init_data(12,0,0)->()", "return:array_init_data(0,12,0)->()", "return:array_init_data_i16(0,6,0)->()",
			"return:array_get_nth(0)->(0)", "return:array_get_nth(5)->(0)", "return:array_get_nth(11)->(0)", "trap:array_get_nth(12)",
			"return:array_get_nth_i16(0)->(0)", "return:array_get_nth_i16(2)->(0)", "return:array_get_nth_i16(5)->(0)", "trap:array_get_nth_i16(6)",
			"return:array_init_data(4,2,2)->()", "return:array_get_nth(3)->(0)", "return:array_get_nth(4)->(99)",
			"return:array_get_nth(5)->(100)", "return:array_get_nth(6)->(0)", "return:array_init_data_i16(2,5,2)->()",
			"return:array_get_nth_i16(1)->(0)", "return:array_get_nth_i16(2)->(26470)", "return:array_get_nth_i16(3)->(26984)",
			"return:array_get_nth_i16(4)->(0)", "return:drop_segs()->()", "return:array_init_data(0,0,0)->()", "trap:array_init_data(0,0,1)",
		},
	},
	{
		Base: "gc/array_init_data", Filename: "array_init_data.3.wasm", CommandLine: 145, SourceLine: 113, Size: 435,
		SHA256: "05827a01cec2e9f3623e9d00b54aff258bbc7b497f47b76ffd31452bbcb9b31f", Class: "i32-i64-data-width",
		ControlGraph: "array.init_data source bounds are measured in bytes: one i32 element requires four bytes and one i64 element requires eight bytes before any destination write",
		Actions: []string{
			"trap:f0()", "trap:f1()", "trap:f2()", "trap:f3()", "return:f4()->()", "trap:f9()",
			"trap:g0()", "trap:g1()", "trap:g4()", "trap:g7()", "return:g8()->()", "trap:g9()",
		},
	},
	{
		Base: "gc/array_init_elem", Filename: "array_init_elem.3.wasm", CommandLine: 56, SourceLine: 44, Size: 268,
		SHA256: "77153cc9166a1b88e564a93e473e2d4d31979288ac4b82b9b0038911cd15983b", Class: "funcref-element-init",
		ControlGraph: "preflight null, destination and passive-element ranges, and every selected reference subtype before writes; publish object/card/post-bulk barriers as applicable; drop preserves zero-length success",
		Actions: []string{
			"trap:array_init_elem-null()", "trap:array_init_elem(13,0,0)", "trap:array_init_elem(0,13,0)",
			"trap:array_init_elem(0,0,13)", "trap:array_init_elem(0,0,13)", "return:array_init_elem(12,0,0)->()",
			"return:array_init_elem(0,12,0)->()", "trap:array_call_nth(0)", "trap:array_call_nth(5)",
			"trap:array_call_nth(11)", "trap:array_call_nth(12)", "return:array_init_elem(2,3,2)->()",
			"trap:array_call_nth(1)", "return:array_call_nth(2)->()", "return:array_call_nth(3)->()", "trap:array_call_nth(4)",
			"return:drop_segs()->()", "return:array_init_elem(0,0,0)->()", "trap:array_init_elem(0,0,1)",
		},
	},
}

type stagedGCArrayInitInvalidPin struct {
	Base        string `json:"-"`
	Filename    string `json:"filename"`
	CommandLine int    `json:"command_line"`
	SourceLine  int    `json:"source_line"`
	Size        int    `json:"size"`
	SHA256      string `json:"sha256"`
	Error       string `json:"error"`
}

var stagedGCArrayInitInvalidPins = []stagedGCArrayInitInvalidPin{
	{Base: "gc/array_init_data", Filename: "array_init_data.0.wasm", CommandLine: 1, SourceLine: 5, Size: 109, SHA256: "6a2af6c7db355d780d22795fae3dce2d841e4cccfa4b32ff320b829c60135dfe", Error: "array is immutable"},
	{Base: "gc/array_init_data", Filename: "array_init_data.1.wasm", CommandLine: 13, SourceLine: 18, Size: 109, SHA256: "2a365d88470d3839add00990c53cc7d497ed521602e1fa91afcd4905d262fdd3", Error: "array type is not numeric or vector"},
	{Base: "gc/array_init_elem", Filename: "array_init_elem.0.wasm", CommandLine: 1, SourceLine: 5, Size: 102, SHA256: "19a97cf39d38738e3ed003778bb2fefc11b311225b4e1d1ee9ecc8487f53fe3f", Error: "array is immutable"},
	{Base: "gc/array_init_elem", Filename: "array_init_elem.1.wasm", CommandLine: 13, SourceLine: 18, Size: 102, SHA256: "6dd5ba5468d0d7ea9593a96150f957c65ff4c048716a20756bed1ae8212c23eb", Error: "type mismatch"},
	{Base: "gc/array_init_elem", Filename: "array_init_elem.2.wasm", CommandLine: 25, SourceLine: 31, Size: 102, SHA256: "2990ab38c59da1014e4260c9699f4ba5d8a2c368baf175740aabe3fa60f847e8", Error: "type mismatch"},
}

type stagedGCArrayInitLeaderDelta struct {
	Filename     string                      `json:"filename"`
	CommandLine  int                         `json:"command_line"`
	SourceLine   int                         `json:"source_line"`
	Size         int                         `json:"size"`
	SHA256       string                      `json:"sha256"`
	Class        string                      `json:"class"`
	Gate         string                      `json:"gate,omitempty"`
	TypeGraph    string                      `json:"type_graph"`
	StateGraph   string                      `json:"state_graph"`
	ControlGraph string                      `json:"control_graph"`
	Opcodes      []stagedGCStructOpcodeCount `json:"opcodes,omitempty"`
	Actions      []string                    `json:"actions"`
}

type stagedGCArrayInitFileDelta struct {
	Name     string                          `json:"name"`
	Leaders  []stagedGCArrayInitLeaderDelta  `json:"leaders"`
	Invalids []stagedGCArrayInitInvalidPin   `json:"invalids"`
	Gates    []stagedTypedReferenceGateCount `json:"gates"`
	Counts   stagedSpecCounts                `json:"counts"`
}

type stagedGCArrayInitDelta struct {
	Schema        int                          `json:"schema"`
	SuiteRevision string                       `json:"suite_revision"`
	Files         []stagedGCArrayInitFileDelta `json:"files"`
	Totals        stagedSpecCounts             `json:"totals"`
}

func stagedGCArrayInitLeaderPinFor(base string, data []byte, line int) (stagedGCArrayInitLeaderPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedGCArrayInitLeaderPins {
		if pin.Base == base && pin.CommandLine == line && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedGCArrayInitLeaderPin{}, false
}

func stagedGCArrayInitInvalidPinFor(base string, data []byte, line int) (stagedGCArrayInitInvalidPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedGCArrayInitInvalidPins {
		if pin.Base == base && pin.CommandLine == line && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedGCArrayInitInvalidPin{}, false
}

func stagedGCArrayInitLeaderDeltaFor(base string, data []byte, line int) (stagedGCArrayInitLeaderDelta, stagedGCArrayInitLeaderPin, error) {
	pin, ok := stagedGCArrayInitLeaderPinFor(base, data, line)
	if !ok {
		return stagedGCArrayInitLeaderDelta{}, stagedGCArrayInitLeaderPin{}, fmt.Errorf("unknown %s binary at command line %d (size=%d)", base, line, len(data))
	}
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return stagedGCArrayInitLeaderDelta{}, stagedGCArrayInitLeaderPin{}, err
	}
	opcodes, err := stagedGCStructOpcodeInventory(m)
	if err != nil {
		return stagedGCArrayInitLeaderDelta{}, stagedGCArrayInitLeaderPin{}, err
	}
	gate := "array init segment/root helper product"
	if stagedGCArrayInitOfficialExecution[base] {
		gate = ""
	}
	return stagedGCArrayInitLeaderDelta{
		Filename: pin.Filename, CommandLine: pin.CommandLine, SourceLine: pin.SourceLine, Size: pin.Size, SHA256: pin.SHA256,
		Class: pin.Class, Gate: gate, TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m),
		ControlGraph: pin.ControlGraph, Opcodes: opcodes, Actions: append([]string(nil), pin.Actions...),
	}, pin, nil
}

func stagedGCArrayInitInvalids(base string) []stagedGCArrayInitInvalidPin {
	var out []stagedGCArrayInitInvalidPin
	for _, pin := range stagedGCArrayInitInvalidPins {
		if pin.Base == base {
			out = append(out, pin)
		}
	}
	return out
}

func stagedGCArrayInitActionKey(cmd stagedSpecCommand) string {
	kind := "return"
	if cmd.Type == "assert_trap" {
		kind = "trap"
	}
	args := make([]string, len(cmd.Action.Args))
	for i, arg := range cmd.Action.Args {
		bits, ok := stagedSpecScalar(arg)
		if !ok {
			return "invalid"
		}
		args[i] = strconv.FormatUint(uint64(uint32(bits)), 10)
	}
	key := kind + ":" + cmd.Action.Field + "(" + strings.Join(args, ",") + ")"
	if kind == "trap" {
		return key
	}
	results := make([]string, len(cmd.Expected))
	for i, result := range cmd.Expected {
		bits, ok := stagedSpecScalar(result)
		if !ok {
			return "invalid"
		}
		results[i] = strconv.FormatUint(uint64(uint32(bits)), 10)
	}
	return key + "->(" + strings.Join(results, ",") + ")"
}

func compileStagedGCArrayInit(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCArrayProducts = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func replayStagedGCArrayInitScript(t *testing.T, base, tmp string, script stagedSpecScript) (stagedSpecCounts, []stagedGCArrayInitLeaderDelta, map[string]int) {
	t.Helper()
	var counts stagedSpecCounts
	gates := map[string]int{}
	var latest []byte
	var current *stagedGCArrayInitLeaderPin
	var instance *Instance
	seenLeaders := map[string]bool{}
	seenInvalids := map[string]bool{}
	seenActions := map[string][]string{}
	var leaders []stagedGCArrayInitLeaderDelta
	defer func() {
		if instance != nil {
			_ = instance.Close()
		}
	}()

	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read definition: %v", base, cmd.Line, err)
				continue
			}
			latest, current = data, nil
		case "module_instance":
			leader, pin, err := stagedGCArrayInitLeaderDeltaFor(base, latest, cmd.Line)
			if err != nil {
				counts.Failures++
				t.Error(err)
				continue
			}
			seenLeaders[pin.Filename] = true
			leaders = append(leaders, leader)
			current = &pin
			if instance != nil {
				_ = instance.Close()
				instance = nil
			}
			if !stagedGCArrayInitOfficialExecution[base] {
				counts.ExpectedFeatureRejects++
				gates[leader.Gate]++
				continue
			}
			c, err := compileStagedGCArrayInit(latest)
			if err != nil {
				counts.Failures++
				counts.UnexpectedCompileRejects++
				t.Errorf("%s.wast:%d compile: %v", base, cmd.Line, err)
				continue
			}
			instance, err = instantiateCore(c, InstantiateOptions{})
			_ = c.Close()
			if err != nil {
				counts.Failures++
				counts.UnexpectedLinkRejects++
				t.Errorf("%s.wast:%d instantiate: %v", base, cmd.Line, err)
				continue
			}
			counts.ModulesPassed++
		case "assert_invalid":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read invalid: %v", base, cmd.Line, err)
				continue
			}
			pin, ok := stagedGCArrayInitInvalidPinFor(base, data, cmd.Line)
			if !ok {
				counts.Failures++
				t.Errorf("%s.wast:%d unknown invalid binary size=%d", base, cmd.Line, len(data))
				continue
			}
			m, decodeErr := corewasm.DecodeModule(data)
			if decodeErr != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d invalid decode: %v", base, cmd.Line, decodeErr)
				continue
			}
			validationErr := corewasm.ValidateModule(m)
			var verr *corewasm.ValidationError
			if !errors.As(validationErr, &verr) || verr.Code != corewasm.ErrTypeMismatch {
				counts.Failures++
				t.Errorf("%s.wast:%d validation=%v, want %s", base, cmd.Line, validationErr, pin.Error)
				continue
			}
			seenInvalids[pin.Filename] = true
			counts.ExpectedInvalid++
		case "assert_return", "assert_trap":
			if current == nil {
				counts.Failures++
				t.Errorf("%s.wast:%d action has no leader", base, cmd.Line)
				continue
			}
			key := stagedGCArrayInitActionKey(cmd)
			seenActions[current.Filename] = append(seenActions[current.Filename], key)
			if instance == nil {
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
				t.Errorf("%s.wast:%d unsupported arguments", base, cmd.Line)
				continue
			}
			got, invokeErr := instance.Invoke(cmd.Action.Field, args...)
			if cmd.Type == "assert_trap" {
				if invokeErr == nil {
					counts.Failures++
					t.Errorf("%s.wast:%d %s returned %v, want trap", base, cmd.Line, cmd.Action.Field, got)
				} else {
					counts.AssertionsPassed++
				}
				continue
			}
			if invokeErr != nil || len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("%s.wast:%d %s=%v,%v want %v", base, cmd.Line, cmd.Action.Field, got, invokeErr, cmd.Expected)
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
				t.Errorf("%s.wast:%d %s=%v want %v", base, cmd.Line, cmd.Action.Field, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		default:
			counts.Failures++
			t.Errorf("%s.wast:%d unhandled command %q", base, cmd.Line, cmd.Type)
		}
	}
	wantLeaders := 1
	if base == "gc/array_init_data" {
		wantLeaders = 2
	}
	if len(seenLeaders) != wantLeaders {
		counts.Failures++
		t.Errorf("%s leader coverage=%d, want %d", base, len(seenLeaders), wantLeaders)
	}
	if len(seenInvalids) != len(stagedGCArrayInitInvalids(base)) {
		counts.Failures++
		t.Errorf("%s invalid coverage=%d, want %d", base, len(seenInvalids), len(stagedGCArrayInitInvalids(base)))
	}
	for _, pin := range stagedGCArrayInitLeaderPins {
		if pin.Base == base && !reflect.DeepEqual(seenActions[pin.Filename], pin.Actions) {
			counts.Failures++
			t.Errorf("%s actions=%v, want %v", pin.Filename, seenActions[pin.Filename], pin.Actions)
		}
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i].CommandLine < leaders[j].CommandLine })
	return counts, leaders, gates
}

func TestStagedOfficialGCArrayInitAccounting(t *testing.T) {
	bases := []string{"gc/array_init_data", "gc/array_init_elem"}
	delta := stagedGCArrayInitDelta{Schema: 2, SuiteRevision: stagedRelease3Revision}
	for _, base := range bases {
		var script stagedSpecScript
		tmp := stagedOfficialTypedReferenceJSON(t, base, &script)
		counts, leaders, gateCounts := replayStagedGCArrayInitScript(t, base, tmp, script)
		wantCommands, wantInvalid, wantLeaders, wantBlocked := 48, 2, 2, 42
		if base == "gc/array_init_elem" {
			wantCommands, wantInvalid, wantLeaders, wantBlocked = 24, 3, 1, 19
		}
		wantModules, wantAssertions, wantGates := 0, 0, wantLeaders
		if stagedGCArrayInitOfficialExecution[base] {
			wantModules, wantAssertions, wantGates, wantBlocked = wantLeaders, wantBlocked, 0, 0
		}
		if counts.Commands != wantCommands || counts.ModulesPassed != wantModules || counts.AssertionsPassed != wantAssertions || counts.ExpectedInvalid != wantInvalid || counts.ExpectedFeatureRejects != wantGates || counts.BlockedCommands != wantBlocked || counts.ExpectedMalformed != 0 || counts.Failures != 0 || counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 {
			t.Fatalf("staged %s accounting has hidden or changed gaps: %+v", base, counts)
		}
		var gates []stagedTypedReferenceGateCount
		for reason, count := range gateCounts {
			gates = append(gates, stagedTypedReferenceGateCount{Family: "gc", Reason: reason, Count: count})
		}
		sort.Slice(gates, func(i, j int) bool { return gates[i].Reason < gates[j].Reason })
		delta.Files = append(delta.Files, stagedGCArrayInitFileDelta{Name: base, Leaders: leaders, Invalids: stagedGCArrayInitInvalids(base), Gates: gates, Counts: counts})
		delta.Totals.add(counts)
	}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedGCArrayInitDeltaPath)
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
		t.Fatalf("read %s: %v", stagedGCArrayInitDeltaPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("staged array-init accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact leaders/invalids/actions\n%s", got)
	}
}

func TestStagedGCArrayInitPinsRejectUnknowns(t *testing.T) {
	if _, ok := stagedGCArrayInitLeaderPinFor("gc/array_init_data", []byte("not wasm"), 48); ok {
		t.Fatal("unknown array init leader matched")
	}
	if _, ok := stagedGCArrayInitInvalidPinFor("gc/array_init_elem", []byte("not wasm"), 25); ok {
		t.Fatal("unknown array init invalid matched")
	}
}
