package plugin

import (
	"sort"
	"strings"

	"github.com/wago-org/wago"
)

type Report struct {
	Name                 string   `json:"plugin"`
	wago.ExtensionInfo            // flattened plugin metadata
	Capabilities         []string `json:"capabilities,omitempty"`
	RequiresCapabilities []string `json:"requiresCapabilities,omitempty"`
	Imports              []Import `json:"imports,omitempty"`
}

type Import struct {
	Module     string   `json:"module"`
	Name       string   `json:"name"`
	Params     []string `json:"params,omitempty"`
	Results    []string `json:"results,omitempty"`
	Capability string   `json:"capability,omitempty"`
	Docs       string   `json:"docs,omitempty"`
}

func BuildReport(name string, extension wago.Extension) Report {
	result := Report{Name: name, ExtensionInfo: extension.Info()}
	for _, capability := range extension.Info().RequiresCapabilities {
		result.RequiresCapabilities = append(result.RequiresCapabilities, string(capability))
	}
	sort.Strings(result.RequiresCapabilities)
	rt := wago.NewRuntime()
	if err := rt.Use(extension); err != nil {
		return result
	}
	defer rt.Close()
	for _, capability := range rt.Capabilities() {
		result.Capabilities = append(result.Capabilities, string(capability))
	}
	for _, spec := range rt.ProvidedImports() {
		result.Imports = append(result.Imports, Import{
			Module: spec.Module, Name: spec.Name,
			Params: valueTypes(spec.Params), Results: valueTypes(spec.Results),
			Capability: capability(spec), Docs: spec.Docs,
		})
	}
	return result
}

func CompatibilityDetail(c wago.Compatibility) string {
	var parts []string
	if len(c.Engines) > 0 {
		parts = append(parts, "engines: "+strings.Join(engineTerms(c.Engines), ", "))
	}
	if len(c.Platforms) > 0 {
		parts = append(parts, "platforms: "+strings.Join(c.Platforms, ", "))
	}
	return strings.Join(parts, " · ")
}

func Signature(params, results []string) string {
	signature := "(" + strings.Join(params, ", ") + ")"
	if len(results) == 0 {
		return signature
	}
	return signature + " -> " + strings.Join(results, ", ")
}

func engineTerms(engines map[string]string) []string {
	names := make([]string, 0, len(engines))
	for name := range engines {
		names = append(names, name)
	}
	sort.Strings(names)
	terms := make([]string, len(names))
	for i, name := range names {
		if constraint := engines[name]; constraint != "" && constraint != "*" {
			terms[i] = name + " " + constraint
		} else {
			terms[i] = name
		}
	}
	return terms
}

func valueTypes(types []wago.ValType) []string {
	if len(types) == 0 {
		return nil
	}
	values := make([]string, len(types))
	for i, valueType := range types {
		values[i] = valueType.String()
	}
	return values
}

func capability(spec wago.ImportSpec) string {
	if spec.HasCapability {
		return string(spec.Capability)
	}
	return ""
}
