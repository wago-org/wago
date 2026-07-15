//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/spectest"
)

const stagedTable64DeltaPath = "tests/spec-v3-staged-table64.json"

var stagedTable64OfficialFiles = []string{
	"table64", "table_copy64", "table_copy_mixed", "table_fill64", "table_get64",
	"table_grow64", "table_init64", "table_set64", "table_size64",
}

type stagedTable64Delta struct {
	Schema        int                   `json:"schema"`
	SuiteRevision string                `json:"suite_revision"`
	Files         []stagedSpecFileDelta `json:"files"`
	Totals        stagedSpecCounts      `json:"totals"`
}

func stagedTable64FirstLine(out []byte) string {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return "(no output)"
	}
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}
	return text
}

func resolveStagedTable64Interpreter() (string, error) {
	path := os.Getenv("WAGO_SPEC_INTERPRETER")
	if path == "" {
		return "", fmt.Errorf("WAGO_SPEC_INTERPRETER must name the pinned official Release 3 interpreter")
	}
	if want := os.Getenv("WAGO_SPEC_INTERPRETER_REVISION"); want != stagedRelease3Revision {
		return "", fmt.Errorf("configured spec interpreter revision = %q, want %q", want, stagedRelease3Revision)
	}
	stamp, err := os.ReadFile(filepath.Join(filepath.Dir(path), "source-revision"))
	if err != nil {
		return "", fmt.Errorf("configured spec interpreter lacks source revision stamp: %w", err)
	}
	if got := strings.TrimSpace(string(stamp)); got != stagedRelease3Revision {
		return "", fmt.Errorf("configured spec interpreter source revision = %q, want %q", got, stagedRelease3Revision)
	}
	out, err := exec.Command(path, "-v", "--help").CombinedOutput()
	if err != nil || stagedTable64FirstLine(out) != "wasm 3.0.0 reference interpreter" {
		return "", fmt.Errorf("configured spec interpreter identity check failed: %v: %s", err, stagedTable64FirstLine(out))
	}
	return path, nil
}

func compileStagedTable64Official(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	features.MultiMemory = true // admits Core 3 compact-import source syntax only in this supplementary runner
	return compileWithFrontendFeatures(cfg, data, features)
}

func stagedOfficialTable64JSON(t *testing.T, base string, dst any) string {
	t.Helper()
	checkout := filepath.Clean("../../tests/spec-v3")
	suite, err := spectest.DiscoverRelease3(checkout)
	if err != nil {
		t.Fatalf("discover pinned Release 3 suite: %v", err)
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	out, err := exec.Command(wast2json, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "1.0.41" {
		t.Fatalf("wast2json version = %q, %v; want pinned 1.0.41", strings.TrimSpace(string(out)), err)
	}
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, base+".json")
	wast := filepath.Join(suite.CoreDir, "memory64", base+".wast")
	if out, err := exec.Command(wast2json, "--enable-all", wast, "-o", jsonPath).CombinedOutput(); err != nil {
		interpreter, resolveErr := resolveStagedTable64Interpreter()
		if resolveErr != nil {
			t.Fatalf("convert pinned %s.wast with WABT (%v: %s), interpreter unavailable: %v", base, err, stagedTable64FirstLine(out), resolveErr)
		}
		binaryScript := filepath.Join(tmp, base+".bin.wast")
		if interpOut, interpErr := exec.Command(interpreter, "-d", wast, "-o", binaryScript).CombinedOutput(); interpErr != nil {
			t.Fatalf("convert pinned %s.wast: WABT %v: %s; interpreter %v: %s", base, err, stagedTable64FirstLine(out), interpErr, stagedTable64FirstLine(interpOut))
		}
		converter, resolveErr := resolveRepoPath("scripts/spec-interpreter-json.py")
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if convertOut, convertErr := exec.Command(converter, binaryScript, jsonPath).CombinedOutput(); convertErr != nil {
			t.Fatalf("convert pinned %s binary script: %v: %s", base, convertErr, stagedTable64FirstLine(convertOut))
		}
		t.Logf("%s: text oracle fallback=WebAssembly/spec interpreter", base)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("decode %s JSON: %v", base, err)
	}
	return tmp
}

func stagedTable64KnownGate(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	for _, gate := range []string{
		"table64 requires an explicit bounded maximum",
		"table64 maximum",
		"requires an explicit maximum no greater than 16384 entries",
		"requires exactly one local table",
		"requires exactly one local funcref table",
		"rejects element segments and table initializer expressions",
		"outside staged get/set/grow/size family",
		"64-bit table imports remain outside the staged table64 boundary",
	} {
		if bytes.Contains([]byte(text), []byte(gate)) {
			return true
		}
	}
	return false
}

func replayStagedTable64Script(t *testing.T, base, tmp string, script stagedSpecScript) (counts stagedSpecCounts) {
	t.Helper()
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
		c, err := compileStagedTable64Official(data)
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
				if stagedTable64KnownGate(err) {
					counts.ExpectedFeatureRejects++
					current = stagedSpecModule{}
					continue
				}
				if bytes.Contains([]byte(err.Error()), []byte("compile:")) {
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
				if stagedTable64KnownGate(err) {
					counts.ExpectedFeatureRejects++
					current = stagedSpecModule{}
					continue
				}
				if bytes.Contains([]byte(err.Error()), []byte("compile:")) {
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
			if c, err := compileStagedTable64Official(data); err == nil {
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
	return counts
}

func TestStagedOfficialTable64FamilyAccounting(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	delta := stagedTable64Delta{Schema: 1, SuiteRevision: stagedRelease3Revision}
	for _, base := range stagedTable64OfficialFiles {
		t.Run(base, func(t *testing.T) {
			var script stagedSpecScript
			tmp := stagedOfficialTable64JSON(t, base, &script)
			counts := replayStagedTable64Script(t, base, tmp, script)
			delta.Files = append(delta.Files, stagedSpecFileDelta{Name: "memory64/" + base, Status: "accounted", Counts: counts})
			delta.Totals.add(counts)
		})
	}
	if delta.Totals.UnexpectedCompileRejects != 0 || delta.Totals.UnexpectedLinkRejects != 0 || delta.Totals.Failures != 0 {
		t.Fatalf("staged table64 accounting has hidden gaps: %+v", delta.Totals)
	}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedTable64DeltaPath)
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
		t.Fatalf("staged table64 accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact gates\n%s", got)
	}
	t.Logf("staged table64 accounting: files=%d commands=%d modules=%d assertions=%d feature-rejects=%d blocked=%d invalid=%d malformed=%d",
		len(delta.Files), delta.Totals.Commands, delta.Totals.ModulesPassed, delta.Totals.AssertionsPassed,
		delta.Totals.ExpectedFeatureRejects, delta.Totals.BlockedCommands, delta.Totals.ExpectedInvalid, delta.Totals.ExpectedMalformed)
}
