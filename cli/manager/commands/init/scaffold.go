package initcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/wago-org/wago"
)

func writePluginScaffold(dir string, pkg map[string]any) error {
	module, _ := pkg["module"].(string)
	name, _ := pkg["name"].(string)
	description, _ := pkg["description"].(string)
	version, _ := pkg["version"].(string)
	stability, _ := pkg["stability"].(string)
	license, _ := pkg["license"].(string)
	repository, _ := pkg["repository"].(string)
	homepage, _ := pkg["homepage"].(string)
	engineValues := stringMap(pkg["engines"])
	platformValues := stringSlice(pkg["platforms"], false)
	authorValues := stringSlice(pkg["authors"], true)
	engines := goStringMapLiteral(pkg["engines"])
	platforms := goStringSliceLiteral(pkg["platforms"], false)
	authors := goAuthorNamesLiteral(pkg["authors"])
	if version == "" {
		version = "0.1.0"
	}
	registerDir := filepath.Join(dir, "register")
	if err := os.MkdirAll(registerDir, 0o755); err != nil {
		return err
	}
	sourcePath := filepath.Join(registerDir, "register.go")
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		source := fmt.Sprintf(`// Package register exposes this module's explicit Wago provider catalog.
package register

import wago "github.com/wago-org/wago"

type plugin struct{}

func (plugin) Register(*wago.Registrar) error {
	// Add declarative registrations here. Privileged registrar interfaces require
	// a matching publisher-authored authority request and consumer-reviewed grant.
	return nil
}

var definition = wago.PluginDefinition{
	ID: %s,
	Name: %s,
	Version: %s,
	Description: %s,
	Stability: wago.Stability(%s),
	Compatibility: wago.Compatibility{Engines: %s, Platforms: %s},
	Provenance: wago.PluginProvenance{Repository: %s, Homepage: %s, License: %s, Authors: %s},
}

// Providers returns an ordinary catalog value. It performs no process-global registration.
func Providers() []wago.PluginProvider {
	return []wago.PluginProvider{{Definition: definition, New: func() wago.Plugin { return plugin{} }}}
}
`, strconv.Quote(module), strconv.Quote(name), strconv.Quote(version), strconv.Quote(description), strconv.Quote(stability), engines, platforms, strconv.Quote(repository), strconv.Quote(homepage), strconv.Quote(license), authors)
		if err := writeNewRegularFile(sourcePath, []byte(source)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	testPath := filepath.Join(registerDir, "register_test.go")
	if _, err := os.Stat(testPath); os.IsNotExist(err) {
		testSource := fmt.Sprintf(`package register

import (
	"bytes"
	"os"
	"testing"

	wago "github.com/wago-org/wago"
)

func TestProviderCatalog(t *testing.T) {
	providers := Providers()
	if len(providers) != 1 || providers[0].Definition.ID != %s || providers[0].New == nil || providers[0].New() == nil {
		t.Fatalf("Providers() = %%#v", providers)
	}
	want, err := wago.EncodeProviderCatalog(%s, providers)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../wago.providers.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("wago.providers.json is stale; run: wago plugin catalog")
	}
}
`, strconv.Quote(module), strconv.Quote(module+"/register"))
		if err := writeNewRegularFile(testPath, []byte(testSource)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	goModPath := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		goMod := "module " + module + "\n\ngo 1.22\n\nrequire github.com/wago-org/wago v0.1.0\n"
		if err := writeNewRegularFile(goModPath, []byte(goMod)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	catalogPath := filepath.Join(dir, wago.ProviderCatalogFile)
	if _, err := os.Stat(catalogPath); os.IsNotExist(err) {
		definition := wago.PluginDefinition{
			ID: module, Name: name, Version: version, Description: description, Stability: wago.Stability(stability),
			Compatibility: wago.Compatibility{Engines: engineValues, Platforms: platformValues},
			Provenance:    wago.PluginProvenance{Repository: repository, Homepage: homepage, License: license, Authors: authorValues},
		}
		catalog, err := wago.EncodeProviderCatalog(module+"/register", []wago.PluginProvider{{
			Definition: definition,
			New:        func() wago.Plugin { return nil },
		}})
		if err != nil {
			return fmt.Errorf("build provider catalog: %w", err)
		}
		if err := writeNewRegularFile(catalogPath, catalog); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}

func writeNewRegularFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func stringMap(raw any) map[string]string {
	values, _ := raw.(map[string]any)
	result := make(map[string]string, len(values))
	for key, rawValue := range values {
		result[key], _ = rawValue.(string)
	}
	return result
}

func stringSlice(raw any, authors bool) []string {
	values := make([]string, 0)
	switch list := raw.(type) {
	case []string:
		values = append(values, list...)
	case []any:
		for _, rawValue := range list {
			if authors {
				author, _ := rawValue.(map[string]any)
				value, _ := author["name"].(string)
				values = append(values, value)
				continue
			}
			value, _ := rawValue.(string)
			values = append(values, value)
		}
	}
	return values
}

func goStringMapLiteral(raw any) string {
	values, _ := raw.(map[string]any)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var literal strings.Builder
	literal.WriteString("map[string]string{")
	for index, key := range keys {
		if index > 0 {
			literal.WriteString(", ")
		}
		value, _ := values[key].(string)
		literal.WriteString(strconv.Quote(key))
		literal.WriteString(": ")
		literal.WriteString(strconv.Quote(value))
	}
	literal.WriteString("}")
	return literal.String()
}

func goStringSliceLiteral(raw any, authors bool) string {
	values := stringSlice(raw, authors)
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = strconv.Quote(value)
	}
	return "[]string{" + strings.Join(parts, ", ") + "}"
}

func goAuthorNamesLiteral(raw any) string { return goStringSliceLiteral(raw, true) }
