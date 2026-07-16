//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"crypto/sha256"
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

const stagedGCTypeSubtypingDeltaPath = "tests/spec-v3-staged-gc-type-subtyping.json"

var stagedGCTypeSubtypingLeaderSourceLines = []int{
	3, 15, 24, 37, 43, 53, 68, 89, 115, 124, 151, 159, 177, 188,
	229, 290, 319, 348, 360, 378, 390, 401, 422, 438, 461, 471,
	486, 497, 540, 566, 572, 578, 588, 598, 614, 621, 628, 639,
	652, 659, 668, 677, 692, 706, 901,
}

var stagedGCTypeSubtypingInvalidSourceLines = []int{
	139, 205, 215, 726, 734, 742, 750, 763, 771, 779, 787, 795,
	803, 811, 819, 827, 835, 843, 851, 859, 867, 875, 883, 891,
}

var stagedGCTypeSubtypingUnlinkableSourceLines = []int{510, 520, 530, 548, 556, 605, 698, 713}

var stagedGCTypeSubtypingValidValidatorGapLines = map[int]bool{}

var stagedGCTypeSubtypingInvalidValidatorGapLines = map[int]bool{}

type stagedGCTypeSubtypingLeaderDelta struct {
	Filename     string                      `json:"filename"`
	CommandLine  int                         `json:"command_line"`
	SourceLine   int                         `json:"source_line"`
	Size         int                         `json:"size"`
	SHA256       string                      `json:"sha256"`
	Class        string                      `json:"class"`
	Gate         string                      `json:"gate,omitempty"`
	Admitted     bool                        `json:"admitted,omitempty"`
	ValidatorGap bool                        `json:"validator_gap"`
	TypeGraph    string                      `json:"type_graph"`
	StateGraph   string                      `json:"state_graph"`
	Opcodes      []stagedGCStructOpcodeCount `json:"opcodes,omitempty"`
	Actions      []string                    `json:"actions,omitempty"`
}

type stagedGCTypeSubtypingInvalidDelta struct {
	Filename     string `json:"filename"`
	CommandLine  int    `json:"command_line"`
	SourceLine   int    `json:"source_line"`
	Size         int    `json:"size"`
	SHA256       string `json:"sha256"`
	Text         string `json:"text"`
	ValidatorGap bool   `json:"validator_gap"`
}

type stagedGCTypeSubtypingUnlinkableDelta struct {
	Filename    string `json:"filename"`
	CommandLine int    `json:"command_line"`
	SourceLine  int    `json:"source_line"`
	Size        int    `json:"size"`
	SHA256      string `json:"sha256"`
	Text        string `json:"text"`
	Admitted    bool   `json:"admitted,omitempty"`
	TypeGraph   string `json:"type_graph"`
	StateGraph  string `json:"state_graph"`
}

type stagedGCTypeSubtypingDelta struct {
	Schema        int                                    `json:"schema"`
	SuiteRevision string                                 `json:"suite_revision"`
	File          string                                 `json:"file"`
	Leaders       []stagedGCTypeSubtypingLeaderDelta     `json:"leaders"`
	Invalids      []stagedGCTypeSubtypingInvalidDelta    `json:"invalids"`
	Unlinkables   []stagedGCTypeSubtypingUnlinkableDelta `json:"unlinkables"`
	Gates         []stagedTypedReferenceGateCount        `json:"gates"`
	Counts        stagedSpecCounts                       `json:"counts"`
}

func stagedGCTypeSubtypingClass(sourceLine int) (class, gate string) {
	if sourceLine <= 0 {
		return "", ""
	}
	switch {
	case sourceLine <= 24:
		return "declaration-supertype-graph", "metadata-only struct/array/function supertype declarations"
	case sourceLine <= 53:
		return "recursive-definition-graph", "metadata-only recursive struct supertype declarations"
	case sourceLine <= 89:
		return "recursive-function-subsumption", "collector-free recursive function subsumption bodies"
	case sourceLine <= 188:
		return "ref-func-global-subsumption", "collector-free local ref.func global subsumption"
	case sourceLine == 229:
		return "runtime-recursive-call-cast", "recursive function subtype call_indirect and ref.cast runtime"
	case sourceLine == 290:
		return "runtime-finality-call-cast", "final function identity call_indirect and ref.cast runtime"
	case sourceLine == 319:
		return "runtime-table-call-subtyping", "typed function table call_indirect runtime"
	case sourceLine <= 480:
		return "runtime-ref-test-function-identity", "collector-free function ref.test over recursive structural identity"
	case sourceLine == 497 || sourceLine == 572 || sourceLine == 588 || sourceLine == 621 || sourceLine == 639 || sourceLine == 659 || sourceLine == 677:
		return "link-consumer", "collector-free recursive function subtype link consumer"
	case sourceLine < 901:
		return "link-provider", "collector-free recursive function subtype link provider"
	case sourceLine == 901:
		return "non-flat-exported-function", "non-flat declared subtype exported function invocation"
	default:
		return "", ""
	}
}

func stagedGCTypeSubtypingActionKey(cmd stagedSpecCommand) string {
	valueKey := func(v stagedSpecValue) string {
		var value string
		if err := json.Unmarshal(v.Value, &value); err != nil {
			value = strings.TrimSpace(string(v.Value))
		}
		key := v.Type + "=" + value
		if v.HeapType != "" {
			key += "@" + v.HeapType
		}
		return key
	}
	args := make([]string, len(cmd.Action.Args))
	for i, arg := range cmd.Action.Args {
		args[i] = valueKey(arg)
	}
	key := cmd.Type + ":" + cmd.Action.Field + "(" + strings.Join(args, ",") + ")"
	switch cmd.Type {
	case "assert_return":
		results := make([]string, len(cmd.Expected))
		for i, result := range cmd.Expected {
			results[i] = valueKey(result)
		}
		return key + "->(" + strings.Join(results, ",") + ")"
	case "assert_trap":
		return key + "!" + cmd.Text
	default:
		return key
	}
}

func stagedGCTypeSubtypingModuleDelta(data []byte, filename string, commandLine, sourceLine int) (stagedGCTypeSubtypingLeaderDelta, error) {
	class, gate := stagedGCTypeSubtypingClass(sourceLine)
	if class == "" || gate == "" {
		return stagedGCTypeSubtypingLeaderDelta{}, fmt.Errorf("unclassified gc/type-subtyping source line %d", sourceLine)
	}
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return stagedGCTypeSubtypingLeaderDelta{}, err
	}
	validationErr := corewasm.ValidateModule(m)
	validatorGap := validationErr != nil
	if validatorGap != stagedGCTypeSubtypingValidValidatorGapLines[commandLine] {
		return stagedGCTypeSubtypingLeaderDelta{}, fmt.Errorf("valid leader validator-gap state=%v, want %v (validation=%v)", validatorGap, stagedGCTypeSubtypingValidValidatorGapLines[commandLine], validationErr)
	}
	if validatorGap {
		var verr *corewasm.ValidationError
		if !errors.As(validationErr, &verr) || verr.Code != corewasm.ErrTypeMismatch {
			return stagedGCTypeSubtypingLeaderDelta{}, fmt.Errorf("valid leader validation=%v, want exact type mismatch gap", validationErr)
		}
	}
	opcodes, err := stagedGCStructOpcodeInventory(m)
	if err != nil {
		return stagedGCTypeSubtypingLeaderDelta{}, err
	}
	return stagedGCTypeSubtypingLeaderDelta{
		Filename: filename, CommandLine: commandLine, SourceLine: sourceLine,
		Size: len(data), SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Class: class, Gate: gate, ValidatorGap: validatorGap,
		TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m), Opcodes: opcodes,
	}, nil
}

func stagedGCTypeSubtypingUnlinkableDeltaFor(data []byte, filename string, commandLine, sourceLine int, text string) (stagedGCTypeSubtypingUnlinkableDelta, error) {
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return stagedGCTypeSubtypingUnlinkableDelta{}, err
	}
	if err := corewasm.ValidateModule(m); err != nil {
		return stagedGCTypeSubtypingUnlinkableDelta{}, fmt.Errorf("unlinkable validation: %w", err)
	}
	return stagedGCTypeSubtypingUnlinkableDelta{
		Filename: filename, CommandLine: commandLine, SourceLine: sourceLine,
		Size: len(data), SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Text: text,
		TypeGraph: stagedGCStructTypeGraph(m), StateGraph: stagedGCStructStateGraph(m),
	}, nil
}

func replayStagedGCTypeSubtypingScript(t *testing.T, tmp string, script stagedSpecScript) stagedGCTypeSubtypingDelta {
	t.Helper()
	delta := stagedGCTypeSubtypingDelta{Schema: 2, SuiteRevision: stagedRelease3Revision, File: "gc/type-subtyping"}
	gates := map[string]int{}
	var latest, currentFilename string
	var latestData []byte
	leaderIndex, invalidIndex, unlinkableIndex := 0, 0, 0
	currentLeader := -1
	currentAdmitted := false
	currentProduct := stagedGCTypeSubtypingProduct(0)
	var currentInstance *Instance
	var currentCompiled *Compiled
	registered := map[string]stagedSpecModule{}
	var registeredLive []stagedSpecModule
	closeCurrent := func() {
		if currentInstance != nil {
			_ = currentInstance.Close()
			currentInstance = nil
		}
		if currentCompiled != nil {
			_ = currentCompiled.Close()
			currentCompiled = nil
		}
	}
	defer func() {
		closeCurrent()
		for i := len(registeredLive) - 1; i >= 0; i-- {
			_ = registeredLive[i].in.Close()
			_ = registeredLive[i].c.Close()
		}
	}()

	for _, cmd := range script.Commands {
		delta.Counts.Commands++
		switch cmd.Type {
		case "module_definition":
			closeCurrent()
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d read definition: %v", cmd.Line, err)
				continue
			}
			latest, latestData = cmd.Filename, data
			currentLeader = -1
			currentAdmitted = false
			currentProduct = 0
		case "module_instance":
			if leaderIndex >= len(stagedGCTypeSubtypingLeaderSourceLines) {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d unexpected leader %s", cmd.Line, latest)
				continue
			}
			leader, err := stagedGCTypeSubtypingModuleDelta(latestData, latest, cmd.Line, stagedGCTypeSubtypingLeaderSourceLines[leaderIndex])
			if err != nil {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d leader: %v", cmd.Line, err)
				continue
			}
			delta.Leaders = append(delta.Leaders, leader)
			currentLeader = len(delta.Leaders) - 1
			currentFilename = latest
			leaderIndex++
			m, decodeErr := corewasm.DecodeModule(latestData)
			product, productErr := stagedGCTypeSubtypingProductShape(m)
			admitted := decodeErr == nil && productErr == nil && stagedGCTypeSubtypingProductPinned(latestData, product)
			if !admitted {
				delta.Counts.ExpectedFeatureRejects++
				gates[leader.Gate]++
				continue
			}
			c, compileErr := compileStagedGCTypeSubtypingProductForTest(latestData)
			if compileErr != nil {
				delta.Counts.Failures++
				delta.Counts.UnexpectedCompileRejects++
				t.Errorf("gc/type-subtyping.wast:%d admitted leader %s compile: %v", cmd.Line, latest, compileErr)
				continue
			}
			imports, importsErr := stagedSpecImports(c, registered, nil)
			if importsErr != nil {
				_ = c.Close()
				delta.Counts.Failures++
				delta.Counts.UnexpectedLinkRejects++
				t.Errorf("gc/type-subtyping.wast:%d admitted leader %s imports: %v", cmd.Line, latest, importsErr)
				continue
			}
			in, instantiateErr := instantiateCore(c, InstantiateOptions{Imports: imports})
			if instantiateErr != nil {
				_ = c.Close()
				delta.Counts.Failures++
				delta.Counts.UnexpectedLinkRejects++
				t.Errorf("gc/type-subtyping.wast:%d admitted leader %s instantiate: %v", cmd.Line, latest, instantiateErr)
				continue
			}
			if in.gc != nil {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d admitted leader %s allocated a collector", cmd.Line, latest)
			}
			if product.usesRefTest() || product.usesRuntimeFunctionIdentity() || product.usesLinkFunctionIdentity() {
				currentInstance = in
				currentCompiled = c
			} else {
				_ = in.Close()
				_ = c.Close()
			}
			delta.Leaders[currentLeader].Gate = ""
			delta.Leaders[currentLeader].Admitted = true
			currentAdmitted = true
			currentProduct = product
			delta.Counts.ModulesPassed++
		case "register":
			if currentLeader < 0 {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d register has no classified leader", cmd.Line)
				continue
			}
			if !currentAdmitted {
				delta.Counts.BlockedCommands++
				continue
			}
			wantAs := ""
			switch currentProduct {
			case stagedGCTypeSubtypingLinkProvider:
				wantAs = "M"
			case stagedGCTypeSubtypingFinalityLinkProvider:
				wantAs = "M2"
			case stagedGCTypeSubtypingStructLinkProvider:
				wantAs = "M3"
			case stagedGCTypeSubtypingStructProjectionLinkProvider:
				wantAs = "M4"
			case stagedGCTypeSubtypingStructMismatchLinkProvider:
				wantAs = "M5"
			case stagedGCTypeSubtypingIndependentStructLinkProvider:
				wantAs = "M6"
			case stagedGCTypeSubtypingExtendedProjectionLinkProvider:
				wantAs = "M7"
			case stagedGCTypeSubtypingDuplicateRecursiveLinkProvider:
				wantAs = "M8"
			}
			if wantAs == "" || currentInstance == nil || currentCompiled == nil || cmd.As != wantAs {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d admitted register is outside the exact pinned linking clusters", cmd.Line)
				continue
			}
			m := stagedSpecModule{in: currentInstance, c: currentCompiled}
			registered[cmd.As] = m
			registeredLive = append(registeredLive, m)
			currentInstance = nil
			currentCompiled = nil
		case "assert_return", "assert_trap", "action":
			if currentLeader < 0 {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d action has no classified leader", cmd.Line)
				continue
			}
			delta.Leaders[currentLeader].Actions = append(delta.Leaders[currentLeader].Actions, stagedGCTypeSubtypingActionKey(cmd))
			if !currentAdmitted {
				delta.Counts.BlockedCommands++
				continue
			}
			if currentInstance == nil || len(cmd.Action.Args) != 0 {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d admitted action is outside the exact bounded product contract", cmd.Line)
				continue
			}
			if currentProduct.usesRuntimeFunctionIdentity() {
				_, err := currentInstance.Invoke(cmd.Action.Field)
				switch cmd.Type {
				case "assert_return":
					if len(cmd.Expected) != 0 || err != nil {
						delta.Counts.Failures++
						t.Errorf("gc/type-subtyping.wast:%d invoke %s = %v; want empty success", cmd.Line, cmd.Action.Field, err)
						continue
					}
				case "assert_trap":
					want := "wrong signature"
					if cmd.Text == "cast" {
						want = "cast failure"
					}
					if err == nil || !strings.Contains(err.Error(), want) {
						delta.Counts.Failures++
						t.Errorf("gc/type-subtyping.wast:%d invoke %s = %v; want %s trap", cmd.Line, cmd.Action.Field, err, want)
						continue
					}
				default:
					delta.Counts.Failures++
					t.Errorf("gc/type-subtyping.wast:%d runtime function-identity action kind %s is unsupported", cmd.Line, cmd.Type)
					continue
				}
				delta.Counts.AssertionsPassed++
				continue
			}
			if cmd.Type != "assert_return" || len(cmd.Expected) == 0 || len(cmd.Expected) > 8 {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d admitted action is outside the exact bounded function ref.test contract", cmd.Line)
				continue
			}
			wantBits := make([]uint64, len(cmd.Expected))
			valid := true
			for i, expected := range cmd.Expected {
				var want string
				if expected.Type != "i32" {
					valid = false
					break
				}
				if err := json.Unmarshal(expected.Value, &want); err != nil || (want != "0" && want != "1") {
					valid = false
					break
				}
				if want == "1" {
					wantBits[i] = 1
				}
			}
			if !valid {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d expected results are outside the exact i32 boolean contract: %+v", cmd.Line, cmd.Expected)
				continue
			}
			got, err := currentInstance.Invoke(cmd.Action.Field)
			if err != nil || !reflect.DeepEqual(got, wantBits) {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d invoke %s = %v, %v; want %v", cmd.Line, cmd.Action.Field, got, err, wantBits)
				continue
			}
			delta.Counts.AssertionsPassed++
		case "assert_invalid":
			if invalidIndex >= len(stagedGCTypeSubtypingInvalidSourceLines) {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d unexpected invalid %s", cmd.Line, cmd.Filename)
				continue
			}
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d read invalid: %v", cmd.Line, err)
				continue
			}
			m, decodeErr := corewasm.DecodeModule(data)
			if decodeErr != nil {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d invalid decode: %v", cmd.Line, decodeErr)
				continue
			}
			validationErr := corewasm.ValidateModule(m)
			validatorGap := validationErr == nil
			if validatorGap != stagedGCTypeSubtypingInvalidValidatorGapLines[cmd.Line] {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d validator-gap state=%v, want %v (validation=%v)", cmd.Line, validatorGap, stagedGCTypeSubtypingInvalidValidatorGapLines[cmd.Line], validationErr)
				continue
			}
			if !validatorGap {
				var verr *corewasm.ValidationError
				if !errors.As(validationErr, &verr) || verr.Code != corewasm.ErrTypeMismatch {
					delta.Counts.Failures++
					t.Errorf("gc/type-subtyping.wast:%d validation=%v, want exact type mismatch", cmd.Line, validationErr)
					continue
				}
			}
			delta.Invalids = append(delta.Invalids, stagedGCTypeSubtypingInvalidDelta{
				Filename: cmd.Filename, CommandLine: cmd.Line, SourceLine: stagedGCTypeSubtypingInvalidSourceLines[invalidIndex],
				Size: len(data), SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Text: cmd.Text, ValidatorGap: validatorGap,
			})
			invalidIndex++
			delta.Counts.ExpectedInvalid++
		case "assert_unlinkable":
			if unlinkableIndex >= len(stagedGCTypeSubtypingUnlinkableSourceLines) {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d unexpected unlinkable %s", cmd.Line, latest)
				continue
			}
			unlinkable, err := stagedGCTypeSubtypingUnlinkableDeltaFor(latestData, latest, cmd.Line, stagedGCTypeSubtypingUnlinkableSourceLines[unlinkableIndex], cmd.Text)
			if err != nil {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d unlinkable: %v", cmd.Line, err)
				continue
			}
			unlinkableIndex++
			m, decodeErr := corewasm.DecodeModule(latestData)
			product, productErr := stagedGCTypeSubtypingProductShape(m)
			admitted := decodeErr == nil && productErr == nil && product.isLinkConsumer() && stagedGCTypeSubtypingProductPinned(latestData, product)
			if !admitted {
				delta.Unlinkables = append(delta.Unlinkables, unlinkable)
				delta.Counts.BlockedCommands++
				continue
			}
			c, compileErr := compileStagedGCTypeSubtypingProductForTest(latestData)
			if compileErr != nil {
				delta.Counts.Failures++
				delta.Counts.UnexpectedCompileRejects++
				t.Errorf("gc/type-subtyping.wast:%d unlinkable compile: %v", cmd.Line, compileErr)
				continue
			}
			imports, importsErr := stagedSpecImports(c, registered, nil)
			if importsErr != nil {
				_ = c.Close()
				delta.Counts.Failures++
				delta.Counts.UnexpectedLinkRejects++
				t.Errorf("gc/type-subtyping.wast:%d unlinkable imports: %v", cmd.Line, importsErr)
				continue
			}
			in, linkErr := instantiateCore(c, InstantiateOptions{Imports: imports})
			if in != nil {
				_ = in.Close()
			}
			_ = c.Close()
			if linkErr == nil || !strings.Contains(linkErr.Error(), "signature mismatch") {
				delta.Counts.Failures++
				t.Errorf("gc/type-subtyping.wast:%d unlinkable = %v, want incompatible import type", cmd.Line, linkErr)
				continue
			}
			unlinkable.Admitted = true
			delta.Unlinkables = append(delta.Unlinkables, unlinkable)
			delta.Counts.ExpectedUnlinkable++
		default:
			delta.Counts.Failures++
			t.Errorf("gc/type-subtyping.wast:%d unhandled command %q after %s", cmd.Line, cmd.Type, currentFilename)
		}
	}
	if leaderIndex != len(stagedGCTypeSubtypingLeaderSourceLines) || invalidIndex != len(stagedGCTypeSubtypingInvalidSourceLines) || unlinkableIndex != len(stagedGCTypeSubtypingUnlinkableSourceLines) {
		delta.Counts.Failures++
		t.Errorf("gc/type-subtyping coverage leaders=%d/%d invalids=%d/%d unlinkables=%d/%d", leaderIndex, len(stagedGCTypeSubtypingLeaderSourceLines), invalidIndex, len(stagedGCTypeSubtypingInvalidSourceLines), unlinkableIndex, len(stagedGCTypeSubtypingUnlinkableSourceLines))
	}
	gateNames := make([]string, 0, len(gates))
	for name := range gates {
		gateNames = append(gateNames, name)
	}
	sort.Strings(gateNames)
	for _, name := range gateNames {
		delta.Gates = append(delta.Gates, stagedTypedReferenceGateCount{Family: "gc", Reason: name, Count: gates[name]})
	}
	return delta
}

func TestStagedOfficialGCTypeSubtypingAccounting(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/type-subtyping", &script)
	delta := replayStagedGCTypeSubtypingScript(t, tmp, script)
	counts := delta.Counts
	if counts.Commands != 170 || counts.ModulesPassed != 40 || counts.AssertionsPassed != 23 || counts.ExpectedInvalid != 24 || counts.ExpectedMalformed != 0 || counts.ExpectedUnlinkable != 6 || counts.ExpectedUninstantiable != 0 || counts.ExpectedFeatureRejects != 5 || counts.BlockedCommands != 11 || counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 || counts.Failures != 0 {
		t.Fatalf("staged gc/type-subtyping accounting has hidden or changed gaps: %+v", counts)
	}
	admitted := 0
	for _, leader := range delta.Leaders {
		if leader.Admitted {
			admitted++
		}
	}
	if len(delta.Leaders) != 45 || admitted != 40 || len(delta.Invalids) != 24 || len(delta.Unlinkables) != 8 || len(stagedGCTypeSubtypingValidValidatorGapLines) != 0 || len(stagedGCTypeSubtypingInvalidValidatorGapLines) != 0 {
		t.Fatalf("staged gc/type-subtyping inventory changed: leaders=%d admitted=%d invalids=%d unlinkables=%d valid-validator-gaps=%d invalid-validator-gaps=%d", len(delta.Leaders), admitted, len(delta.Invalids), len(delta.Unlinkables), len(stagedGCTypeSubtypingValidValidatorGapLines), len(stagedGCTypeSubtypingInvalidValidatorGapLines))
	}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedGCTypeSubtypingDeltaPath)
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
		t.Fatalf("read %s: %v", stagedGCTypeSubtypingDeltaPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("staged gc/type-subtyping accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing all leaders, invalids, unlinkables, actions, and gates\n%s", got)
	}
}

func TestStagedGCTypeSubtypingAccountingRejectsUnknowns(t *testing.T) {
	if class, gate := stagedGCTypeSubtypingClass(0); class != "" || gate != "" {
		t.Fatalf("unknown source line classified as %q/%q", class, gate)
	}
	if got := stagedGCTypeSubtypingActionKey(stagedSpecCommand{Type: "assert_trap", Text: "cast", Action: stagedSpecAction{Field: "fail"}}); got != "assert_trap:fail()!cast" {
		t.Fatalf("action key=%q", got)
	}
}
