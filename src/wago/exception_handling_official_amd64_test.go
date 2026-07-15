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
	"sort"
	"strings"
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

const stagedExceptionHandlingDeltaPath = "tests/spec-v3-staged-exception-handling.json"

var stagedExceptionHandlingOfficialFiles = []string{
	"exceptions/tag", "exceptions/throw", "exceptions/throw_ref", "exceptions/try_table", "ref_null",
}

// The official interpreter validates malformed quoted modules while producing
// its binary script, so those source commands have no binary-script record.
// Keep their exact source lines explicit instead of silently omitting them.
var stagedExceptionHandlingSourceOnlyMalformed = map[string][]int{
	"exceptions/try_table": {339, 344},
}

type stagedExceptionHandlingGateCount struct {
	Boundary string `json:"boundary"`
	Reason   string `json:"reason"`
	Count    int    `json:"count"`
}

type stagedExceptionHandlingFileDelta struct {
	Name                string                             `json:"name"`
	Status              string                             `json:"status"`
	SourceOnlyMalformed []int                              `json:"source_only_malformed,omitempty"`
	Gates               []stagedExceptionHandlingGateCount `json:"gates,omitempty"`
	Counts              stagedSpecCounts                   `json:"counts"`
}

type stagedExceptionHandlingDelta struct {
	Schema        int                                `json:"schema"`
	SuiteRevision string                             `json:"suite_revision"`
	Files         []stagedExceptionHandlingFileDelta `json:"files"`
	Gates         []stagedExceptionHandlingGateCount `json:"gates,omitempty"`
	Totals        stagedSpecCounts                   `json:"totals"`
}

const (
	stagedEHBoundaryDecoderValidator = "decoder-validator"
	stagedEHBoundaryProduct          = "product-lifecycle"
	stagedEHBoundaryUnwind           = "native-unwind-abi"
	stagedEHBoundaryExceptionRef     = "exception-reference-roots"
	stagedEHBoundaryGC               = "gc-interaction"
	stagedEHBoundaryPlatform         = "platform"
)

var stagedExceptionHandlingKnownGates = map[string]map[string]bool{
	stagedEHBoundaryDecoderValidator: {
		"throw instruction validation is incomplete": true,
	},
	stagedEHBoundaryProduct: {
		"tag imports, exports, and cross-module link identity are outside the bounded local product slice": true,
	},
	stagedEHBoundaryUnwind: {
		"general try_table catch dispatch remains outside the bounded native unwind ABI": true,
	},
	stagedEHBoundaryExceptionRef: {
		"throw_ref, catch_ref, catch_all_ref, exn, and noexn require rooted exception values": true,
	},
	stagedEHBoundaryGC: {
		"GC-managed tag payloads require collector roots and barriers": true,
	},
	stagedEHBoundaryPlatform: {
		"exception handling has no native backend on this platform": true,
	},
}

func stagedExceptionHandlingGateList(counts map[string]int) []stagedExceptionHandlingGateCount {
	out := make([]stagedExceptionHandlingGateCount, 0, len(counts))
	for key, count := range counts {
		boundary, reason, ok := strings.Cut(key, "\x00")
		if !ok || !stagedExceptionHandlingKnownGates[boundary][reason] {
			panic("unknown staged exception-handling gate: " + key)
		}
		out = append(out, stagedExceptionHandlingGateCount{Boundary: boundary, Reason: reason, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Boundary != out[j].Boundary {
			return out[i].Boundary < out[j].Boundary
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

func stagedExceptionHandlingModuleGate(base string, data []byte) (boundary, reason string, err error) {
	m, err := corewasm.DecodeModule(data)
	if err != nil {
		return "", "", fmt.Errorf("decode: %w", err)
	}
	if err := corewasm.ValidateModule(m); err != nil {
		var verr *corewasm.ValidationError
		if !errors.As(err, &verr) || verr.Code != corewasm.ErrUnsupportedValidationOpcode {
			return "", "", fmt.Errorf("validate: %w", err)
		}
		if base == "exceptions/throw_ref" {
			return stagedEHBoundaryExceptionRef, "throw_ref, catch_ref, catch_all_ref, exn, and noexn require rooted exception values", nil
		}
		if verr.Detail == corewasm.InstrThrow.String() {
			return stagedEHBoundaryDecoderValidator, "throw instruction validation is incomplete", nil
		}
		if verr.Detail == corewasm.InstrThrowRef.String() {
			return stagedEHBoundaryExceptionRef, "throw_ref, catch_ref, catch_all_ref, exn, and noexn require rooted exception values", nil
		}
		return "", "", fmt.Errorf("validate: %w", err)
	}

	switch base {
	case "exceptions/tag":
		return stagedEHBoundaryProduct, "tag imports, exports, and cross-module link identity are outside the bounded local product slice", nil
	case "exceptions/try_table":
		return stagedEHBoundaryUnwind, "general try_table catch dispatch remains outside the bounded native unwind ABI", nil
	case "ref_null":
		return stagedEHBoundaryExceptionRef, "throw_ref, catch_ref, catch_all_ref, exn, and noexn require rooted exception values", nil
	case "exceptions/throw_ref":
		return stagedEHBoundaryExceptionRef, "throw_ref, catch_ref, catch_all_ref, exn, and noexn require rooted exception values", nil
	case "exceptions/throw":
		return stagedEHBoundaryUnwind, "general try_table catch dispatch remains outside the bounded native unwind ABI", nil
	default:
		return "", "", fmt.Errorf("unclassified exception-handling file %q", base)
	}
}

func replayStagedExceptionThrowScript(t *testing.T, tmp string, script stagedSpecScript) (counts stagedSpecCounts, gates map[string]int) {
	t.Helper()
	gates = map[string]int{}
	var current stagedSpecModule
	defer func() {
		if current.in != nil {
			_ = current.in.Close()
		}
		if current.c != nil {
			_ = current.c.Close()
		}
	}()
	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("exceptions/throw.wast:%d read module: %v", cmd.Line, err)
				continue
			}
			cfg := NewRuntimeConfig()
			features := cfg.frontendFeatures()
			features.ExceptionHandling = true
			c, err := compileWithFrontendFeatures(cfg, data, features)
			if err != nil {
				counts.UnexpectedCompileRejects++
				counts.Failures++
				t.Errorf("exceptions/throw.wast:%d compile: %v", cmd.Line, err)
				continue
			}
			in, err := instantiateCore(c, InstantiateOptions{})
			if err != nil {
				_ = c.Close()
				counts.UnexpectedLinkRejects++
				counts.Failures++
				t.Errorf("exceptions/throw.wast:%d instantiate: %v", cmd.Line, err)
				continue
			}
			current = stagedSpecModule{in: in, c: c}
			counts.ModulesPassed++
		case "assert_invalid":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("exceptions/throw.wast:%d read invalid module: %v", cmd.Line, err)
				continue
			}
			m, err := corewasm.DecodeModule(data)
			if err == nil {
				err = corewasm.ValidateModule(m)
			}
			if err == nil {
				counts.Failures++
				t.Errorf("exceptions/throw.wast:%d invalid module validated: %s", cmd.Line, cmd.Text)
				continue
			}
			counts.ExpectedInvalid++
		case "assert_return", "assert_exception":
			if current.in == nil || cmd.Action.Type != "invoke" {
				counts.Failures++
				t.Errorf("exceptions/throw.wast:%d unavailable or unsupported action", cmd.Line)
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
				t.Errorf("exceptions/throw.wast:%d unsupported scalar argument", cmd.Line)
				continue
			}
			got, callErr := current.in.Invoke(cmd.Action.Field, args...)
			if cmd.Type == "assert_exception" {
				if callErr == nil || !strings.Contains(callErr.Error(), "unhandled WebAssembly exception") {
					counts.Failures++
					t.Errorf("exceptions/throw.wast:%d result=%v err=%v, want exception", cmd.Line, got, callErr)
				} else {
					counts.AssertionsPassed++
				}
				continue
			}
			if callErr != nil || len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("exceptions/throw.wast:%d result=%v err=%v want=%v", cmd.Line, got, callErr, cmd.Expected)
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
				t.Errorf("exceptions/throw.wast:%d result=%v want=%v", cmd.Line, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		default:
			counts.Failures++
			t.Errorf("exceptions/throw.wast:%d unhandled command %q", cmd.Line, cmd.Type)
		}
	}
	return counts, gates
}

func replayStagedExceptionHandlingScript(t *testing.T, base, tmp string, script stagedSpecScript) (counts stagedSpecCounts, gates map[string]int) {
	t.Helper()
	if base == "exceptions/throw" {
		return replayStagedExceptionThrowScript(t, tmp, script)
	}
	gates = map[string]int{}
	definitions := map[string][]byte{}
	var latestDefinition []byte
	moduleAvailable := false
	namedAvailable := map[string]bool{}

	recordModuleGate := func(data []byte, cmd stagedSpecCommand) {
		boundary, reason, err := stagedExceptionHandlingModuleGate(base, data)
		if err != nil {
			counts.UnexpectedCompileRejects++
			counts.Failures++
			t.Errorf("%s.wast:%d valid module classification failed: %v", base, cmd.Line, err)
			moduleAvailable = false
			return
		}
		if !stagedExceptionHandlingKnownGates[boundary][reason] {
			counts.Failures++
			t.Errorf("%s.wast:%d unknown gate %q/%q", base, cmd.Line, boundary, reason)
			moduleAvailable = false
			return
		}
		counts.ExpectedFeatureRejects++
		gates[boundary+"\x00"+reason]++
		moduleAvailable = false
		if cmd.Name != "" {
			namedAvailable[cmd.Name] = false
		}
	}

	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read module definition: %v", base, cmd.Line, err)
				continue
			}
			latestDefinition = data
			if cmd.Name != "" {
				definitions[cmd.Name] = data
			}
		case "module_instance":
			data := latestDefinition
			if cmd.Module != "" {
				data = definitions[cmd.Module]
			}
			if data == nil {
				counts.Failures++
				t.Errorf("%s.wast:%d unavailable module definition %q", base, cmd.Line, cmd.Module)
				continue
			}
			recordModuleGate(data, cmd)
		case "module":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read module: %v", base, cmd.Line, err)
				continue
			}
			recordModuleGate(data, cmd)
		case "register":
			available := moduleAvailable
			if cmd.Name != "" {
				available = namedAvailable[cmd.Name]
			}
			if !available {
				counts.BlockedCommands++
				continue
			}
			counts.Failures++
			t.Errorf("%s.wast:%d unexpectedly reached register", base, cmd.Line)
		case "assert_invalid":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read invalid module: %v", base, cmd.Line, err)
				continue
			}
			m, err := corewasm.DecodeModule(data)
			if err == nil {
				err = corewasm.ValidateModule(m)
			}
			if err == nil {
				counts.Failures++
				t.Errorf("%s.wast:%d invalid module validated: %s", base, cmd.Line, cmd.Text)
				continue
			}
			counts.ExpectedInvalid++
		case "assert_malformed":
			counts.ExpectedMalformed++
		case "assert_unlinkable", "assert_uninstantiable":
			data := latestDefinition
			var err error
			if cmd.Filename != "" {
				data, err = os.ReadFile(filepath.Join(tmp, cmd.Filename))
			}
			if err != nil || data == nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read rejected module: %v", base, cmd.Line, err)
				continue
			}
			recordModuleGate(data, cmd)
		case "assert_return", "action", "assert_trap", "assert_exception":
			available := moduleAvailable
			if cmd.Action.Module != "" {
				available = namedAvailable[cmd.Action.Module]
			}
			if !available {
				counts.BlockedCommands++
				continue
			}
			counts.Failures++
			t.Errorf("%s.wast:%d unexpectedly reached action %q", base, cmd.Line, cmd.Type)
		default:
			counts.Failures++
			t.Errorf("%s.wast:%d unhandled command %q", base, cmd.Line, cmd.Type)
		}
	}
	return counts, gates
}

func TestStagedOfficialExceptionHandlingFamilyAccounting(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	delta := stagedExceptionHandlingDelta{Schema: 2, SuiteRevision: stagedRelease3Revision}
	totalGates := map[string]int{}
	for _, base := range stagedExceptionHandlingOfficialFiles {
		t.Run(strings.ReplaceAll(base, "/", "-"), func(t *testing.T) {
			var script stagedSpecScript
			tmp := stagedOfficialTypedReferenceJSON(t, base, &script)
			counts, gates := replayStagedExceptionHandlingScript(t, base, tmp, script)
			sourceOnlyMalformed := stagedExceptionHandlingSourceOnlyMalformed[base]
			if len(sourceOnlyMalformed) != 0 {
				if counts.ExpectedMalformed != 0 {
					counts.Failures++
					t.Errorf("%s.wast converter unexpectedly emitted malformed commands as well as pinned source-only lines", base)
				}
				counts.Commands += len(sourceOnlyMalformed)
				counts.ExpectedMalformed += len(sourceOnlyMalformed)
			}
			delta.Files = append(delta.Files, stagedExceptionHandlingFileDelta{Name: base, Status: "accounted", SourceOnlyMalformed: sourceOnlyMalformed, Gates: stagedExceptionHandlingGateList(gates), Counts: counts})
			for gate, count := range gates {
				totalGates[gate] += count
			}
			delta.Totals.add(counts)
		})
	}
	delta.Gates = stagedExceptionHandlingGateList(totalGates)
	if delta.Totals.UnexpectedCompileRejects != 0 || delta.Totals.UnexpectedLinkRejects != 0 || delta.Totals.Failures != 0 {
		t.Fatalf("staged exception-handling accounting has hidden gaps: %+v", delta.Totals)
	}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedExceptionHandlingDeltaPath)
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
		t.Fatalf("staged exception-handling accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact gates\n%s", got)
	}
	t.Logf("staged exception-handling accounting: files=%d commands=%d modules=%d assertions=%d feature-rejects=%d blocked=%d invalid=%d malformed=%d unlinkable=%d uninstantiable=%d",
		len(delta.Files), delta.Totals.Commands, delta.Totals.ModulesPassed, delta.Totals.AssertionsPassed,
		delta.Totals.ExpectedFeatureRejects, delta.Totals.BlockedCommands, delta.Totals.ExpectedInvalid, delta.Totals.ExpectedMalformed,
		delta.Totals.ExpectedUnlinkable, delta.Totals.ExpectedUninstantiable)
}
