package wago

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
)

const (
	// ProviderCatalogSchemaURI identifies the strict provider artifact format.
	ProviderCatalogSchemaURI = "https://wago.sh/v1/providers.schema.json"
	// ProviderCatalogFile is the module-root provider artifact filename.
	ProviderCatalogFile = "wago.providers.json"

	maxProviderCatalogEntries = 128
)

// ProviderCatalogEntry is one immutable provider definition in a source-module
// artifact. ImportPath names the source module's explicit register package.
type ProviderCatalogEntry struct {
	ImportPath       string           `json:"importPath"`
	Definition       PluginDefinition `json:"definition"`
	DefinitionDigest string           `json:"definitionDigest"`
}

// ProviderCatalogDocument is the canonical module-root provider artifact.
type ProviderCatalogDocument struct {
	Schema    string                 `json:"$schema"`
	Providers []ProviderCatalogEntry `json:"providers"`
}

// CanonicalPluginDefinition validates and returns an independent canonical copy
// of def. Set-like fields are sorted and embedded configuration schema JSON is
// normalized exactly as it is for DefinitionDigest.
func CanonicalPluginDefinition(def PluginDefinition) (PluginDefinition, error) {
	return canonicalPluginDefinition(def)
}

// EncodeProviderCatalog returns deterministic, indented artifact JSON ending in
// one newline. The catalog owns one source module: it must include the provider
// whose ID is that module, while any additional provider IDs are child paths.
func EncodeProviderCatalog(importPath string, providers []PluginProvider) ([]byte, error) {
	if len(providers) == 0 || len(providers) > maxProviderCatalogEntries {
		return nil, fmt.Errorf("wago: provider catalog must contain 1 to %d providers", maxProviderCatalogEntries)
	}
	entries := make([]ProviderCatalogEntry, 0, len(providers))
	seen := make(map[string]struct{}, len(providers))
	for index, provider := range providers {
		if provider.New == nil {
			return nil, fmt.Errorf("wago: provider catalog provider %d has no factory", index)
		}
		definition, err := canonicalPluginDefinition(provider.Definition)
		if err != nil {
			return nil, fmt.Errorf("wago: provider catalog provider %d: %w", index, err)
		}
		if _, duplicate := seen[definition.ID]; duplicate {
			return nil, fmt.Errorf("wago: provider catalog has duplicate provider %q", definition.ID)
		}
		seen[definition.ID] = struct{}{}
		digest, err := definitionDigestCanonical(definition)
		if err != nil {
			return nil, fmt.Errorf("wago: provider catalog provider %q: %w", definition.ID, err)
		}
		entries = append(entries, ProviderCatalogEntry{
			ImportPath: importPath, Definition: definition, DefinitionDigest: digest,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Definition.ID < entries[j].Definition.ID
	})
	if _, err := providerCatalogSource(entries); err != nil {
		return nil, err
	}
	document := ProviderCatalogDocument{Schema: ProviderCatalogSchemaURI, Providers: entries}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("wago: encode provider catalog: %w", err)
	}
	return append(encoded, '\n'), nil
}

// DecodeProviderCatalog strictly decodes and validates a canonical provider
// artifact. Unknown fields, trailing JSON, noncanonical definitions or order,
// stale digests, and mixed source ownership are rejected.
func DecodeProviderCatalog(encoded []byte) (ProviderCatalogDocument, error) {
	if len(encoded) == 0 || len(encoded) > 2<<20 {
		return ProviderCatalogDocument{}, errors.New("wago: provider catalog must contain 1 byte to 2 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var document ProviderCatalogDocument
	if err := decoder.Decode(&document); err != nil {
		return ProviderCatalogDocument{}, fmt.Errorf("wago: decode provider catalog: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return ProviderCatalogDocument{}, fmt.Errorf("wago: decode provider catalog: %w", err)
	}
	if document.Schema != ProviderCatalogSchemaURI {
		return ProviderCatalogDocument{}, fmt.Errorf("wago: provider catalog $schema must be %q", ProviderCatalogSchemaURI)
	}
	if len(document.Providers) == 0 || len(document.Providers) > maxProviderCatalogEntries {
		return ProviderCatalogDocument{}, fmt.Errorf("wago: provider catalog must contain 1 to %d providers", maxProviderCatalogEntries)
	}

	canonical := make([]ProviderCatalogEntry, len(document.Providers))
	seen := make(map[string]struct{}, len(document.Providers))
	for index, entry := range document.Providers {
		definition, err := canonicalPluginDefinition(entry.Definition)
		if err != nil {
			return ProviderCatalogDocument{}, fmt.Errorf("wago: provider catalog providers[%d]: %w", index, err)
		}
		// MarshalIndent necessarily reformats an embedded RawMessage along with the
		// surrounding document. Compare its canonical value while still requiring
		// every ordered definition collection to already be canonical.
		encodedDefinition := entry.Definition
		encodedDefinition.ConfigSchema = definition.ConfigSchema
		if !reflect.DeepEqual(encodedDefinition, definition) {
			return ProviderCatalogDocument{}, fmt.Errorf("wago: provider catalog provider %q definition is not canonical", definition.ID)
		}
		if _, duplicate := seen[definition.ID]; duplicate {
			return ProviderCatalogDocument{}, fmt.Errorf("wago: provider catalog has duplicate provider %q", definition.ID)
		}
		seen[definition.ID] = struct{}{}
		digest, err := definitionDigestCanonical(definition)
		if err != nil {
			return ProviderCatalogDocument{}, fmt.Errorf("wago: provider catalog provider %q: %w", definition.ID, err)
		}
		if entry.DefinitionDigest != digest {
			return ProviderCatalogDocument{}, fmt.Errorf("wago: provider catalog provider %q definitionDigest is %q, want %q", definition.ID, entry.DefinitionDigest, digest)
		}
		canonical[index] = ProviderCatalogEntry{
			ImportPath: entry.ImportPath, Definition: definition, DefinitionDigest: digest,
		}
	}
	for index := 1; index < len(canonical); index++ {
		if canonical[index-1].Definition.ID >= canonical[index].Definition.ID {
			return ProviderCatalogDocument{}, errors.New("wago: provider catalog providers are not in canonical ID order")
		}
	}
	if _, err := providerCatalogSource(canonical); err != nil {
		return ProviderCatalogDocument{}, err
	}
	return ProviderCatalogDocument{Schema: ProviderCatalogSchemaURI, Providers: canonical}, nil
}

// providerCatalogSource infers the owning source module from the one provider
// whose ID names the module itself. Every other provider must be a child package,
// and every entry must point at that module's single register package.
func providerCatalogSource(entries []ProviderCatalogEntry) (string, error) {
	source := ""
	for _, entry := range entries {
		if entry.ImportPath != entry.Definition.ID+"/register" {
			continue
		}
		if source != "" {
			return "", errors.New("wago: provider catalog contains more than one source-module provider")
		}
		source = entry.Definition.ID
	}
	if source == "" {
		return "", errors.New("wago: provider catalog importPath must be the source provider ID plus /register")
	}
	registerPath := source + "/register"
	for _, entry := range entries {
		if entry.ImportPath != registerPath {
			return "", fmt.Errorf("wago: provider catalog provider %q importPath must be %q", entry.Definition.ID, registerPath)
		}
		if entry.Definition.ID != source && !strings.HasPrefix(entry.Definition.ID, source+"/") {
			return "", fmt.Errorf("wago: provider catalog provider %q does not belong to source module %q", entry.Definition.ID, source)
		}
	}
	return source, nil
}
