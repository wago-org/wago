//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/json"
	"errors"
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

const stagedGCStructDeltaPath = "tests/spec-v3-staged-gc-struct.json"

var stagedGCStructSourceOnlyMalformed = []int{15}

var stagedGCStructPinnedInvalidCodes = map[int]corewasm.ValidationErrorCode{
	18: corewasm.ErrUnknownType,
	25: corewasm.ErrUnknownType,
	42: corewasm.ErrTypeMismatch,
	85: corewasm.ErrTypeMismatch,
}

type stagedGCStructLeaderDelta struct {
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

type stagedGCStructDelta struct {
	Schema              int                             `json:"schema"`
	SuiteRevision       string                          `json:"suite_revision"`
	File                string                          `json:"file"`
	SourceOnlyMalformed []int                           `json:"source_only_malformed"`
	Leaders             []stagedGCStructLeaderDelta     `json:"leaders"`
	Gates               []stagedTypedReferenceGateCount `json:"gates"`
	Counts              stagedSpecCounts                `json:"counts"`
}

func compileStagedGCStruct(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCStructProducts = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func stagedGCStructLeaderDeltaFor(data []byte, line int) (stagedGCStructLeaderDelta, stagedGCStructLeaderPin, error) {
	pin, ok := stagedGCStructLeaderPinFor(data, line)
	if !ok {
		return stagedGCStructLeaderDelta{}, stagedGCStructLeaderPin{}, fmt.Errorf("unknown gc/struct binary at command line %d (size=%d)", line, len(data))
	}
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return stagedGCStructLeaderDelta{}, stagedGCStructLeaderPin{}, fmt.Errorf("decode %s: %w", pin.Filename, err)
	}
	// The basic and packed leaders intentionally reach the current validator's
	// missing GC constant-expression admission. Their official validity is pinned
	// by the revision-stamped text oracle and exact binary identity; accounting
	// must still decode and inventory them rather than hiding them behind one
	// generic validation failure.
	opcodes, err := stagedGCStructOpcodeInventory(m)
	if err != nil {
		return stagedGCStructLeaderDelta{}, stagedGCStructLeaderPin{}, err
	}
	return stagedGCStructLeaderDelta{
		Filename: pin.Filename, CommandLine: pin.CommandLine, SourceLine: pin.SourceLine,
		Size: pin.Size, SHA256: pin.SHA256, Class: pin.Product.String(), Gate: pin.Product.gateReason(),
		TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m), Opcodes: opcodes,
		Actions: append([]string(nil), pin.Actions...),
	}, pin, nil
}

func replayStagedGCStructScript(t *testing.T, tmp string, script stagedSpecScript) (stagedSpecCounts, []stagedGCStructLeaderDelta, map[string]int) {
	t.Helper()
	var counts stagedSpecCounts
	gates := map[string]int{}
	var latestDefinition []byte
	var current *stagedGCStructLeaderPin
	var currentModule stagedSpecModule
	var live []stagedSpecModule
	defer func() {
		for i := len(live) - 1; i >= 0; i-- {
			_ = live[i].in.Close()
			_ = live[i].c.Close()
		}
	}()
	seenPins := map[string]bool{}
	seenActions := map[string][]string{}
	leaders := make([]stagedGCStructLeaderDelta, 0, len(stagedGCStructLeaderPins))
	pinnedInvalidSeen := 0

	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("gc/struct.wast:%d read module definition: %v", cmd.Line, err)
				latestDefinition = nil
				continue
			}
			latestDefinition = data
			current = nil
			currentModule = stagedSpecModule{}
		case "module_instance":
			if latestDefinition == nil {
				counts.Failures++
				t.Errorf("gc/struct.wast:%d module instance has no definition", cmd.Line)
				continue
			}
			leader, pin, err := stagedGCStructLeaderDeltaFor(latestDefinition, cmd.Line)
			if err != nil {
				counts.Failures++
				t.Error(err)
				continue
			}
			if seenPins[pin.Filename] {
				counts.Failures++
				t.Errorf("gc/struct.wast:%d duplicate leader %s", cmd.Line, pin.Filename)
				continue
			}
			seenPins[pin.Filename] = true
			leaders = append(leaders, leader)
			current = &pin
			c, compileErr := compileStagedGCStruct(latestDefinition)
			if compileErr != nil {
				if executable, ok := stagedGCStructExecutionProduct(latestDefinition); ok {
					counts.Failures++
					counts.UnexpectedCompileRejects++
					t.Errorf("gc/struct.wast:%d executable leader %s (%s) rejected: %v", cmd.Line, pin.Filename, executable, compileErr)
					continue
				}
				counts.ExpectedFeatureRejects++
				gates[pin.Product.gateReason()]++
				continue
			}
			in, instantiateErr := instantiateCore(c, InstantiateOptions{})
			if instantiateErr != nil {
				_ = c.Close()
				counts.Failures++
				counts.UnexpectedLinkRejects++
				t.Errorf("gc/struct.wast:%d executable leader %s instantiate: %v", cmd.Line, pin.Filename, instantiateErr)
				continue
			}
			currentModule = stagedSpecModule{in: in, c: c}
			live = append(live, currentModule)
			counts.ModulesPassed++
		case "assert_invalid":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("gc/struct.wast:%d read invalid module: %v", cmd.Line, err)
				continue
			}
			m, decodeErr := corewasm.DecodeModule(data)
			if decodeErr != nil {
				counts.Failures++
				t.Errorf("gc/struct.wast:%d invalid module decode failed unexpectedly: %v", cmd.Line, decodeErr)
				continue
			}
			validationErr := corewasm.ValidateModule(m)
			wantCode, pinned := stagedGCStructPinnedInvalidCodes[cmd.Line]
			var verr *corewasm.ValidationError
			if !pinned || !errors.As(validationErr, &verr) || verr.Code != wantCode {
				counts.Failures++
				t.Errorf("gc/struct.wast:%d validation error = %v, want exact %v", cmd.Line, validationErr, wantCode)
				continue
			}
			if c, err := compileStagedGCStruct(data); err == nil {
				_ = c.Close()
				counts.Failures++
				t.Errorf("gc/struct.wast:%d invalid module compiled: %s", cmd.Line, cmd.Text)
				continue
			}
			pinnedInvalidSeen++
			counts.ExpectedInvalid++
		case "assert_return", "assert_trap", "action":
			if current == nil {
				counts.Failures++
				t.Errorf("gc/struct.wast:%d action has no classified current module", cmd.Line)
				continue
			}
			kind := "action"
			if cmd.Type == "assert_return" {
				kind = "return"
			} else if cmd.Type == "assert_trap" {
				kind = "trap"
			}
			seenActions[current.Filename] = append(seenActions[current.Filename], kind+":"+cmd.Action.Field)
			if currentModule.in == nil {
				counts.BlockedCommands++
				continue
			}
			if current.Product == stagedGCStructBasic && cmd.Action.Field == "new" {
				if _, err := currentModule.in.Invoke("new"); err == nil || !strings.Contains(err.Error(), "non-null anyref result") {
					counts.Failures++
					t.Errorf("gc/struct.wast:%d public ref.struct egress = %v, want explicit non-null anyref result rejection", cmd.Line, err)
					continue
				}
				gates[current.Product.gateReason()]++
				counts.BlockedCommands++
				continue
			}
			args := make([]uint64, len(cmd.Action.Args))
			valid := cmd.Action.Type == "invoke"
			for i, arg := range cmd.Action.Args {
				args[i], valid = stagedTypedReferenceArgument(currentModule, nil, arg)
				if !valid {
					break
				}
			}
			if !valid {
				counts.Failures++
				t.Errorf("gc/struct.wast:%d unsupported staged action", cmd.Line)
				continue
			}
			got, callErr := currentModule.in.Invoke(cmd.Action.Field, args...)
			if cmd.Type == "assert_trap" {
				if callErr == nil {
					counts.Failures++
					t.Errorf("gc/struct.wast:%d expected trap: %s", cmd.Line, cmd.Text)
				} else if current.Product == stagedGCStructNullDereference && !strings.Contains(callErr.Error(), "null reference") {
					counts.Failures++
					t.Errorf("gc/struct.wast:%d trap = %v, want null-reference trap class", cmd.Line, callErr)
				} else {
					counts.AssertionsPassed++
				}
				continue
			}
			if callErr != nil || len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("gc/struct.wast:%d result=%v err=%v want=%v", cmd.Line, got, callErr, cmd.Expected)
				continue
			}
			matched := true
			for i := range got {
				if !stagedTypedReferenceMatch(currentModule, got[i], cmd.Expected[i]) {
					matched = false
					break
				}
			}
			if !matched {
				counts.Failures++
				t.Errorf("gc/struct.wast:%d result=%v want=%v", cmd.Line, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		default:
			counts.Failures++
			t.Errorf("gc/struct.wast:%d unhandled command %q", cmd.Line, cmd.Type)
		}
	}
	if counts.ExpectedMalformed != 0 {
		counts.Failures++
		t.Error("gc/struct converter unexpectedly emitted source-only malformed command")
	}
	counts.Commands += len(stagedGCStructSourceOnlyMalformed)
	counts.ExpectedMalformed += len(stagedGCStructSourceOnlyMalformed)
	if pinnedInvalidSeen != len(stagedGCStructPinnedInvalidCodes) {
		counts.Failures++
		t.Errorf("gc/struct pinned invalid coverage = %d, want %d", pinnedInvalidSeen, len(stagedGCStructPinnedInvalidCodes))
	}
	if len(seenPins) != len(stagedGCStructLeaderPins) {
		counts.Failures++
		t.Errorf("gc/struct leader coverage = %d, want %d", len(seenPins), len(stagedGCStructLeaderPins))
	}
	for _, pin := range stagedGCStructLeaderPins {
		if !reflect.DeepEqual(seenActions[pin.Filename], pin.Actions) {
			counts.Failures++
			t.Errorf("%s action inventory = %v, want %v", pin.Filename, seenActions[pin.Filename], pin.Actions)
		}
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i].CommandLine < leaders[j].CommandLine })
	return counts, leaders, gates
}

func TestStagedOfficialGCStructAccounting(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/struct", &script)
	counts, leaders, gateCounts := replayStagedGCStructScript(t, tmp, script)
	if counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 || counts.Failures != 0 {
		t.Fatalf("staged gc/struct accounting has hidden gaps: %+v", counts)
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
	delta := stagedGCStructDelta{
		Schema: 2, SuiteRevision: stagedRelease3Revision, File: "gc/struct",
		SourceOnlyMalformed: append([]int(nil), stagedGCStructSourceOnlyMalformed...),
		Leaders:             leaders, Gates: gates, Counts: counts,
	}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedGCStructDeltaPath)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("WAGO_UPDATE_STAGED_SPEC") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("staged gc/struct accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact leaders and gates\n%s", got)
	}
	t.Logf("staged gc/struct accounting: commands=%d modules=%d assertions=%d feature-rejects=%d blocked=%d invalid=%d malformed=%d",
		counts.Commands, counts.ModulesPassed, counts.AssertionsPassed, counts.ExpectedFeatureRejects, counts.BlockedCommands, counts.ExpectedInvalid, counts.ExpectedMalformed)
}

func TestStagedGCStructLeaderPinsRejectUnknowns(t *testing.T) {
	for _, pin := range stagedGCStructLeaderPins {
		if strings.TrimSpace(pin.gateReasonForTest()) == "" {
			t.Fatalf("%s has empty gate reason", pin.Filename)
		}
	}
	unknown := []byte("not a wasm module")
	if _, ok := stagedGCStructLeaderPinFor(unknown, 9); ok {
		t.Fatal("unknown gc/struct binary matched a leader pin")
	}
}

func (p stagedGCStructLeaderPin) gateReasonForTest() string { return p.Product.gateReason() }
