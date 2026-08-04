package wasmtimecore3

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	UpstreamRevision    = "e8ac8c27f19939bfb1d26d920368d8b6028a67a9"
	InterpreterRevision = "9d36019973201a19f9c9ebb0f10828b2fe2374aa"
	WasmToolsVersion    = "1.251.0"
)

type Fixture struct{ Path, Coverage string }
type InventoryRow struct{ Path, Status, Reason string }
type RuntimeReuseRow struct {
	Path, PinnedSourceSHA256, RuntimeSourceSHA256, Relation, Reason string
}
type Provenance struct {
	UpstreamRepo        string `json:"upstream_repo"`
	Revision            string `json:"revision"`
	RevisionDate        string `json:"revision_date"`
	SourceRoot          string `json:"source_root"`
	InterpreterRepo     string `json:"interpreter_repo"`
	InterpreterRevision string `json:"interpreter_revision"`
	Converter           string `json:"converter"`
	AlternateConverter  string `json:"alternate_converter"`
	WasmToolsVersion    string `json:"wasm_tools_version"`
	FixtureTreeSHA256   string `json:"fixture_tree_sha256"`
	AdaptedTreeSHA256   string `json:"adapted_tree_sha256"`
	RuntimeReuseSHA256  string `json:"runtime_reuse_sha256"`
}
type document struct {
	Source   string    `json:"source"`
	Commands []command `json:"commands"`
}
type command struct {
	Type       string          `json:"type"`
	Filename   string          `json:"filename"`
	Name       string          `json:"name"`
	Module     string          `json:"module"`
	As         string          `json:"as"`
	Thread     string          `json:"thread"`
	Text       string          `json:"text"`
	ModuleType string          `json:"module_type"`
	Line       int             `json:"line"`
	Action     json.RawMessage `json:"action"`
	Expected   json.RawMessage `json:"expected"`
	Either     json.RawMessage `json:"either"`
	Commands   []command       `json:"commands"`
}

var inventoryStatuses = map[string]bool{
	"ported-runtime": true, "ported-core3": true, "ported-adapted": true,
	"outside-core3": true, "excluded-nonstandard": true,
}

var coverage = map[string]bool{
	"exception-handling": true, "gc": true, "memory64": true,
	"multi-memory": true, "simd": true, "table64": true,
	"typed-function-references": true,
}

func LoadManifest(path string) ([]Fixture, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Fixture
	previous := ""
	s := bufio.NewScanner(f)
	for line := 1; s.Scan(); line++ {
		text := s.Text()
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Split(text, "\t")
		if len(fields) != 2 {
			return nil, fmt.Errorf("manifest line %d: want 2 fields", line)
		}
		path := fields[0]
		if path <= previous || !strings.HasSuffix(path, ".wast") || filepath.IsAbs(path) || strings.Contains(path, "\\") || strings.Contains(path, "../") {
			return nil, fmt.Errorf("manifest line %d: unsafe or unsorted path %q", line, path)
		}
		previous = path
		last := ""
		for _, label := range strings.Split(fields[1], ",") {
			if !coverage[label] || label <= last {
				return nil, fmt.Errorf("manifest line %d: bad coverage %q", line, label)
			}
			last = label
		}
		out = append(out, Fixture{path, fields[1]})
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("manifest is empty")
	}
	return out, nil
}

func LoadInventory(path string) ([]InventoryRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []InventoryRow
	previous := ""
	s := bufio.NewScanner(f)
	for line := 1; s.Scan(); line++ {
		text := s.Text()
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Split(text, "\t")
		if len(fields) != 3 || fields[0] <= previous || !inventoryStatuses[fields[1]] || fields[2] == "" {
			return nil, fmt.Errorf("inventory line %d is malformed or unsorted", line)
		}
		previous = fields[0]
		out = append(out, InventoryRow{fields[0], fields[1], fields[2]})
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func LoadRuntimeReuse(path string) ([]RuntimeReuseRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []RuntimeReuseRow
	previous := ""
	s := bufio.NewScanner(f)
	for line := 1; s.Scan(); line++ {
		text := s.Text()
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Split(text, "\t")
		if len(fields) != 5 || fields[0] <= previous || !validSHA256(fields[1]) || !validSHA256(fields[2]) || fields[4] == "" {
			return nil, fmt.Errorf("runtime reuse line %d is malformed or unsorted", line)
		}
		if fields[3] != "byte-identical" && fields[3] != "diagnostic-text-only" {
			return nil, fmt.Errorf("runtime reuse line %d has unknown relation %q", line, fields[3])
		}
		previous = fields[0]
		out = append(out, RuntimeReuseRow{fields[0], fields[1], fields[2], fields[3], fields[4]})
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func LoadProvenance(path string) (Provenance, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Provenance{}, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var p Provenance
	if err := dec.Decode(&p); err != nil {
		return p, err
	}
	if p.Revision != UpstreamRevision || p.InterpreterRevision != InterpreterRevision || p.WasmToolsVersion != WasmToolsVersion || p.AlternateConverter == "" || !validSHA256(p.FixtureTreeSHA256) || !validSHA256(p.AdaptedTreeSHA256) || !validSHA256(p.RuntimeReuseSHA256) {
		return p, fmt.Errorf("invalid Wasmtime Core 3 provenance")
	}
	return p, nil
}

func ValidateFixture(dir string) (modules, assertions int, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, "commands.json"))
	if err != nil {
		return 0, 0, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var doc document
	if err := dec.Decode(&doc); err != nil {
		return 0, 0, fmt.Errorf("decode commands.json: %w", err)
	}
	if (doc.Source != "WebAssembly/spec interpreter 3.0.0" && doc.Source != "wasm-tools 1.251.0 json-from-wast") || len(doc.Commands) == 0 {
		return 0, 0, fmt.Errorf("invalid command document")
	}
	referenced := map[string]bool{}
	threads := map[string]bool{}
	var validateCommands func([]command) error
	validateCommands = func(commands []command) error {
		for i, cmd := range commands {
			if cmd.Line <= 0 {
				return fmt.Errorf("command %d has invalid line", i)
			}
			switch cmd.Type {
			case "module_definition", "module", "assert_invalid", "assert_malformed":
				if err := moduleFile(dir, cmd.Filename, referenced); err != nil {
					return err
				}
				if cmd.Type == "module" {
					modules++
				}
			case "module_instance":
				modules++
			case "action", "assert_return", "assert_trap", "assert_exhaustion", "assert_exception", "assert_unlinkable", "assert_uninstantiable":
				assertions++
			case "register":
			case "thread":
				if cmd.Name == "" || threads[cmd.Name] || len(cmd.Commands) == 0 {
					return fmt.Errorf("invalid thread command %q", cmd.Name)
				}
				threads[cmd.Name] = true
				if err := validateCommands(cmd.Commands); err != nil {
					return fmt.Errorf("thread %q: %w", cmd.Name, err)
				}
			case "wait":
				if cmd.Thread == "" || !threads[cmd.Thread] {
					return fmt.Errorf("wait references unknown thread %q", cmd.Thread)
				}
			default:
				return fmt.Errorf("unknown command type %q", cmd.Type)
			}
		}
		return nil
	}
	if err := validateCommands(doc.Commands); err != nil {
		return 0, 0, err
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "commands.*.wasm"))
	if len(matches) != len(referenced) {
		return 0, 0, fmt.Errorf("artifact count %d != referenced %d", len(matches), len(referenced))
	}
	return modules, assertions, nil
}

func moduleFile(dir, name string, seen map[string]bool) error {
	if !strings.HasPrefix(name, "commands.") || !strings.HasSuffix(name, ".wasm") || strings.ContainsAny(name, `/\\`) || seen[name] {
		return fmt.Errorf("invalid or duplicate module artifact %q", name)
	}
	seen[name] = true
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		return err
	}
	return nil
}

func FileSHA256(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func validSHA256(text string) bool {
	if len(text) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(text)
	return err == nil
}

func TreeSHA256(root string) (string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			rel, _ := filepath.Rel(root, path)
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(raw))
		h.Write(raw)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
