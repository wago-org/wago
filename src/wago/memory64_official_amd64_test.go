//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const stagedMemory64DeltaPath = "tests/spec-v3-staged-memory64.json"

// stagedMemory64OfficialFiles is the complete pinned Release 3 memory64 family
// excluding the nine table64-specific files, which have their own accounting
// runner and product boundary.
var stagedMemory64OfficialFiles = []string{
	"address64", "align64", "binary_leb128_64", "bulk64", "call_indirect64",
	"endianness64", "float_memory64", "load64", "memory_copy64", "memory_fill64",
	"memory_grow64", "memory_init64", "memory_redundancy64", "memory_trap64",
	"memory64-imports", "memory64",
}

type stagedMemory64GateCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type stagedMemory64FileDelta struct {
	Name   string                    `json:"name"`
	Status string                    `json:"status"`
	Gates  []stagedMemory64GateCount `json:"gates,omitempty"`
	Counts stagedSpecCounts          `json:"counts"`
}

type stagedMemory64Delta struct {
	Schema        int                       `json:"schema"`
	SuiteRevision string                    `json:"suite_revision"`
	Files         []stagedMemory64FileDelta `json:"files"`
	Gates         []stagedMemory64GateCount `json:"gates,omitempty"`
	Totals        stagedSpecCounts          `json:"totals"`
}

func stagedMemory64KnownGate(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	text := err.Error()
	for _, gate := range []struct {
		contains string
		reason   string
	}{
		{"exceeds staged ceiling 65535", "declared memory64 limit exceeds the 65,535-page execution ceiling"},
		{"requires a declared maximum no greater than 65535 pages", "memory64 declaration is outside the bounded local reservation policy"},
		{"outside staged scalar family", "memory64 instruction family is not yet staged"},
		{"requires exactly one local memory", "memory64 imports or multi-memory combinations are not yet staged"},
		{"64-bit memory imports remain outside the staged memory64 boundary", "memory64 imports are not yet staged"},
		{"table64 disabled", "table64 call_indirect/import support is not yet staged with memory64"},
		{"table64 without a declared maximum must be private and non-growing", "table64 call_indirect/import support is not yet staged with memory64"},
		{"staged table64 requires exactly one local table", "table64 call_indirect/import support is not yet staged with memory64"},
		{"64-bit table imports remain outside the staged table64 boundary", "table64 call_indirect/import support is not yet staged with memory64"},
	} {
		if strings.Contains(text, gate.contains) {
			return gate.reason, true
		}
	}
	return "", false
}

func stagedMemory64GateList(counts map[string]int) []stagedMemory64GateCount {
	out := make([]stagedMemory64GateCount, 0, len(counts))
	for reason, count := range counts {
		out = append(out, stagedMemory64GateCount{Reason: reason, Count: count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Reason < out[j].Reason })
	return out
}

func replayStagedMemory64Script(t *testing.T, base, tmp string, script stagedSpecScript) (counts stagedSpecCounts, gates map[string]int) {
	t.Helper()
	gates = map[string]int{}
	standardTable, err := NewTable(10, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer standardTable.Close()
	standardMemory, err := NewSharedMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer standardMemory.Close()
	noop := HostFunc(func(HostModule, []uint64, []uint64) {})
	standard := Imports{
		"spectest.print": noop, "spectest.print_i32": noop, "spectest.print_i64": noop,
		"spectest.print_f32": noop, "spectest.print_f64": noop,
		"spectest.print_i32_f32": noop, "spectest.print_f64_f64": noop,
		"spectest.global_i32": GlobalImport{Type: ValI32, Bits: I32(666)},
		"spectest.global_i64": GlobalImport{Type: ValI64, Bits: I64(666)},
		"spectest.global_f32": GlobalImport{Type: ValF32, Bits: F32(666)},
		"spectest.global_f64": GlobalImport{Type: ValF64, Bits: F64(666)},
		"spectest.memory":     standardMemory, "spectest.table": standardTable,
	}
	var current stagedSpecModule
	var live []stagedSpecModule
	defer func() {
		for i := len(live) - 1; i >= 0; i-- {
			_ = live[i].in.Close()
			_ = live[i].c.Close()
		}
	}()
	named := map[string]stagedSpecModule{}
	registered := map[string]stagedSpecModule{}
	definitions := map[string][]byte{}
	var latestDefinition []byte

	instantiate := func(data []byte, cmd stagedSpecCommand) (stagedSpecModule, error) {
		c, err := compileStagedMemory64(data)
		if err != nil {
			return stagedSpecModule{}, fmt.Errorf("compile: %w", err)
		}
		imports, err := stagedSpecImports(c, registered, standard)
		if err != nil {
			_ = c.Close()
			return stagedSpecModule{}, fmt.Errorf("imports: %w", err)
		}
		in, err := instantiateCore(c, InstantiateOptions{Imports: imports})
		if err != nil {
			_ = c.Close()
			return stagedSpecModule{}, fmt.Errorf("instantiate: %w", err)
		}
		m := stagedSpecModule{in: in, c: c}
		live = append(live, m)
		if cmd.Name != "" {
			named[cmd.Name] = m
		}
		return m, nil
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
				current = stagedSpecModule{}
				t.Errorf("%s.wast:%d unavailable module definition %q", base, cmd.Line, cmd.Module)
				continue
			}
			m, err := instantiate(data, cmd)
			if err != nil {
				if reason, ok := stagedMemory64KnownGate(err); ok {
					counts.ExpectedFeatureRejects++
					gates[reason]++
					current = stagedSpecModule{}
					continue
				}
				if strings.Contains(err.Error(), "compile:") {
					counts.UnexpectedCompileRejects++
				} else {
					counts.UnexpectedLinkRejects++
				}
				counts.Failures++
				current = stagedSpecModule{}
				t.Errorf("%s.wast:%d module instance rejected: %v", base, cmd.Line, err)
				continue
			}
			current = m
			counts.ModulesPassed++
		case "module":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read module: %v", base, cmd.Line, err)
				current = stagedSpecModule{}
				continue
			}
			m, err := instantiate(data, cmd)
			if err != nil {
				if reason, ok := stagedMemory64KnownGate(err); ok {
					counts.ExpectedFeatureRejects++
					gates[reason]++
					current = stagedSpecModule{}
					continue
				}
				if strings.Contains(err.Error(), "compile:") {
					counts.UnexpectedCompileRejects++
				} else {
					counts.UnexpectedLinkRejects++
				}
				counts.Failures++
				t.Errorf("%s.wast:%d module rejected: %v", base, cmd.Line, err)
				current = stagedSpecModule{}
				continue
			}
			current = m
			counts.ModulesPassed++
		case "register":
			m := current
			if cmd.Name != "" {
				m = named[cmd.Name]
			}
			if m.in == nil {
				counts.BlockedCommands++
				continue
			}
			if cmd.As == "" {
				counts.Failures++
				t.Errorf("%s.wast:%d register command has empty name", base, cmd.Line)
				continue
			}
			registered[cmd.As] = m
		case "assert_invalid":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read invalid module: %v", base, cmd.Line, err)
				continue
			}
			if c, err := compileStagedMemory64(data); err == nil {
				_ = c.Close()
				counts.Failures++
				t.Errorf("%s.wast:%d invalid module compiled: %s", base, cmd.Line, cmd.Text)
			} else {
				counts.ExpectedInvalid++
			}
		case "assert_malformed":
			counts.ExpectedMalformed++
		case "assert_unlinkable", "assert_uninstantiable":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read rejected module: %v", base, cmd.Line, err)
				continue
			}
			m, err := instantiate(data, cmd)
			if err == nil {
				_ = m.in.Close()
				counts.Failures++
				t.Errorf("%s.wast:%d expected instantiation rejection: %s", base, cmd.Line, cmd.Text)
				continue
			}
			if reason, ok := stagedMemory64KnownGate(err); ok {
				counts.ExpectedFeatureRejects++
				gates[reason]++
				continue
			}
			if cmd.Type == "assert_unlinkable" {
				counts.ExpectedUnlinkable++
			} else {
				counts.ExpectedUninstantiable++
			}
		case "assert_return", "action", "assert_trap":
			m := current
			if cmd.Action.Module != "" {
				m = named[cmd.Action.Module]
			}
			if m.in == nil {
				counts.BlockedCommands++
				continue
			}
			args := make([]uint64, len(cmd.Action.Args))
			valid := cmd.Action.Type == "invoke"
			for i, arg := range cmd.Action.Args {
				args[i], valid = stagedSpecScalar(arg)
				if !valid {
					break
				}
			}
			if !valid {
				counts.Failures++
				t.Errorf("%s.wast:%d unsupported staged action", base, cmd.Line)
				continue
			}
			got, callErr := m.in.Invoke(cmd.Action.Field, args...)
			if cmd.Type == "assert_trap" {
				if callErr == nil {
					counts.Failures++
					t.Errorf("%s.wast:%d expected trap: %s", base, cmd.Line, cmd.Text)
				} else {
					counts.AssertionsPassed++
				}
				continue
			}
			if callErr != nil || len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("%s.wast:%d result=%v err=%v want=%v", base, cmd.Line, got, callErr, cmd.Expected)
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
				t.Errorf("%s.wast:%d result=%v want=%v", base, cmd.Line, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		default:
			counts.Failures++
			t.Errorf("%s.wast:%d unhandled command %q", base, cmd.Line, cmd.Type)
		}
	}
	return counts, gates
}

func TestStagedOfficialMemory64FamilyAccounting(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	delta := stagedMemory64Delta{Schema: 2, SuiteRevision: stagedRelease3Revision}
	totalGates := map[string]int{}
	for _, base := range stagedMemory64OfficialFiles {
		t.Run(base, func(t *testing.T) {
			var script stagedSpecScript
			tmp := stagedOfficialTable64JSON(t, base, &script)
			counts, gates := replayStagedMemory64Script(t, base, tmp, script)
			delta.Files = append(delta.Files, stagedMemory64FileDelta{Name: "memory64/" + base, Status: "accounted", Gates: stagedMemory64GateList(gates), Counts: counts})
			for reason, count := range gates {
				totalGates[reason] += count
			}
			delta.Totals.add(counts)
		})
	}
	delta.Gates = stagedMemory64GateList(totalGates)
	if delta.Totals.UnexpectedCompileRejects != 0 || delta.Totals.UnexpectedLinkRejects != 0 || delta.Totals.Failures != 0 {
		t.Fatalf("staged memory64 accounting has hidden gaps: %+v", delta.Totals)
	}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedMemory64DeltaPath)
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
		t.Fatalf("staged memory64 accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact gates\n%s", got)
	}
	t.Logf("staged memory64 accounting: files=%d commands=%d modules=%d assertions=%d feature-rejects=%d blocked=%d invalid=%d malformed=%d unlinkable=%d uninstantiable=%d",
		len(delta.Files), delta.Totals.Commands, delta.Totals.ModulesPassed, delta.Totals.AssertionsPassed,
		delta.Totals.ExpectedFeatureRejects, delta.Totals.BlockedCommands, delta.Totals.ExpectedInvalid, delta.Totals.ExpectedMalformed,
		delta.Totals.ExpectedUnlinkable, delta.Totals.ExpectedUninstantiable)
}
