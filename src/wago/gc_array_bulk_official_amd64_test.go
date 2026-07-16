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

const (
	stagedGCArrayBulkDeltaPath         = "tests/spec-v3-staged-gc-array-bulk.json"
	stagedGCArrayBulkOfficialExecution = false
)

type stagedGCArrayBulkLeaderPin struct {
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

var stagedGCArrayBulkLeaderPins = []stagedGCArrayBulkLeaderPin{
	{
		Base: "gc/array_fill", Filename: "array_fill.3.wasm", CommandLine: 48, SourceLine: 38, Size: 183,
		SHA256: "0893caa7ae7ab2d870329da9697d405a51592cb3ecc1b4b833780ef9b2580169", Class: "packed-i8-fill",
		ControlGraph: "preflight null and dst+len before mutation; zero length at dst=12 succeeds; successful fill truncates i32 to i8",
		Actions: []string{
			"trap:array_fill-null()", "trap:array_fill(13,0,0)", "trap:array_fill(0,0,13)",
			"return:array_fill(12,0,0)->()", "return:array_get_nth(0)->(0)", "return:array_get_nth(5)->(0)",
			"return:array_get_nth(11)->(0)", "trap:array_get_nth(12)", "return:array_fill(2,11,2)->()",
			"return:array_get_nth(1)->(0)", "return:array_get_nth(2)->(11)", "return:array_get_nth(3)->(11)",
			"return:array_get_nth(4)->(0)",
		},
	},
	{
		Base: "gc/array_copy", Filename: "array_copy.4.wasm", CommandLine: 76, SourceLine: 54, Size: 402,
		SHA256: "3ce0c22105571618832b6d97164a26e4b7dee035f540957422b887c4c04d4f35", Class: "packed-i8-copy",
		ControlGraph: "preflight both nulls and both ranges before mutation; zero length accepts either end; same-array copies use memmove ordering in both overlap directions",
		Actions: []string{
			"trap:array_copy-null-left()", "trap:array_copy-null-right()", "trap:array_copy(13,0,0)",
			"trap:array_copy(0,13,0)", "trap:array_copy(0,0,13)", "trap:array_copy(0,0,13)",
			"return:array_copy(12,0,0)->()", "return:array_copy(0,12,0)->()", "return:array_get_nth(0)->(0)",
			"return:array_get_nth(5)->(0)", "return:array_get_nth(11)->(0)", "trap:array_get_nth(12)",
			"return:array_copy(0,0,2)->()", "return:array_get_nth(0)->(10)", "return:array_get_nth(1)->(10)",
			"return:array_get_nth(2)->(0)", "return:array_copy_overlap_test-1()->()", "return:array_get_nth(0)->(97)",
			"return:array_get_nth(1)->(97)", "return:array_get_nth(2)->(98)", "return:array_get_nth(5)->(101)",
			"return:array_get_nth(10)->(106)", "return:array_get_nth(11)->(107)", "return:array_copy_overlap_test-2()->()",
			"return:array_get_nth(0)->(98)", "return:array_get_nth(1)->(99)", "return:array_get_nth(5)->(103)",
			"return:array_get_nth(9)->(107)", "return:array_get_nth(10)->(108)", "return:array_get_nth(11)->(108)",
		},
	},
}

type stagedGCArrayBulkInvalidPin struct {
	Base        string `json:"-"`
	Filename    string `json:"filename"`
	CommandLine int    `json:"command_line"`
	SourceLine  int    `json:"source_line"`
	Size        int    `json:"size"`
	SHA256      string `json:"sha256"`
	Error       string `json:"error"`
}

var stagedGCArrayBulkInvalidPins = []stagedGCArrayBulkInvalidPin{
	{Base: "gc/array_fill", Filename: "array_fill.0.wasm", CommandLine: 1, SourceLine: 5, Size: 87, SHA256: "883e06140ac851a975af3b2e2bfd3bc756001a1fa829a8ca2ca1fe6e4fb63515", Error: "array is immutable"},
	{Base: "gc/array_fill", Filename: "array_fill.1.wasm", CommandLine: 12, SourceLine: 16, Size: 87, SHA256: "eea1a50d6ac59d524d141bb380aa04ffa10eb65f55e133a6fab505a92d2129d7", Error: "type mismatch"},
	{Base: "gc/array_fill", Filename: "array_fill.2.wasm", CommandLine: 23, SourceLine: 27, Size: 87, SHA256: "e218ec3b33cc8e8673b2e00d351de1369ab1e6181269d93c84edb63a7bbc0ab2", Error: "type mismatch"},
	{Base: "gc/array_copy", Filename: "array_copy.0.wasm", CommandLine: 1, SourceLine: 5, Size: 94, SHA256: "294fb4f7458399853d96ac6201b3a7997f97ba7a8bebd3bcc52eb4ab32bfae35", Error: "array is immutable"},
	{Base: "gc/array_copy", Filename: "array_copy.1.wasm", CommandLine: 12, SourceLine: 17, Size: 99, SHA256: "03323c7ef08dd425b5c85e37eda0c9d8f35a37f34fc34e26a673e4d7b757faa2", Error: "array types do not match"},
	{Base: "gc/array_copy", Filename: "array_copy.2.wasm", CommandLine: 24, SourceLine: 29, Size: 99, SHA256: "b40d7390d19c24b71d17d4994ad27b725869fba12f74b6e3bddfd8ee8ecc0e9e", Error: "array types do not match"},
	{Base: "gc/array_copy", Filename: "array_copy.3.wasm", CommandLine: 36, SourceLine: 41, Size: 103, SHA256: "ab52af2b8f79bce4f327080bc022b0904daf551cc814ce84d226d6bc8a184753", Error: "array types do not match"},
}

type stagedGCArrayBulkLeaderDelta struct {
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

type stagedGCArrayBulkFileDelta struct {
	Name     string                          `json:"name"`
	Leaders  []stagedGCArrayBulkLeaderDelta  `json:"leaders"`
	Invalids []stagedGCArrayBulkInvalidPin   `json:"invalids"`
	Gates    []stagedTypedReferenceGateCount `json:"gates"`
	Counts   stagedSpecCounts                `json:"counts"`
}

type stagedGCArrayBulkDelta struct {
	Schema        int                          `json:"schema"`
	SuiteRevision string                       `json:"suite_revision"`
	Files         []stagedGCArrayBulkFileDelta `json:"files"`
	Totals        stagedSpecCounts             `json:"totals"`
}

func stagedGCArrayBulkLeaderPinFor(base string, data []byte, line int) (stagedGCArrayBulkLeaderPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedGCArrayBulkLeaderPins {
		if pin.Base == base && pin.CommandLine == line && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedGCArrayBulkLeaderPin{}, false
}

func stagedGCArrayBulkInvalidPinFor(base string, data []byte, line int) (stagedGCArrayBulkInvalidPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedGCArrayBulkInvalidPins {
		if pin.Base == base && pin.CommandLine == line && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedGCArrayBulkInvalidPin{}, false
}

func stagedGCArrayBulkLeaderDeltaFor(base string, data []byte, line int) (stagedGCArrayBulkLeaderDelta, stagedGCArrayBulkLeaderPin, error) {
	pin, ok := stagedGCArrayBulkLeaderPinFor(base, data, line)
	if !ok {
		return stagedGCArrayBulkLeaderDelta{}, stagedGCArrayBulkLeaderPin{}, fmt.Errorf("unknown %s binary at command line %d (size=%d)", base, line, len(data))
	}
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return stagedGCArrayBulkLeaderDelta{}, stagedGCArrayBulkLeaderPin{}, err
	}
	opcodes, err := stagedGCStructOpcodeInventory(m)
	if err != nil {
		return stagedGCArrayBulkLeaderDelta{}, stagedGCArrayBulkLeaderPin{}, err
	}
	gate := "bulk array fill/copy helper product"
	if stagedGCArrayBulkOfficialExecution {
		gate = ""
	}
	return stagedGCArrayBulkLeaderDelta{
		Filename: pin.Filename, CommandLine: pin.CommandLine, SourceLine: pin.SourceLine, Size: pin.Size, SHA256: pin.SHA256,
		Class: pin.Class, Gate: gate, TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m),
		ControlGraph: pin.ControlGraph, Opcodes: opcodes, Actions: append([]string(nil), pin.Actions...),
	}, pin, nil
}

func stagedGCArrayBulkInvalids(base string) []stagedGCArrayBulkInvalidPin {
	var out []stagedGCArrayBulkInvalidPin
	for _, pin := range stagedGCArrayBulkInvalidPins {
		if pin.Base == base {
			out = append(out, pin)
		}
	}
	return out
}

func stagedGCArrayBulkValue(v stagedSpecValue) (string, bool) {
	bits, ok := stagedSpecScalar(v)
	if !ok {
		return "", false
	}
	return strconv.FormatUint(uint64(uint32(bits)), 10), true
}

func stagedGCArrayBulkActionKey(cmd stagedSpecCommand) string {
	kind := "return"
	if cmd.Type == "assert_trap" {
		kind = "trap"
	}
	args := make([]string, len(cmd.Action.Args))
	for i, arg := range cmd.Action.Args {
		value, ok := stagedGCArrayBulkValue(arg)
		if !ok {
			return "invalid"
		}
		args[i] = value
	}
	key := kind + ":" + cmd.Action.Field + "(" + strings.Join(args, ",") + ")"
	if kind == "trap" {
		return key
	}
	results := make([]string, len(cmd.Expected))
	for i, result := range cmd.Expected {
		value, ok := stagedGCArrayBulkValue(result)
		if !ok {
			return "invalid"
		}
		results[i] = value
	}
	return key + "->(" + strings.Join(results, ",") + ")"
}

func compileStagedGCArrayBulk(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCArrayProducts = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func replayStagedGCArrayBulkScript(t *testing.T, base, tmp string, script stagedSpecScript) (stagedSpecCounts, []stagedGCArrayBulkLeaderDelta, map[string]int) {
	t.Helper()
	var counts stagedSpecCounts
	gates := map[string]int{}
	var latest []byte
	var current *stagedGCArrayBulkLeaderPin
	var instance *Instance
	seenLeaders := map[string]bool{}
	seenInvalids := map[string]bool{}
	seenActions := map[string][]string{}
	var leaders []stagedGCArrayBulkLeaderDelta
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
			leader, pin, err := stagedGCArrayBulkLeaderDeltaFor(base, latest, cmd.Line)
			if err != nil {
				counts.Failures++
				t.Error(err)
				continue
			}
			seenLeaders[pin.Filename] = true
			leaders = append(leaders, leader)
			current = &pin
			if !stagedGCArrayBulkOfficialExecution {
				counts.ExpectedFeatureRejects++
				gates[leader.Gate]++
				continue
			}
			c, err := compileStagedGCArrayBulk(latest)
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
			pin, ok := stagedGCArrayBulkInvalidPinFor(base, data, cmd.Line)
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
			key := stagedGCArrayBulkActionKey(cmd)
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
	if len(seenLeaders) != 1 {
		counts.Failures++
		t.Errorf("%s leader coverage=%d, want 1", base, len(seenLeaders))
	}
	if len(seenInvalids) != len(stagedGCArrayBulkInvalids(base)) {
		counts.Failures++
		t.Errorf("%s invalid coverage=%d, want %d", base, len(seenInvalids), len(stagedGCArrayBulkInvalids(base)))
	}
	for _, pin := range stagedGCArrayBulkLeaderPins {
		if pin.Base == base && !reflect.DeepEqual(seenActions[pin.Filename], pin.Actions) {
			counts.Failures++
			t.Errorf("%s actions=%v, want %v", pin.Filename, seenActions[pin.Filename], pin.Actions)
		}
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i].CommandLine < leaders[j].CommandLine })
	return counts, leaders, gates
}

func TestStagedOfficialGCArrayBulkAccounting(t *testing.T) {
	bases := []string{"gc/array_fill", "gc/array_copy"}
	delta := stagedGCArrayBulkDelta{Schema: 2, SuiteRevision: stagedRelease3Revision}
	for _, base := range bases {
		var script stagedSpecScript
		tmp := stagedOfficialTypedReferenceJSON(t, base, &script)
		counts, leaders, gateCounts := replayStagedGCArrayBulkScript(t, base, tmp, script)
		wantCommands, wantInvalid, wantBlocked := 18, 3, 13
		if base == "gc/array_copy" {
			wantCommands, wantInvalid, wantBlocked = 36, 4, 30
		}
		wantModules, wantAssertions, wantGates := 0, 0, 1
		if stagedGCArrayBulkOfficialExecution {
			wantModules, wantAssertions, wantGates, wantBlocked = 1, wantBlocked, 0, 0
		}
		if counts.Commands != wantCommands || counts.ModulesPassed != wantModules || counts.AssertionsPassed != wantAssertions || counts.ExpectedInvalid != wantInvalid || counts.ExpectedFeatureRejects != wantGates || counts.BlockedCommands != wantBlocked || counts.ExpectedMalformed != 0 || counts.Failures != 0 || counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 {
			t.Fatalf("staged %s accounting has hidden or changed gaps: %+v", base, counts)
		}
		var gates []stagedTypedReferenceGateCount
		for reason, count := range gateCounts {
			gates = append(gates, stagedTypedReferenceGateCount{Family: "gc", Reason: reason, Count: count})
		}
		sort.Slice(gates, func(i, j int) bool { return gates[i].Reason < gates[j].Reason })
		delta.Files = append(delta.Files, stagedGCArrayBulkFileDelta{Name: base, Leaders: leaders, Invalids: stagedGCArrayBulkInvalids(base), Gates: gates, Counts: counts})
		delta.Totals.add(counts)
	}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedGCArrayBulkDeltaPath)
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
		t.Fatalf("read %s: %v", stagedGCArrayBulkDeltaPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("staged bulk-array accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact leaders/invalids/actions\n%s", got)
	}
}

func TestStagedGCArrayBulkPinsRejectUnknowns(t *testing.T) {
	if _, ok := stagedGCArrayBulkLeaderPinFor("gc/array_fill", []byte("not wasm"), 48); ok {
		t.Fatal("unknown bulk array leader matched")
	}
	if _, ok := stagedGCArrayBulkInvalidPinFor("gc/array_copy", []byte("not wasm"), 36); ok {
		t.Fatal("unknown bulk array invalid matched")
	}
}
