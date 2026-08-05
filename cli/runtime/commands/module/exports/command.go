// Package exports defines wago module exports.
package exports

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	runtimemodule "github.com/wago-org/wago/cli/runtime/internal/module"
)

// Report is one exported WebAssembly value. Kind-specific fields are omitted
// when they do not apply; pointer fields keep meaningful zero and false values
// visible in JSON.
type Report struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Index     int      `json:"index"`
	Params    []string `json:"params,omitempty"`
	Results   []string `json:"results,omitempty"`
	Type      string   `json:"type,omitempty"`
	Mutable   *bool    `json:"mutable,omitempty"`
	Min       *uint64  `json:"min,omitempty"`
	Max       *uint64  `json:"max,omitempty"`
	Addr64    *bool    `json:"addr64,omitempty"`
	Shared    *bool    `json:"shared,omitempty"`
	TypeIndex *uint32  `json:"typeIndex,omitempty"`
}

func Command() *command.Cmd {
	return &command.Cmd{
		Name: "exports", Summary: "list a module's exports and types", Args: "<file>",
		Automation: command.JSONOutput,
		Run:        run,
	}
}

func run(c *command.Ctx) {
	rt, mod := runtimemodule.Compile(c.One("<file>"))
	defer rt.Close()
	defer mod.Close()
	reports := BuildReport(mod.Metadata())
	if automation.JSON() {
		ui.PrintJSON(reports)
		return
	}
	if len(reports) == 0 {
		fmt.Println(ui.Dim("module has no exports"))
		return
	}
	fmt.Printf("%s\n", ui.Bold("exports:"))
	for _, report := range reports {
		line := fmt.Sprintf("  %s  %s", report.Name, ui.Dim(report.Kind))
		if detail := detail(report); detail != "" {
			line += "  " + ui.Dim(detail)
		}
		fmt.Println(line)
	}
}

// BuildReport converts structural module metadata into a stable name-sorted
// export list shared by human and JSON output.
func BuildReport(metadata wago.ModuleMetadata) []Report {
	var reports []Report
	for _, function := range metadata.Functions {
		for _, name := range function.Exports {
			reports = append(reports, Report{
				Name: name, Kind: "func", Index: function.Index,
				Params: valueTypes(function.Params), Results: valueTypes(function.Results),
			})
		}
	}
	for _, global := range metadata.Globals {
		for _, name := range global.Exports {
			mutable := global.Mutable
			reports = append(reports, Report{Name: name, Kind: "global", Index: global.Index, Type: global.Type.String(), Mutable: &mutable})
		}
	}
	for _, table := range metadata.Tables {
		for _, name := range table.Exports {
			min, addr64 := table.Min, table.Addr64
			report := Report{Name: name, Kind: "table", Index: table.Index, Type: table.Type.String(), Min: &min, Addr64: &addr64}
			if table.HasMax {
				max := table.Max
				report.Max = &max
			}
			reports = append(reports, report)
		}
	}
	for _, memory := range metadata.Memories {
		for _, name := range memory.Exports {
			min, addr64, shared := memory.Min, memory.Addr64, memory.Shared
			report := Report{Name: name, Kind: "memory", Index: memory.Index, Min: &min, Addr64: &addr64, Shared: &shared}
			if memory.HasMax {
				max := memory.Max
				report.Max = &max
			}
			reports = append(reports, report)
		}
	}
	for _, tag := range metadata.Tags {
		for _, name := range tag.Exports {
			typeIndex := tag.TypeIndex
			reports = append(reports, Report{Name: name, Kind: "tag", Index: tag.Index, Params: valueTypes(tag.Params), TypeIndex: &typeIndex})
		}
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].Name < reports[j].Name })
	return reports
}

func detail(report Report) string {
	switch report.Kind {
	case "func":
		return signature(report.Params, report.Results)
	case "global":
		state := "immutable"
		if report.Mutable != nil && *report.Mutable {
			state = "mutable"
		}
		return report.Type + " " + state
	case "table":
		return limits(report.Type, report.Min, report.Max, report.Addr64, nil, "table64")
	case "memory":
		return limits("", report.Min, report.Max, report.Addr64, report.Shared, "memory64")
	case "tag":
		return signature(report.Params, nil)
	default:
		return ""
	}
}

func limits(valueType string, min, max *uint64, addr64, shared *bool, addr64Name string) string {
	parts := make([]string, 0, 5)
	if valueType != "" {
		parts = append(parts, valueType)
	}
	if min != nil {
		parts = append(parts, fmt.Sprintf("min=%d", *min))
	}
	if max != nil {
		parts = append(parts, fmt.Sprintf("max=%d", *max))
	}
	if addr64 != nil && *addr64 {
		parts = append(parts, addr64Name)
	}
	if shared != nil && *shared {
		parts = append(parts, "shared")
	}
	return strings.Join(parts, " ")
}

func signature(params, results []string) string {
	result := "(" + strings.Join(params, ", ") + ")"
	if len(results) != 0 {
		result += " -> " + strings.Join(results, ", ")
	}
	return result
}

func valueTypes(types []wago.ValType) []string {
	if len(types) == 0 {
		return nil
	}
	values := make([]string, len(types))
	for index, valueType := range types {
		values[index] = valueType.String()
	}
	return values
}
