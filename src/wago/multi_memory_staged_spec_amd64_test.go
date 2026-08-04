//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// stagedMultiMemorySpecFiles is the complete pinned Release 3 multi-memory
// family plus its main-core SIMD indexed-memory companion. Exact JSON command
// streams are replayed with only the internal compact-import + multi-memory gate.
// Every pinned consumer is executed; unexpected compile, link, or assertion gaps
// remain fatal rather than being omitted or reclassified.
type stagedSpecFile struct {
	Family string
	Name   string
}

var stagedMultiMemorySpecFiles = []stagedSpecFile{
	{Name: "address0"}, {Name: "address1"}, {Name: "align0"}, {Name: "binary0"},
	{Name: "data0"}, {Name: "data1"}, {Name: "data_drop0"}, {Name: "exports0"},
	{Name: "float_exprs0"}, {Name: "float_exprs1"}, {Name: "float_memory0"},
	{Name: "imports0"}, {Name: "imports1"}, {Name: "imports2"}, {Name: "imports3"}, {Name: "imports4"},
	{Name: "linking0"}, {Name: "linking1"}, {Name: "linking2"}, {Name: "linking3"},
	{Name: "load0"}, {Name: "load1"}, {Name: "load2"}, {Name: "memory-multi"},
	{Name: "memory_copy0"}, {Name: "memory_copy1"}, {Name: "memory_fill0"}, {Name: "memory_grow"},
	{Name: "memory_init0"}, {Name: "memory_size0"}, {Name: "memory_size1"}, {Name: "memory_size2"},
	{Name: "memory_size3"}, {Name: "memory_size_import"}, {Name: "memory_trap0"}, {Name: "memory_trap1"},
	{Name: "start0"}, {Name: "store0"}, {Name: "store1"}, {Name: "store2"}, {Name: "traps0"},
	{Family: "simd", Name: "simd_memory-multi"},
}

const (
	stagedMultiMemoryDeltaPath = "tests/spec-v3-staged-multi-memory.json"
	stagedRelease3Revision     = "9d36019973201a19f9c9ebb0f10828b2fe2374aa"
)

type stagedSpecValue struct {
	Type     string          `json:"type"`
	Value    json.RawMessage `json:"value"`
	HeapType string          `json:"heap_type,omitempty"`
}

type stagedSpecAction struct {
	Type   string            `json:"type"`
	Module string            `json:"module"`
	Field  string            `json:"field"`
	Args   []stagedSpecValue `json:"args"`
}

type stagedSpecCommand struct {
	Type     string            `json:"type"`
	Line     int               `json:"line"`
	Filename string            `json:"filename"`
	Name     string            `json:"name"`
	Module   string            `json:"module"`
	As       string            `json:"as"`
	Action   stagedSpecAction  `json:"action"`
	Expected []stagedSpecValue `json:"expected"`
	Text     string            `json:"text"`
}

type stagedSpecScript struct {
	Commands []stagedSpecCommand `json:"commands"`
}

type stagedSpecCounts struct {
	Commands                 int `json:"commands"`
	ModulesPassed            int `json:"modules_passed"`
	AssertionsPassed         int `json:"assertions_passed"`
	ExpectedInvalid          int `json:"expected_invalid"`
	ExpectedMalformed        int `json:"expected_malformed,omitempty"`
	ExpectedUnlinkable       int `json:"expected_unlinkable"`
	ExpectedUninstantiable   int `json:"expected_uninstantiable"`
	ExpectedFeatureRejects   int `json:"expected_feature_rejects"`
	BlockedCommands          int `json:"blocked_commands"`
	UnexpectedCompileRejects int `json:"unexpected_compile_rejects"`
	UnexpectedLinkRejects    int `json:"unexpected_link_rejects"`
	Failures                 int `json:"failures"`
}

func (c *stagedSpecCounts) add(other stagedSpecCounts) {
	c.Commands += other.Commands
	c.ModulesPassed += other.ModulesPassed
	c.AssertionsPassed += other.AssertionsPassed
	c.ExpectedInvalid += other.ExpectedInvalid
	c.ExpectedMalformed += other.ExpectedMalformed
	c.ExpectedUnlinkable += other.ExpectedUnlinkable
	c.ExpectedUninstantiable += other.ExpectedUninstantiable
	c.ExpectedFeatureRejects += other.ExpectedFeatureRejects
	c.BlockedCommands += other.BlockedCommands
	c.UnexpectedCompileRejects += other.UnexpectedCompileRejects
	c.UnexpectedLinkRejects += other.UnexpectedLinkRejects
	c.Failures += other.Failures
}

type stagedSpecFileDelta struct {
	Name   string           `json:"name"`
	Status string           `json:"status"`
	Gate   string           `json:"gate,omitempty"`
	Counts stagedSpecCounts `json:"counts"`
}

type stagedMultiMemoryDelta struct {
	Schema        int                   `json:"schema"`
	SuiteRevision string                `json:"suite_revision"`
	Files         []stagedSpecFileDelta `json:"files"`
	Totals        stagedSpecCounts      `json:"totals"`
}

type stagedSpecModule struct {
	in *Instance
	c  *Compiled
}

func compileStagedMultiMemory(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.MultiMemory = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func stagedSpecScalar(v stagedSpecValue) (uint64, bool) {
	var s string
	if err := json.Unmarshal(v.Value, &s); err != nil {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

func stagedSpecMatch(got uint64, want stagedSpecValue) bool {
	var s string
	if err := json.Unmarshal(want.Value, &s); err != nil {
		return false
	}
	switch s {
	case "nan:canonical":
		if want.Type == "f32" {
			return uint32(got)&0x7fffffff == 0x7fc00000
		}
		return got&0x7fffffffffffffff == 0x7ff8000000000000
	case "nan:arithmetic":
		if want.Type == "f32" {
			b := uint32(got)
			return math.IsNaN(float64(math.Float32frombits(b))) && b&0x400000 != 0
		}
		return math.IsNaN(math.Float64frombits(got)) && got&0x8000000000000 != 0
	}
	bits, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return false
	}
	if want.Type == "i32" || want.Type == "f32" {
		return uint32(got) == uint32(bits)
	}
	return got == bits
}

func stagedSpecImports(c *Compiled, registered map[string]stagedSpecModule, standard Imports) (Imports, error) {
	imports := make(Imports, len(standard))
	for key, value := range standard {
		imports[key] = value
	}
	resolve := func(key string) (stagedSpecModule, string, bool) {
		for i := 0; i < len(key); i++ {
			if key[i] == '.' {
				m, ok := registered[key[:i]]
				return m, key[i+1:], ok
			}
		}
		return stagedSpecModule{}, "", false
	}
	for _, key := range c.Imports {
		if m, field, ok := resolve(key); ok {
			ex, err := m.in.ExportedFunc(field)
			if err != nil {
				return nil, err
			}
			imports[key] = ex
		}
	}
	for _, key := range c.MemoryImports() {
		if m, field, ok := resolve(key); ok {
			memory, err := m.in.ExportedMemory(field)
			if err != nil {
				return nil, err
			}
			imports[key] = memory
		}
	}
	for _, key := range c.TableImports() {
		if m, field, ok := resolve(key); ok {
			table, err := m.in.ExportedTable(field)
			if err != nil {
				return nil, err
			}
			imports[key] = table
		}
	}
	for _, imp := range c.GlobalImports {
		key := imp.Module + "." + imp.Name
		if m, field, ok := resolve(key); ok {
			global, err := m.in.ExportedGlobalObject(field)
			if err != nil {
				return nil, err
			}
			imports[key] = global
		}
	}
	return imports, nil
}

func replayStagedMultiMemoryScript(t *testing.T, base, tmp string, script stagedSpecScript) (counts stagedSpecCounts) {
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

	instantiate := func(data []byte, cmd stagedSpecCommand) (stagedSpecModule, error) {
		c, err := compileStagedMultiMemory(data)
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
			if m.in == nil || cmd.As == "" {
				counts.Failures++
				t.Errorf("%s.wast:%d invalid register command", base, cmd.Line)
				continue
			}
			registered[cmd.As] = m
		case "assert_invalid", "assert_malformed":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read invalid module: %v", base, cmd.Line, err)
				continue
			}
			c, err := compileStagedMultiMemory(data)
			if err == nil {
				_ = c.Close()
				counts.Failures++
				t.Errorf("%s.wast:%d invalid module compiled: %s", base, cmd.Line, cmd.Text)
				continue
			}
			counts.ExpectedInvalid++
		case "assert_unlinkable", "assert_uninstantiable":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read rejected module: %v", base, cmd.Line, err)
				continue
			}
			m, err := instantiate(data, cmd)
			if err == nil {
				counts.Failures++
				t.Errorf("%s.wast:%d expected instantiation rejection: %s", base, cmd.Line, cmd.Text)
				_ = m.in.Close()
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
				counts.Failures++
				t.Errorf("%s.wast:%d action has no live module", base, cmd.Line)
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
			if !valid || cmd.Action.Type != "invoke" {
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
			if callErr != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d action trapped: %v", base, cmd.Line, callErr)
				continue
			}
			if len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("%s.wast:%d result count = %d, want %d", base, cmd.Line, len(got), len(cmd.Expected))
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
				t.Errorf("%s.wast:%d results = %v, want %v", base, cmd.Line, got, cmd.Expected)
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

func TestStagedOfficialMultiMemoryFamilyMatrix(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	files := append([]stagedSpecFile(nil), stagedMultiMemorySpecFiles...)
	sort.Slice(files, func(i, j int) bool {
		if files[i].Family != files[j].Family {
			return files[i].Family < files[j].Family
		}
		return files[i].Name < files[j].Name
	})
	delta := stagedMultiMemoryDelta{Schema: 2, SuiteRevision: stagedRelease3Revision}
	for _, file := range files {
		family := file.Family
		if family == "" {
			family = "multi-memory"
		}
		t.Run(family+"/"+file.Name, func(t *testing.T) {
			var script stagedSpecScript
			tmp := stagedOfficialCoreJSON(t, family, file.Name, &script)
			counts := replayStagedMultiMemoryScript(t, file.Name, tmp, script)
			entry := stagedSpecFileDelta{Name: family + "/" + file.Name, Status: "green", Counts: counts}
			delta.Files = append(delta.Files, entry)
			delta.Totals.add(counts)
		})
	}
	if delta.Totals.UnexpectedCompileRejects != 0 || delta.Totals.UnexpectedLinkRejects != 0 || delta.Totals.Failures != 0 {
		t.Fatalf("staged multi-memory delta has hidden gaps: %+v", delta.Totals)
	}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedMultiMemoryDeltaPath)
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
		t.Fatalf("staged multi-memory delta changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact command accounting\n%s", got)
	}
	t.Logf("staged multi-memory matrix: files=%d commands=%d modules=%d assertions=%d expected-invalid=%d expected-uninstantiable=%d expected-feature-rejects=%d blocked=%d",
		len(delta.Files), delta.Totals.Commands, delta.Totals.ModulesPassed, delta.Totals.AssertionsPassed,
		delta.Totals.ExpectedInvalid, delta.Totals.ExpectedUninstantiable, delta.Totals.ExpectedFeatureRejects, delta.Totals.BlockedCommands)
}

func resolveRepoPath(name string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(wd, filepath.FromSlash(name))
		if _, err := os.Stat(filepath.Dir(candidate)); err == nil {
			return candidate, nil
		}
		next := filepath.Dir(wd)
		if next == wd {
			return "", fmt.Errorf("repository path %q not found from %s", name, wd)
		}
		wd = next
	}
}
