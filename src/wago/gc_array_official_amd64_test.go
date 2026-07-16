//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

const stagedGCArrayDeltaPath = "tests/spec-v3-staged-gc-array.json"

type stagedGCArrayClass uint8

const (
	stagedGCArrayDeclarations stagedGCArrayClass = iota + 1
	stagedGCArrayBindings
	stagedGCArrayNumericDefault
	stagedGCArrayNumericFixed
	stagedGCArrayPackedData
	stagedGCArrayReferenceElements
	stagedGCArrayNullDereference
)

func (c stagedGCArrayClass) String() string {
	switch c {
	case stagedGCArrayDeclarations:
		return "declarations"
	case stagedGCArrayBindings:
		return "bindings"
	case stagedGCArrayNumericDefault:
		return "numeric-default"
	case stagedGCArrayNumericFixed:
		return "numeric-fixed"
	case stagedGCArrayPackedData:
		return "packed-data"
	case stagedGCArrayReferenceElements:
		return "reference-elements"
	case stagedGCArrayNullDereference:
		return "null-dereference"
	default:
		return "unknown"
	}
}

func (c stagedGCArrayClass) gateReason() string {
	switch c {
	case stagedGCArrayDeclarations:
		return "array declaration metadata product"
	case stagedGCArrayBindings:
		return "recursive array binding metadata product"
	case stagedGCArrayNumericDefault:
		return "numeric array.new/default/get/set/len globals and public results"
	case stagedGCArrayNumericFixed:
		return "numeric array.new_fixed/get/set/len global and public results"
	case stagedGCArrayPackedData:
		return "packed array.new_data/get/set/len and data-segment lifecycle"
	case stagedGCArrayReferenceElements:
		return "reference array.new_elem/get/set/len barriers and element lifecycle"
	case stagedGCArrayNullDereference:
		return "null array.get/array.set trap product"
	default:
		return "unknown gc/array product"
	}
}

type stagedGCArrayLeaderPin struct {
	Filename    string
	CommandLine int
	SourceLine  int
	Size        int
	SHA256      string
	Class       stagedGCArrayClass
	Actions     []string
}

var stagedGCArrayLeaderPins = []stagedGCArrayLeaderPin{
	{Filename: "array.0.wasm", CommandLine: 8, SourceLine: 3, Size: 80, SHA256: "995b6f4472185333316f224edf99518254df392aa1592239c2d9a0d81e2c052a", Class: stagedGCArrayDeclarations},
	{Filename: "array.2.wasm", CommandLine: 19, SourceLine: 37, Size: 55, SHA256: "a812822a7372385725cb75c70f0c3cfa7b9cca83a2bb8306a752adc44dc546bd", Class: stagedGCArrayBindings},
	{Filename: "array.5.wasm", CommandLine: 46, SourceLine: 60, Size: 250, SHA256: "dff18bcf6b1ed6fdb6ae63692baa8e649e22794de7f4dbf3bc76e0f2b0f28898", Class: stagedGCArrayNumericDefault, Actions: []string{"return:new", "return:new", "return:get", "return:set_get", "return:len", "trap:get", "trap:set_get"}},
	{Filename: "array.6.wasm", CommandLine: 79, SourceLine: 106, Size: 268, SHA256: "6ff5956b84b5035df8d3419edc8c67348cffd06d5a4cad86cfba56c415acbf25", Class: stagedGCArrayNumericFixed, Actions: []string{"return:new", "return:new", "return:get", "return:set_get", "return:len", "trap:get", "trap:set_get"}},
	{Filename: "array.7.wasm", CommandLine: 117, SourceLine: 151, Size: 351, SHA256: "7fc4afb6a2e3b2f6b1562b4d0185b6d5d4426c579bcda44cce3b3a1401247bce", Class: stagedGCArrayPackedData, Actions: []string{"return:new", "return:new", "return:get_u", "return:get_s", "return:set_get", "return:len", "trap:new-overflow", "trap:get_u", "trap:get_s", "trap:set_get", "return:drop_segs", "trap:new", "trap:new-overflow"}},
	{Filename: "array.8.wasm", CommandLine: 164, SourceLine: 219, Size: 396, SHA256: "19178a5db9c6ded41e185a9422c558a65d4bc1f11e7b0df11a776226f22812a9", Class: stagedGCArrayReferenceElements, Actions: []string{"return:new", "return:new", "return:get", "return:get", "return:set_get", "return:len", "trap:new-overflow", "trap:get", "trap:set_get", "return:drop_segs", "trap:new", "trap:new-overflow"}},
	{Filename: "array.12.wasm", CommandLine: 225, SourceLine: 332, Size: 115, SHA256: "b6446904a92663c6dc462e8c7f4b1a2077c7b942ce7be0fa053c32ecb990b96a", Class: stagedGCArrayNullDereference, Actions: []string{"trap:array.get-null", "trap:array.set-null"}},
}

var stagedGCArrayPinnedInvalidCodes = map[int]corewasm.ValidationErrorCode{
	9: corewasm.ErrUnknownType, 20: corewasm.ErrUnknownType, 24: corewasm.ErrUnknownType,
	186: corewasm.ErrTypeMismatch, 197: corewasm.ErrConstExprRequired, 206: corewasm.ErrConstExprRequired,
}

var stagedGCArrayInvalidSourceLines = map[int]int{9: 27, 20: 48, 24: 52, 186: 292, 197: 302, 206: 315}

type stagedGCArrayLeaderDelta struct {
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
	Actions     []string                    `json:"actions,omitempty"`
}

type stagedGCArrayDelta struct {
	Schema        int                             `json:"schema"`
	SuiteRevision string                          `json:"suite_revision"`
	File          string                          `json:"file"`
	Leaders       []stagedGCArrayLeaderDelta      `json:"leaders"`
	InvalidLines  map[int]int                     `json:"invalid_source_lines"`
	Gates         []stagedTypedReferenceGateCount `json:"gates"`
	Counts        stagedSpecCounts                `json:"counts"`
}

func stagedGCArrayLeaderPinFor(data []byte, line int) (stagedGCArrayLeaderPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedGCArrayLeaderPins {
		if pin.CommandLine == line && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedGCArrayLeaderPin{}, false
}

func stagedGCArrayLeaderDeltaFor(data []byte, line int) (stagedGCArrayLeaderDelta, stagedGCArrayLeaderPin, error) {
	pin, ok := stagedGCArrayLeaderPinFor(data, line)
	if !ok {
		return stagedGCArrayLeaderDelta{}, stagedGCArrayLeaderPin{}, fmt.Errorf("unknown gc/array binary at command line %d (size=%d)", line, len(data))
	}
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return stagedGCArrayLeaderDelta{}, stagedGCArrayLeaderPin{}, err
	}
	opcodes, err := stagedGCStructOpcodeInventory(m)
	if err != nil {
		return stagedGCArrayLeaderDelta{}, stagedGCArrayLeaderPin{}, err
	}
	return stagedGCArrayLeaderDelta{
		Filename: pin.Filename, CommandLine: pin.CommandLine, SourceLine: pin.SourceLine,
		Size: pin.Size, SHA256: pin.SHA256, Class: pin.Class.String(), Gate: pin.Class.gateReason(),
		TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m), Opcodes: opcodes,
		Actions: append([]string(nil), pin.Actions...),
	}, pin, nil
}

func compileStagedGCArray(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCArrayProducts = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func replayStagedGCArrayScript(t *testing.T, tmp string, script stagedSpecScript) (stagedSpecCounts, []stagedGCArrayLeaderDelta, map[string]int) {
	t.Helper()
	var counts stagedSpecCounts
	gates := map[string]int{}
	var latest []byte
	var current *stagedGCArrayLeaderPin
	var instance *Instance
	var currentCompiled *Compiled
	defer func() {
		if instance != nil {
			_ = instance.Close()
		}
	}()
	seenPins := map[string]bool{}
	seenActions := map[string][]string{}
	leaders := make([]stagedGCArrayLeaderDelta, 0, len(stagedGCArrayLeaderPins))
	invalidSeen := 0
	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("gc/array.wast:%d read definition: %v", cmd.Line, err)
				latest, current = nil, nil
				continue
			}
			latest, current = data, nil
		case "module_instance":
			if instance != nil {
				_ = instance.Close()
				instance = nil
			}
			currentCompiled = nil
			leader, pin, err := stagedGCArrayLeaderDeltaFor(latest, cmd.Line)
			if err != nil {
				counts.Failures++
				t.Error(err)
				continue
			}
			if seenPins[pin.Filename] {
				counts.Failures++
				t.Errorf("gc/array.wast:%d duplicate leader %s", cmd.Line, pin.Filename)
				continue
			}
			seenPins[pin.Filename] = true
			leaders = append(leaders, leader)
			current = &pin
			c, compileErr := compileStagedGCArray(latest)
			if compileErr != nil {
				counts.ExpectedFeatureRejects++
				gates[pin.Class.gateReason()]++
				continue
			}
			instance, err = instantiateCore(c, InstantiateOptions{})
			currentCompiled = c
			_ = c.Close()
			if err != nil {
				counts.Failures++
				counts.UnexpectedLinkRejects++
				t.Errorf("gc/array.wast:%d admitted leader %s instantiate: %v", cmd.Line, pin.Filename, err)
				instance = nil
				continue
			}
			if (pin.Class == stagedGCArrayDeclarations || pin.Class == stagedGCArrayBindings) && instance.gc != nil {
				counts.Failures++
				t.Errorf("gc/array.wast:%d metadata-only leader %s allocated a collector", cmd.Line, pin.Filename)
			}
			counts.ModulesPassed++
		case "assert_invalid":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("gc/array.wast:%d read invalid module: %v", cmd.Line, err)
				continue
			}
			m, decodeErr := corewasm.DecodeModule(data)
			if decodeErr != nil {
				counts.Failures++
				t.Errorf("gc/array.wast:%d invalid module decode: %v", cmd.Line, decodeErr)
				continue
			}
			validationErr := corewasm.ValidateModule(m)
			want, pinned := stagedGCArrayPinnedInvalidCodes[cmd.Line]
			var verr *corewasm.ValidationError
			if !pinned || !errors.As(validationErr, &verr) || verr.Code != want {
				counts.Failures++
				t.Errorf("gc/array.wast:%d validation error = %v, want exact %v", cmd.Line, validationErr, want)
				continue
			}
			if c, err := compileStagedGCArray(data); err == nil {
				_ = c.Close()
				counts.Failures++
				t.Errorf("gc/array.wast:%d invalid module compiled", cmd.Line)
				continue
			}
			invalidSeen++
			counts.ExpectedInvalid++
		case "assert_return", "assert_trap", "action":
			if current == nil {
				counts.Failures++
				t.Errorf("gc/array.wast:%d action has no classified leader", cmd.Line)
				continue
			}
			kind := "action"
			if cmd.Type == "assert_return" {
				kind = "return"
			} else if cmd.Type == "assert_trap" {
				kind = "trap"
			}
			seenActions[current.Filename] = append(seenActions[current.Filename], kind+":"+cmd.Action.Field)
			if instance == nil {
				counts.BlockedCommands++
				continue
			}
			module := stagedSpecModule{in: instance, c: currentCompiled}
			args := make([]uint64, len(cmd.Action.Args))
			valid := cmd.Action.Type == "invoke"
			for i, arg := range cmd.Action.Args {
				args[i], valid = stagedTypedReferenceArgument(module, nil, arg)
				if !valid {
					break
				}
			}
			if !valid {
				counts.Failures++
				t.Errorf("gc/array.wast:%d unsupported staged action", cmd.Line)
				continue
			}
			got, invokeErr := instance.Invoke(cmd.Action.Field, args...)
			if cmd.Type == "assert_trap" {
				if invokeErr == nil {
					counts.Failures++
					t.Errorf("gc/array.wast:%d %s returned normally, want trap", cmd.Line, cmd.Action.Field)
				} else {
					counts.AssertionsPassed++
				}
				continue
			}
			if invokeErr != nil || len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("gc/array.wast:%d result=%v err=%v want=%v", cmd.Line, got, invokeErr, cmd.Expected)
				continue
			}
			matched := true
			publicArrayResult := (current.Class == stagedGCArrayNumericDefault || current.Class == stagedGCArrayNumericFixed || current.Class == stagedGCArrayPackedData || current.Class == stagedGCArrayReferenceElements) && cmd.Action.Field == "new"
			for i := range got {
				if publicArrayResult {
					exact, owner, _, ok := instance.refStore.gcRefExactType(got[i])
					wantType := uint32(0)
					if current.Class == stagedGCArrayReferenceElements {
						wantType = 1
					}
					if !ok || got[i] == 0 || got[i]>>32 == 0 || owner != instance || exact.Kind != ValueTypeReference || !exact.Ref.Exact || !exact.Ref.Heap.Defined || exact.Ref.Heap.TypeIndex != wantType {
						matched = false
						break
					}
					continue
				}
				if !stagedTypedReferenceMatch(module, got[i], cmd.Expected[i]) {
					matched = false
					break
				}
			}
			if publicArrayResult && len(got) == 1 {
				if err := instance.ReleaseGCRef(ValueOf(ValAnyRef, got[0]).GCRef()); err != nil {
					counts.Failures++
					t.Errorf("gc/array.wast:%d release public GC result: %v", cmd.Line, err)
					continue
				}
			}
			if !matched {
				counts.Failures++
				t.Errorf("gc/array.wast:%d result=%v want=%v", cmd.Line, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		default:
			counts.Failures++
			t.Errorf("gc/array.wast:%d unhandled command %q", cmd.Line, cmd.Type)
		}
	}
	if invalidSeen != len(stagedGCArrayPinnedInvalidCodes) {
		counts.Failures++
		t.Errorf("gc/array invalid coverage = %d, want %d", invalidSeen, len(stagedGCArrayPinnedInvalidCodes))
	}
	if len(seenPins) != len(stagedGCArrayLeaderPins) {
		counts.Failures++
		t.Errorf("gc/array leader coverage = %d, want %d", len(seenPins), len(stagedGCArrayLeaderPins))
	}
	for _, pin := range stagedGCArrayLeaderPins {
		if !reflect.DeepEqual(seenActions[pin.Filename], pin.Actions) {
			counts.Failures++
			t.Errorf("%s action inventory = %v, want %v", pin.Filename, seenActions[pin.Filename], pin.Actions)
		}
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i].CommandLine < leaders[j].CommandLine })
	return counts, leaders, gates
}

func TestStagedOfficialGCArrayAccounting(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/array", &script)
	counts, leaders, gateCounts := replayStagedGCArrayScript(t, tmp, script)
	if counts.Commands != 61 || counts.ModulesPassed != 7 || counts.AssertionsPassed != 41 || counts.ExpectedFeatureRejects != 0 || counts.BlockedCommands != 0 || counts.ExpectedInvalid != 6 || counts.Failures != 0 || counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 {
		t.Fatalf("staged gc/array accounting has hidden or changed gaps: %+v", counts)
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
	delta := stagedGCArrayDelta{Schema: 2, SuiteRevision: stagedRelease3Revision, File: "gc/array", Leaders: leaders, InvalidLines: stagedGCArrayInvalidSourceLines, Gates: gates, Counts: counts}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedGCArrayDeltaPath)
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
		t.Fatalf("read %s: %v", stagedGCArrayDeltaPath, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("staged gc/array accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact leaders and gates\n%s", got)
	}
}
