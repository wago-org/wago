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
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	stagedGCBrOnCastDeltaPath         = "tests/spec-v3-staged-gc-br-on-cast.json"
	stagedGCBrOnCastOfficialExecution = false
)

type stagedGCBrOnCastClass uint8

const (
	stagedGCBrOnCastAbstract stagedGCBrOnCastClass = iota + 1
	stagedGCBrOnCastConcrete
	stagedGCBrOnCastNullability
)

func (c stagedGCBrOnCastClass) String() string {
	switch c {
	case stagedGCBrOnCastAbstract:
		return "abstract"
	case stagedGCBrOnCastConcrete:
		return "concrete"
	case stagedGCBrOnCastNullability:
		return "nullability"
	default:
		return "unknown"
	}
}

func (c stagedGCBrOnCastClass) gateReason(op string) string {
	switch c {
	case stagedGCBrOnCastAbstract:
		return op + " abstract table/control product"
	case stagedGCBrOnCastConcrete:
		return op + " declared-super/canonical control product"
	case stagedGCBrOnCastNullability:
		return op + " nullability/control-shape product"
	default:
		return "unknown " + op + " product"
	}
}

type stagedGCBrOnCastLeaderPin struct {
	Base         string
	Filename     string
	CommandLine  int
	SourceLine   int
	Size         int
	SHA256       string
	Class        stagedGCBrOnCastClass
	ControlGraph string
	Actions      []string
}

func stagedGCBrOnCastAbstractActions(prefix string) []string {
	fields := []struct {
		name   string
		values []uint32
	}{
		{prefix + "null", []uint32{0, ^uint32(0), ^uint32(0), ^uint32(0), ^uint32(0)}},
		{prefix + "i31", []uint32{^uint32(0), 7, ^uint32(0), ^uint32(0), ^uint32(0)}},
		{prefix + "struct", []uint32{^uint32(0), ^uint32(0), 6, ^uint32(0), ^uint32(0)}},
		{prefix + "array", []uint32{^uint32(0), ^uint32(0), ^uint32(0), 3, ^uint32(0)}},
		{"null-diff", []uint32{1, 0, 1, 0, 0}},
	}
	out := []string{"action:init"}
	for _, field := range fields {
		for _, value := range field.values {
			out = append(out, "return:"+field.name+":"+strconv.FormatUint(uint64(value), 10))
		}
	}
	return out
}

var stagedGCBrOnCastLeaderPins = []stagedGCBrOnCastLeaderPin{
	{
		Base: "gc/br_on_cast", Filename: "br_on_cast.0.wasm", CommandLine: 28, SourceLine: 3, Size: 385,
		SHA256: "4429db7587ba73adfc04c44a2369bab38343d7f582d1745372940ba96c04a263", Class: stagedGCBrOnCastAbstract,
		ControlGraph: "br_on_null depth0; br_on_cast depth0 any?->i31; br_on_cast depth0 any?->struct then nested depth1 struct?->$st and depth0 any?->$at; br_on_cast depth0 any?->array; null-diff nested depth1 any?->struct?",
		Actions:      stagedGCBrOnCastAbstractActions("br_on_"),
	},
	{
		Base: "gc/br_on_cast", Filename: "br_on_cast.1.wasm", CommandLine: 106, SourceLine: 104, Size: 772,
		SHA256: "4eabadd98e55bc0c83600f072a16bab75e3d74170d2f24bb6bdae4acf0b5491b", Class: stagedGCBrOnCastConcrete,
		ControlGraph: "test-sub: 30 br_on_cast edges over one result label, including nested result blocks; test-canon: 14 canonical br_on_cast result edges; selected edges carry the original structref",
		Actions:      []string{"action:test-sub", "action:test-canon"},
	},
	{
		Base: "gc/br_on_cast", Filename: "br_on_cast.2.wasm", CommandLine: 118, SourceLine: 211, Size: 111,
		SHA256: "cc5bdeb4b57409c6e194ae094f2a50ece4dce6afa66cd25ec07b4624ccb96632", Class: stagedGCBrOnCastNullability,
		ControlGraph: "three depth1 branches: any->t, any?->t, any?->t?; outer function result proves source/target nullability refinement",
	},
	{
		Base: "gc/br_on_cast_fail", Filename: "br_on_cast_fail.0.wasm", CommandLine: 29, SourceLine: 3, Size: 403,
		SHA256: "a1d339dfb4ed4aa308f1a9eeb8293f0475d427b0d7aea7e8b99159c054fc6815", Class: stagedGCBrOnCastAbstract,
		ControlGraph: "br_on_non_null depth0; br_on_cast_fail depth0 any?->i31/struct/array; struct success falls through nested depth1/depth0 br_on_cast refinement; null-diff depth1 any?->struct?",
		Actions:      stagedGCBrOnCastAbstractActions("br_on_non_"),
	},
	{
		Base: "gc/br_on_cast_fail", Filename: "br_on_cast_fail.1.wasm", CommandLine: 149, SourceLine: 104, Size: 876,
		SHA256: "88894127fabe14a42dc6d5af44027318078b702bcc67fc34e752d145a8164312", Class: stagedGCBrOnCastConcrete,
		ControlGraph: "test-sub: 46 br_on_cast_fail edges over one result label and nested result blocks; test-canon: 14 canonical fallthrough edges; failure branches carry the original source structref",
		Actions:      []string{"action:test-sub", "action:test-canon"},
	},
	{
		Base: "gc/br_on_cast_fail", Filename: "br_on_cast_fail.2.wasm", CommandLine: 161, SourceLine: 226, Size: 103,
		SHA256: "6725d0fa741974c1214cdabc35fa962779f74abd711cc21b04d67d0c4073578e", Class: stagedGCBrOnCastNullability,
		ControlGraph: "three depth1 branches: any->t, any?->t, any?->t?; branch carries source type and fallthrough carries refined target",
	},
}

type stagedGCBrOnCastInvalidPin struct {
	Base        string `json:"-"`
	Filename    string `json:"filename"`
	CommandLine int    `json:"command_line"`
	SourceLine  int    `json:"source_line"`
	Size        int    `json:"size"`
	SHA256      string `json:"sha256"`
	Error       string `json:"error"`
}

var stagedGCBrOnCastInvalidPins = []stagedGCBrOnCastInvalidPin{
	{Base: "gc/br_on_cast", Filename: "br_on_cast.3.wasm", CommandLine: 119, SourceLine: 225, Size: 59, SHA256: "6d2f0c34d7e7cee07ba5ef16a724d7790efb6efa4c4446c80e602d4b9045c9df", Error: "type mismatch"},
	{Base: "gc/br_on_cast", Filename: "br_on_cast.4.wasm", CommandLine: 128, SourceLine: 234, Size: 59, SHA256: "726611afbf86d03231f8a7a32f7c86aef4009e98faba16eacf5dc49ffd3cb2be", Error: "type mismatch"},
	{Base: "gc/br_on_cast", Filename: "br_on_cast.5.wasm", CommandLine: 137, SourceLine: 243, Size: 58, SHA256: "e5af9bca584b387208156e89db27bbb6614553ebac3d1b25800d8ad4fd766c6f", Error: "type mismatch"},
	{Base: "gc/br_on_cast", Filename: "br_on_cast.6.wasm", CommandLine: 146, SourceLine: 252, Size: 48, SHA256: "d200c57c66d9e670d53408c247cd10c6e3b54ea5a5e3c16dbfdf06cbc3a730c5", Error: "type mismatch"},
	{Base: "gc/br_on_cast", Filename: "br_on_cast.7.wasm", CommandLine: 154, SourceLine: 260, Size: 48, SHA256: "1801ed1526c95fb0e2ff4a3a079cf0b4cddfb345d7a0c1b1d5981418d892c7f0", Error: "type mismatch"},
	{Base: "gc/br_on_cast", Filename: "br_on_cast.8.wasm", CommandLine: 162, SourceLine: 271, Size: 77, SHA256: "bc70dfea9c8b9c2b07df8fd5549473a5979f6a1eb90cb210073e6e0df34de461", Error: "type mismatch"},
	{Base: "gc/br_on_cast_fail", Filename: "br_on_cast_fail.3.wasm", CommandLine: 162, SourceLine: 240, Size: 58, SHA256: "81559387a89755aee960ee3a677f1512296848d81fa87479ae433359bdbe70d7", Error: "type mismatch"},
	{Base: "gc/br_on_cast_fail", Filename: "br_on_cast_fail.4.wasm", CommandLine: 171, SourceLine: 249, Size: 59, SHA256: "42d52b162474632c324576ba8eee4bd3692b7cfd91ee350dfa84475f5a718355", Error: "type mismatch"},
	{Base: "gc/br_on_cast_fail", Filename: "br_on_cast_fail.5.wasm", CommandLine: 180, SourceLine: 258, Size: 57, SHA256: "7dff22ba04cd1a6785e7a0a64a5e2e5873718d58233c28e9003ec0dc988e13c2", Error: "type mismatch"},
	{Base: "gc/br_on_cast_fail", Filename: "br_on_cast_fail.6.wasm", CommandLine: 189, SourceLine: 267, Size: 48, SHA256: "4b4fc5e668b8a4dd8ced6fcc80954547349e0f0b4e507c09df810adba53f72b7", Error: "type mismatch"},
	{Base: "gc/br_on_cast_fail", Filename: "br_on_cast_fail.7.wasm", CommandLine: 197, SourceLine: 275, Size: 48, SHA256: "f458e7b957c5ce2ea1ab56efb2e768023689b546376e429ba4d6d027924d8ac0", Error: "type mismatch"},
	{Base: "gc/br_on_cast_fail", Filename: "br_on_cast_fail.8.wasm", CommandLine: 205, SourceLine: 286, Size: 77, SHA256: "52ed5e8210cd8324606e95a0df6862ccade946e983dd1b2e0fbd3e9065b94992", Error: "type mismatch"},
}

type stagedGCBrOnCastLeaderDelta struct {
	Filename     string                      `json:"filename"`
	CommandLine  int                         `json:"command_line"`
	SourceLine   int                         `json:"source_line"`
	Size         int                         `json:"size"`
	SHA256       string                      `json:"sha256"`
	Class        string                      `json:"class"`
	Gate         string                      `json:"gate"`
	TypeGraph    string                      `json:"type_graph"`
	StateGraph   string                      `json:"state_graph"`
	ControlGraph string                      `json:"control_graph"`
	Opcodes      []stagedGCStructOpcodeCount `json:"opcodes,omitempty"`
	Actions      []string                    `json:"actions,omitempty"`
}

type stagedGCBrOnCastFileDelta struct {
	Name     string                          `json:"name"`
	Leaders  []stagedGCBrOnCastLeaderDelta   `json:"leaders"`
	Invalids []stagedGCBrOnCastInvalidPin    `json:"invalids"`
	Gates    []stagedTypedReferenceGateCount `json:"gates"`
	Counts   stagedSpecCounts                `json:"counts"`
}

type stagedGCBrOnCastDelta struct {
	Schema        int                         `json:"schema"`
	SuiteRevision string                      `json:"suite_revision"`
	Files         []stagedGCBrOnCastFileDelta `json:"files"`
	Totals        stagedSpecCounts            `json:"totals"`
}

func stagedGCBrOnCastLeaderPinFor(base string, data []byte, line int) (stagedGCBrOnCastLeaderPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedGCBrOnCastLeaderPins {
		if pin.Base == base && pin.CommandLine == line && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedGCBrOnCastLeaderPin{}, false
}

func stagedGCBrOnCastInvalidPinFor(base string, data []byte, line int) (stagedGCBrOnCastInvalidPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedGCBrOnCastInvalidPins {
		if pin.Base == base && pin.CommandLine == line && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedGCBrOnCastInvalidPin{}, false
}

func stagedGCBrOnCastLeaderDeltaFor(base string, data []byte, line int) (stagedGCBrOnCastLeaderDelta, stagedGCBrOnCastLeaderPin, error) {
	pin, ok := stagedGCBrOnCastLeaderPinFor(base, data, line)
	if !ok {
		return stagedGCBrOnCastLeaderDelta{}, stagedGCBrOnCastLeaderPin{}, fmt.Errorf("unknown %s binary at command line %d (size=%d)", base, line, len(data))
	}
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return stagedGCBrOnCastLeaderDelta{}, stagedGCBrOnCastLeaderPin{}, err
	}
	opcodes, err := stagedGCStructOpcodeInventory(m)
	if err != nil {
		return stagedGCBrOnCastLeaderDelta{}, stagedGCBrOnCastLeaderPin{}, err
	}
	op := "br_on_cast"
	if base == "gc/br_on_cast_fail" {
		op = "br_on_cast_fail"
	}
	gate := pin.Class.gateReason(op)
	if stagedGCBrOnCastOfficialExecution {
		gate = ""
	}
	return stagedGCBrOnCastLeaderDelta{
		Filename: pin.Filename, CommandLine: pin.CommandLine, SourceLine: pin.SourceLine,
		Size: pin.Size, SHA256: pin.SHA256, Class: pin.Class.String(), Gate: gate,
		TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m), ControlGraph: pin.ControlGraph,
		Opcodes: opcodes, Actions: append([]string(nil), pin.Actions...),
	}, pin, nil
}

func compileStagedGCBrOnCastAccounting(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	if stagedGCBrOnCastOfficialExecution {
		features.GCStructProducts = true
		if product, ok := stagedGCStructExecutionProduct(data); ok && (product == stagedGCStructBrOnCastAbstract || product == stagedGCStructBrOnCastFailAbstract) {
			features.GCArrayProducts = true
			features.GCI31Products = true
		}
	}
	return compileWithFrontendFeatures(cfg, data, features)
}

func stagedGCBrOnCastActionKey(cmd stagedSpecCommand) string {
	if cmd.Type == "action" {
		return "action:" + cmd.Action.Field
	}
	if cmd.Type != "assert_return" || len(cmd.Expected) != 1 {
		return cmd.Type + ":" + cmd.Action.Field
	}
	value, ok := stagedSpecScalar(cmd.Expected[0])
	if !ok {
		return "return:" + cmd.Action.Field + ":invalid"
	}
	return "return:" + cmd.Action.Field + ":" + strconv.FormatUint(uint64(uint32(value)), 10)
}

func stagedGCBrOnCastInvalids(base string) []stagedGCBrOnCastInvalidPin {
	var out []stagedGCBrOnCastInvalidPin
	for _, pin := range stagedGCBrOnCastInvalidPins {
		if pin.Base == base {
			out = append(out, pin)
		}
	}
	return out
}

func replayStagedGCBrOnCastScript(t *testing.T, base, tmp string, script stagedSpecScript) (stagedSpecCounts, []stagedGCBrOnCastLeaderDelta, map[string]int) {
	t.Helper()
	var counts stagedSpecCounts
	gates := map[string]int{}
	var latest []byte
	var current *stagedGCBrOnCastLeaderPin
	seenLeaders := map[string]bool{}
	seenInvalids := map[string]bool{}
	seenActions := map[string][]string{}
	var leaders []stagedGCBrOnCastLeaderDelta

	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read definition: %v", base, cmd.Line, err)
				latest, current = nil, nil
				continue
			}
			latest, current = data, nil
		case "module_instance":
			leader, pin, err := stagedGCBrOnCastLeaderDeltaFor(base, latest, cmd.Line)
			if err != nil {
				counts.Failures++
				t.Error(err)
				continue
			}
			if seenLeaders[pin.Filename] {
				counts.Failures++
				t.Errorf("%s.wast:%d duplicate leader %s", base, cmd.Line, pin.Filename)
				continue
			}
			seenLeaders[pin.Filename] = true
			leaders = append(leaders, leader)
			current = &pin
			c, compileErr := compileStagedGCBrOnCastAccounting(latest)
			if compileErr == nil {
				_ = c.Close()
				counts.Failures++
				t.Errorf("%s.wast:%d branch product compiled before execution admission", base, cmd.Line)
				continue
			}
			if bytes.Contains([]byte(compileErr.Error()), []byte("validate:")) {
				counts.Failures++
				counts.UnexpectedCompileRejects++
				t.Errorf("%s.wast:%d valid leader failed validation: %v", base, cmd.Line, compileErr)
				continue
			}
			counts.ExpectedFeatureRejects++
			gates[leader.Gate]++
		case "action", "assert_return":
			if current == nil {
				counts.Failures++
				t.Errorf("%s.wast:%d action has no leader", base, cmd.Line)
				continue
			}
			seenActions[current.Filename] = append(seenActions[current.Filename], stagedGCBrOnCastActionKey(cmd))
			counts.BlockedCommands++
		case "assert_invalid":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read invalid: %v", base, cmd.Line, err)
				continue
			}
			pin, ok := stagedGCBrOnCastInvalidPinFor(base, data, cmd.Line)
			if !ok {
				counts.Failures++
				t.Errorf("%s.wast:%d unknown invalid binary size=%d", base, cmd.Line, len(data))
				continue
			}
			m, decodeErr := corewasm.DecodeModule(data)
			if decodeErr != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d invalid failed decode: %v", base, cmd.Line, decodeErr)
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
		default:
			counts.Failures++
			t.Errorf("%s.wast:%d unhandled command %q", base, cmd.Line, cmd.Type)
		}
	}
	if len(seenLeaders) != 3 {
		counts.Failures++
		t.Errorf("%s leader coverage=%d, want 3", base, len(seenLeaders))
	}
	if len(seenInvalids) != 6 {
		counts.Failures++
		t.Errorf("%s invalid coverage=%d, want 6", base, len(seenInvalids))
	}
	for _, pin := range stagedGCBrOnCastLeaderPins {
		if pin.Base == base && !reflect.DeepEqual(seenActions[pin.Filename], pin.Actions) {
			counts.Failures++
			t.Errorf("%s actions=%v, want %v", pin.Filename, seenActions[pin.Filename], pin.Actions)
		}
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i].CommandLine < leaders[j].CommandLine })
	return counts, leaders, gates
}

func TestStagedOfficialGCBrOnCastAccounting(t *testing.T) {
	bases := []string{"gc/br_on_cast", "gc/br_on_cast_fail"}
	delta := stagedGCBrOnCastDelta{Schema: 2, SuiteRevision: stagedRelease3Revision}
	for _, base := range bases {
		var script stagedSpecScript
		tmp := stagedOfficialTypedReferenceJSON(t, base, &script)
		counts, leaders, gateCounts := replayStagedGCBrOnCastScript(t, base, tmp, script)
		if counts.Commands != 40 || counts.ModulesPassed != 0 || counts.AssertionsPassed != 0 || counts.ExpectedFeatureRejects != 3 || counts.BlockedCommands != 28 || counts.ExpectedInvalid != 6 || counts.ExpectedMalformed != 0 || counts.Failures != 0 || counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 {
			t.Fatalf("staged %s accounting has hidden or changed gaps: %+v", base, counts)
		}
		var gates []stagedTypedReferenceGateCount
		for reason, count := range gateCounts {
			gates = append(gates, stagedTypedReferenceGateCount{Family: "gc", Reason: reason, Count: count})
		}
		sort.Slice(gates, func(i, j int) bool { return gates[i].Reason < gates[j].Reason })
		delta.Files = append(delta.Files, stagedGCBrOnCastFileDelta{Name: base, Leaders: leaders, Invalids: stagedGCBrOnCastInvalids(base), Gates: gates, Counts: counts})
		delta.Totals.add(counts)
	}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedGCBrOnCastDeltaPath)
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
		t.Fatalf("read %s: %v", stagedGCBrOnCastDeltaPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("staged branch-cast accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact leaders/invalids/actions\n%s", got)
	}
}

func TestStagedGCBrOnCastPinsRejectUnknowns(t *testing.T) {
	if _, ok := stagedGCBrOnCastLeaderPinFor("gc/br_on_cast", []byte("not wasm"), 28); ok {
		t.Fatal("unknown branch-cast binary matched")
	}
	if _, ok := stagedGCBrOnCastInvalidPinFor("gc/br_on_cast_fail", []byte("not wasm"), 162); ok {
		t.Fatal("unknown branch-cast invalid matched")
	}
}
