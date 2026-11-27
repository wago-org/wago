// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

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
