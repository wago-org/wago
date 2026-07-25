package wasmtimecorpus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type CommandCounts struct {
	Modules    int
	Assertions int
}

type commandDocument struct {
	SourceFilename string       `json:"source_filename"`
	Commands       []commandRow `json:"commands"`
}

type commandRow struct {
	Type       string         `json:"type"`
	Line       int            `json:"line"`
	Filename   string         `json:"filename"`
	Name       string         `json:"name"`
	As         string         `json:"as"`
	Action     commandAction  `json:"action"`
	Expected   []commandValue `json:"expected"`
	Either     []commandValue `json:"either"`
	Text       string         `json:"text"`
	ModuleType string         `json:"module_type"`
}

type commandAction struct {
	Type   string         `json:"type"`
	Module string         `json:"module"`
	Field  string         `json:"field"`
	Args   []commandValue `json:"args"`
}

type commandValue struct {
	Type     string          `json:"type"`
	LaneType string          `json:"lane_type"`
	Value    json.RawMessage `json:"value"`
}

// ValidateWASTJSONFixture strictly validates commands.json and its exact graph
// of commands.*.wasm artifacts. Unknown fields, unsafe names, duplicate or
// orphan modules, and malformed command shapes are rejected.
func ValidateWASTJSONFixture(dir string) (CommandCounts, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "commands.json"))
	if err != nil {
		return CommandCounts{}, err
	}
	var doc commandDocument
	if err := decodeStrictJSON(raw, &doc); err != nil {
		return CommandCounts{}, fmt.Errorf("decode commands.json strictly: %w", err)
	}
	if err := ValidateRelativePath(doc.SourceFilename); err != nil || !strings.HasSuffix(doc.SourceFilename, "/source.wast") {
		return CommandCounts{}, fmt.Errorf("unsafe source_filename %q", doc.SourceFilename)
	}
	if len(doc.Commands) == 0 {
		return CommandCounts{}, fmt.Errorf("commands.json has no commands")
	}
	var counts CommandCounts
	referenced := map[string]bool{}
	for i, command := range doc.Commands {
		if command.Line <= 0 {
			return CommandCounts{}, fmt.Errorf("command %d has invalid line %d", i, command.Line)
		}
		hasAction := command.Action.Type != "" || command.Action.Module != "" || command.Action.Field != "" || command.Action.Args != nil
		switch command.Type {
		case "module":
			if err := validateModuleFilename(command.Filename); err != nil {
				return CommandCounts{}, err
			}
			if referenced[command.Filename] {
				return CommandCounts{}, fmt.Errorf("duplicate module artifact %q", command.Filename)
			}
			referenced[command.Filename] = true
			counts.Modules++
			if hasAction || command.As != "" || command.Text != "" || command.ModuleType != "" || command.Expected != nil || command.Either != nil {
				return CommandCounts{}, fmt.Errorf("module command at line %d has unrelated fields", command.Line)
			}
		case "register":
			if command.As == "" || command.Filename != "" || hasAction || command.Text != "" || command.ModuleType != "" || command.Expected != nil || command.Either != nil {
				return CommandCounts{}, fmt.Errorf("register command at line %d has invalid shape", command.Line)
			}
		case "action", "assert_return", "assert_trap", "assert_exhaustion":
			if command.Filename != "" || command.As != "" || command.Name != "" || command.ModuleType != "" || !hasAction || command.Action.Type != "invoke" && command.Action.Type != "get" {
				return CommandCounts{}, fmt.Errorf("%s command at line %d has invalid action shape", command.Type, command.Line)
			}
			if (command.Type == "assert_trap" || command.Type == "assert_exhaustion") && command.Text == "" {
				return CommandCounts{}, fmt.Errorf("%s command at line %d has no trap text", command.Type, command.Line)
			}
			counts.Assertions++
		case "assert_uninstantiable", "assert_unlinkable":
			if err := validateModuleFilename(command.Filename); err != nil {
				return CommandCounts{}, err
			}
			if referenced[command.Filename] {
				return CommandCounts{}, fmt.Errorf("duplicate module artifact %q", command.Filename)
			}
			referenced[command.Filename] = true
			if command.ModuleType != "binary" || command.Text == "" || command.Name != "" || command.As != "" || hasAction || command.Expected != nil || command.Either != nil {
				return CommandCounts{}, fmt.Errorf("%s command at line %d has invalid module assertion shape", command.Type, command.Line)
			}
			counts.Assertions++
		default:
			return CommandCounts{}, fmt.Errorf("unknown command type %q at line %d", command.Type, command.Line)
		}
	}
	matches, err := filepath.Glob(filepath.Join(dir, "commands.*.wasm"))
	if err != nil {
		return CommandCounts{}, err
	}
	artifacts := map[string]bool{}
	for _, match := range matches {
		artifacts[filepath.Base(match)] = true
	}
	for name := range referenced {
		if !artifacts[name] {
			return CommandCounts{}, fmt.Errorf("commands.json references missing artifact %q", name)
		}
	}
	for name := range artifacts {
		if !referenced[name] {
			return CommandCounts{}, fmt.Errorf("orphan artifact %q", name)
		}
	}
	return counts, nil
}

func validateModuleFilename(name string) error {
	middle, ok := strings.CutPrefix(name, "commands.")
	if !ok || !strings.HasSuffix(middle, ".wasm") || strings.ContainsAny(middle, `/\\`) {
		return fmt.Errorf("invalid module filename %q", name)
	}
	indexText := strings.TrimSuffix(middle, ".wasm")
	index, err := strconv.ParseUint(indexText, 10, 32)
	if err != nil || strconv.FormatUint(index, 10) != indexText {
		return fmt.Errorf("non-canonical module filename %q", name)
	}
	return nil
}
