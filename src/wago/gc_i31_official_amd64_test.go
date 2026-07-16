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
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

const stagedGCI31DeltaPath = "tests/spec-v3-staged-gc-i31.json"

type stagedGCI31Class uint8

const (
	stagedGCI31Core stagedGCI31Class = iota + 1
	stagedGCI31Table
	stagedGCI31Env
	stagedGCI31TableGlobalInitializer
	stagedGCI31GlobalInitializer
	stagedGCI31AnyGlobal
	stagedGCI31AnyTable
)

func (c stagedGCI31Class) String() string {
	switch c {
	case stagedGCI31Core:
		return "core"
	case stagedGCI31Table:
		return "i31-table"
	case stagedGCI31Env:
		return "numeric-env"
	case stagedGCI31TableGlobalInitializer:
		return "table-global-initializer"
	case stagedGCI31GlobalInitializer:
		return "global-global-initializer"
	case stagedGCI31AnyGlobal:
		return "anyref-global"
	case stagedGCI31AnyTable:
		return "anyref-table"
	default:
		return "unknown"
	}
}

func (c stagedGCI31Class) gateReason() string {
	switch c {
	case stagedGCI31Core:
		return "i31 encode/get/null/mutable-global/public-result product"
	case stagedGCI31Table:
		return "i31 table operations and element lifecycle"
	case stagedGCI31Env:
		return ""
	case stagedGCI31TableGlobalInitializer:
		return "i31 table initializer from imported numeric global"
	case stagedGCI31GlobalInitializer:
		return "i31 global initializer from imported numeric global"
	case stagedGCI31AnyGlobal:
		return "anyref global i31 storage and cast product"
	case stagedGCI31AnyTable:
		return "anyref table i31 storage, cast, and element lifecycle"
	default:
		return "unknown gc/i31 product"
	}
}

type stagedGCI31LeaderPin struct {
	Filename    string
	CommandLine int
	SourceLine  int
	Size        int
	SHA256      string
	Class       stagedGCI31Class
	Actions     []string
}

var stagedGCI31LeaderPins = []stagedGCI31LeaderPin{
	{Filename: "i31.0.wasm", CommandLine: 19, SourceLine: 1, Size: 252, SHA256: "4bdd4d0f186a2fd617b41ad4940e17f2c0415514ebc636a56e41496e8c392aea", Class: stagedGCI31Core, Actions: []string{"return:new", "return:get_u", "return:get_u", "return:get_u", "return:get_u", "return:get_u", "return:get_u", "return:get_u", "return:get_u", "return:get_s", "return:get_s", "return:get_s", "return:get_s", "return:get_s", "return:get_s", "return:get_s", "return:get_s", "trap:get_u-null", "trap:get_s-null", "return:get_globals", "action:set_global", "return:get_globals"}},
	{Filename: "i31.1.wasm", CommandLine: 61, SourceLine: 61, Size: 259, SHA256: "c2a2062022d3b99a27aada76d1fe14cdacf3387e7497943d17d3681f65ed7329", Class: stagedGCI31Table, Actions: []string{"return:size", "return:get", "return:get", "return:get", "return:grow", "return:size", "return:get", "return:get", "action:fill", "return:get", "return:get", "action:copy", "return:get", "return:get", "action:init", "return:get", "return:get", "return:get"}},
	{Filename: "i31.2.wasm", CommandLine: 87, SourceLine: 123, Size: 31, SHA256: "99e43692c16af0e869ec03ae71d348ebd73fff4d401ca3e91393ad6151d6cf96", Class: stagedGCI31Env},
	{Filename: "i31.3.wasm", CommandLine: 97, SourceLine: 128, Size: 96, SHA256: "0a26e50d6ec8ccbaf1cc3a59fc2e1be6dca2c22219ba70ca80a01451882bf0e4", Class: stagedGCI31TableGlobalInitializer, Actions: []string{"return:get", "return:get", "return:get"}},
	{Filename: "i31.4.wasm", CommandLine: 110, SourceLine: 140, Size: 88, SHA256: "024b9a334c9a7bb6933243fa1eaf60ed653338034bfaf9e54fb23ccc13c9ad87", Class: stagedGCI31GlobalInitializer, Actions: []string{"return:get"}},
	{Filename: "i31.5.wasm", CommandLine: 124, SourceLine: 150, Size: 131, SHA256: "757d3266617ea901221facbda9b660d6d1fd52adf492ffa82ba0658dc846b26d", Class: stagedGCI31AnyGlobal, Actions: []string{"return:get_globals", "action:set_global", "return:get_globals"}},
	{Filename: "i31.6.wasm", CommandLine: 147, SourceLine: 168, Size: 262, SHA256: "572387a2c9d7ea9112f3940025b7c57041cd9478185ed7e32bb93a01fbfa5a69", Class: stagedGCI31AnyTable, Actions: []string{"return:size", "return:get", "return:get", "return:get", "return:grow", "return:size", "return:get", "return:get", "action:fill", "return:get", "return:get", "action:copy", "return:get", "return:get", "action:init", "return:get", "return:get", "return:get"}},
}

type stagedGCI31LeaderDelta struct {
	Filename    string                      `json:"filename"`
	CommandLine int                         `json:"command_line"`
	SourceLine  int                         `json:"source_line"`
	Size        int                         `json:"size"`
	SHA256      string                      `json:"sha256"`
	Class       string                      `json:"class"`
	Gate        string                      `json:"gate,omitempty"`
	TypeGraph   string                      `json:"type_graph"`
	StateGraph  string                      `json:"state_graph"`
	Opcodes     []stagedGCStructOpcodeCount `json:"opcodes,omitempty"`
	Actions     []string                    `json:"actions,omitempty"`
}

type stagedGCI31Delta struct {
	Schema        int                             `json:"schema"`
	SuiteRevision string                          `json:"suite_revision"`
	File          string                          `json:"file"`
	Leaders       []stagedGCI31LeaderDelta        `json:"leaders"`
	Gates         []stagedTypedReferenceGateCount `json:"gates"`
	Counts        stagedSpecCounts                `json:"counts"`
}

func stagedGCI31LeaderPinFor(data []byte, line int) (stagedGCI31LeaderPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedGCI31LeaderPins {
		if pin.CommandLine == line && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedGCI31LeaderPin{}, false
}

func stagedGCI31LeaderDeltaFor(data []byte, line int) (stagedGCI31LeaderDelta, stagedGCI31LeaderPin, error) {
	pin, ok := stagedGCI31LeaderPinFor(data, line)
	if !ok {
		return stagedGCI31LeaderDelta{}, stagedGCI31LeaderPin{}, fmt.Errorf("unknown gc/i31 binary at command line %d (size=%d)", line, len(data))
	}
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return stagedGCI31LeaderDelta{}, stagedGCI31LeaderPin{}, err
	}
	opcodes, err := stagedGCStructOpcodeInventory(m)
	if err != nil {
		return stagedGCI31LeaderDelta{}, stagedGCI31LeaderPin{}, err
	}
	return stagedGCI31LeaderDelta{
		Filename: pin.Filename, CommandLine: pin.CommandLine, SourceLine: pin.SourceLine,
		Size: pin.Size, SHA256: pin.SHA256, Class: pin.Class.String(), Gate: pin.Class.gateReason(),
		TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m), Opcodes: opcodes,
		Actions: append([]string(nil), pin.Actions...),
	}, pin, nil
}

func compileStagedGCI31(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	if _, ok := stagedGCI31PinnedProduct(data); ok {
		features.GCI31Products = true
	}
	return compileWithFrontendFeatures(cfg, data, features)
}

func replayStagedGCI31Script(t *testing.T, tmp string, script stagedSpecScript) (stagedSpecCounts, []stagedGCI31LeaderDelta, map[string]int) {
	t.Helper()
	var counts stagedSpecCounts
	gates := map[string]int{}
	var latest []byte
	var current *stagedGCI31LeaderPin
	var currentModule stagedSpecModule
	var live []stagedSpecModule
	defer func() {
		for i := len(live) - 1; i >= 0; i-- {
			_ = live[i].in.Close()
			_ = live[i].c.Close()
		}
	}()
	named := map[string]stagedSpecModule{}
	registered := map[string]stagedSpecModule{}
	seenPins := map[string]bool{}
	seenActions := map[string][]string{}
	leaders := make([]stagedGCI31LeaderDelta, 0, len(stagedGCI31LeaderPins))

	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("gc/i31.wast:%d read definition: %v", cmd.Line, err)
				latest, current = nil, nil
				continue
			}
			latest, current = data, nil
		case "module_instance":
			leader, pin, err := stagedGCI31LeaderDeltaFor(latest, cmd.Line)
			if err != nil {
				counts.Failures++
				t.Error(err)
				continue
			}
			if seenPins[pin.Filename] {
				counts.Failures++
				t.Errorf("gc/i31.wast:%d duplicate leader %s", cmd.Line, pin.Filename)
				continue
			}
			seenPins[pin.Filename] = true
			leaders = append(leaders, leader)
			current = &pin
			currentModule = stagedSpecModule{}
			c, compileErr := compileStagedGCI31(latest)
			if compileErr != nil {
				if pin.Class == stagedGCI31Env {
					counts.Failures++
					counts.UnexpectedCompileRejects++
					t.Errorf("gc/i31.wast:%d numeric env rejected: %v", cmd.Line, compileErr)
					continue
				}
				counts.ExpectedFeatureRejects++
				gates[pin.Class.gateReason()]++
				t.Logf("gc/i31.wast:%d gated %s: %v", cmd.Line, pin.Filename, compileErr)
				continue
			}
			imports, err := stagedSpecImports(c, registered, nil)
			if err != nil {
				_ = c.Close()
				counts.Failures++
				counts.UnexpectedLinkRejects++
				t.Errorf("gc/i31.wast:%d imports: %v", cmd.Line, err)
				continue
			}
			in, err := instantiateCore(c, InstantiateOptions{Imports: imports})
			if err != nil {
				_ = c.Close()
				counts.Failures++
				counts.UnexpectedLinkRejects++
				t.Errorf("gc/i31.wast:%d instantiate: %v", cmd.Line, err)
				continue
			}
			currentModule = stagedSpecModule{in: in, c: c}
			live = append(live, currentModule)
			if cmd.Name != "" {
				named[cmd.Name] = currentModule
			}
			counts.ModulesPassed++
		case "register":
			m := currentModule
			if cmd.Name != "" {
				m = named[cmd.Name]
			}
			if m.in == nil {
				counts.BlockedCommands++
				continue
			}
			if cmd.As == "" {
				counts.Failures++
				t.Errorf("gc/i31.wast:%d register has empty name", cmd.Line)
				continue
			}
			registered[cmd.As] = m
		case "assert_return", "assert_trap", "action":
			if current == nil {
				counts.Failures++
				t.Errorf("gc/i31.wast:%d action has no classified leader", cmd.Line)
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
				t.Errorf("gc/i31.wast:%d unsupported staged action", cmd.Line)
				continue
			}
			got, invokeErr := currentModule.in.Invoke(cmd.Action.Field, args...)
			if cmd.Type == "assert_trap" {
				if invokeErr == nil {
					counts.Failures++
					t.Errorf("gc/i31.wast:%d %s returned normally, want trap", cmd.Line, cmd.Action.Field)
				} else {
					counts.AssertionsPassed++
				}
				continue
			}
			if invokeErr != nil || len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("gc/i31.wast:%d result=%v err=%v want=%v", cmd.Line, got, invokeErr, cmd.Expected)
				continue
			}
			matched := true
			for i := range got {
				if current.Class == stagedGCI31Core && cmd.Action.Field == "new" && cmd.Expected[i].Type == "ref" {
					matched = got[i]>>32 == 0 && uint32(got[i])&1 == 1
				} else {
					matched = stagedTypedReferenceMatch(currentModule, got[i], cmd.Expected[i])
				}
				if !matched {
					break
				}
			}
			if !matched {
				counts.Failures++
				t.Errorf("gc/i31.wast:%d result=%v want=%v", cmd.Line, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		default:
			counts.Failures++
			t.Errorf("gc/i31.wast:%d unhandled command %q", cmd.Line, cmd.Type)
		}
	}
	if len(seenPins) != len(stagedGCI31LeaderPins) {
		counts.Failures++
		t.Errorf("gc/i31 leader coverage = %d, want %d", len(seenPins), len(stagedGCI31LeaderPins))
	}
	for _, pin := range stagedGCI31LeaderPins {
		if !reflect.DeepEqual(seenActions[pin.Filename], pin.Actions) {
			counts.Failures++
			t.Errorf("%s action inventory = %v, want %v", pin.Filename, seenActions[pin.Filename], pin.Actions)
		}
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i].CommandLine < leaders[j].CommandLine })
	return counts, leaders, gates
}

func TestStagedOfficialGCI31Accounting(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/i31", &script)
	counts, leaders, gateCounts := replayStagedGCI31Script(t, tmp, script)
	if counts.Commands != 80 || counts.ModulesPassed != 2 || counts.AssertionsPassed != 22 || counts.ExpectedFeatureRejects != 5 || counts.BlockedCommands != 43 || counts.ExpectedInvalid != 0 || counts.ExpectedMalformed != 0 || counts.Failures != 0 || counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 {
		t.Fatalf("staged gc/i31 accounting has hidden or changed gaps: %+v", counts)
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
	delta := stagedGCI31Delta{Schema: 2, SuiteRevision: stagedRelease3Revision, File: "gc/i31", Leaders: leaders, Gates: gates, Counts: counts}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedGCI31DeltaPath)
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
		t.Fatalf("read %s: %v", stagedGCI31DeltaPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("staged gc/i31 accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact leaders and gates\n%s", got)
	}
}

func TestStagedGCI31LeaderPinsRejectUnknowns(t *testing.T) {
	if _, ok := stagedGCI31LeaderPinFor([]byte("not wasm"), 19); ok {
		t.Fatal("unknown gc/i31 binary matched a leader pin")
	}
	for _, pin := range stagedGCI31LeaderPins {
		if pin.Class != stagedGCI31Env && pin.Class.gateReason() == "" {
			t.Fatalf("%s has empty gate reason", pin.Filename)
		}
	}
}
