// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone || !wago_standalone_no_plugins

package wago

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/wago-org/wago/internal/jsonstrict"
	"github.com/wago-org/wago/internal/namecheck"
	"github.com/wago-org/wago/src/core/semver"
)

type pluginRawMessage = json.RawMessage

// DefinitionDigest returns the lowercase SHA-256 of the canonical JSON form of
// a valid definition. Set-like fields are sorted and embedded schema JSON is
// recursively canonicalized, so producer ordering cannot change the digest.
func DefinitionDigest(def PluginDefinition) (string, error) {
	canonical, err := canonicalPluginDefinition(def)
	if err != nil {
		return "", err
	}
	return definitionDigestCanonical(canonical)
}

// definitionDigestCanonical hashes a definition already validated and frozen by
// canonicalPluginDefinition. Keeping this separate lets plan validation avoid
// validating and cloning the same immutable definition a second time.
func definitionDigestCanonical(canonical PluginDefinition) (string, error) {
	b, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("wago: marshal canonical plugin definition: %w", err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func canonicalPluginDefinition(def PluginDefinition) (PluginDefinition, error) {
	if err := validatePluginDefinition(def); err != nil {
		return PluginDefinition{}, err
	}
	def.Compatibility.Engines = cloneStringMap(def.Compatibility.Engines)
	def.Compatibility.Platforms = sortedStrings(def.Compatibility.Platforms)
	def.Provenance.Authors = sortedStrings(def.Provenance.Authors)
	def.Requires = append([]PluginRequirement(nil), def.Requires...)
	sort.Slice(def.Requires, func(i, j int) bool {
		if def.Requires[i].ID == def.Requires[j].ID {
			return def.Requires[i].Version < def.Requires[j].Version
		}
		return def.Requires[i].ID < def.Requires[j].ID
	})
	def.Authorities = append([]AuthorityRequest(nil), def.Authorities...)
	for i := range def.Authorities {
		def.Authorities[i].Scope.Modules = sortedStrings(def.Authorities[i].Scope.Modules)
	}
	sort.Slice(def.Authorities, func(i, j int) bool { return def.Authorities[i].Name < def.Authorities[j].Name })
	def.Provides = append([]ContractSpec(nil), def.Provides...)
	sort.Slice(def.Provides, func(i, j int) bool {
		if def.Provides[i].ID == def.Provides[j].ID {
			return def.Provides[i].Major < def.Provides[j].Major
		}
		return def.Provides[i].ID < def.Provides[j].ID
	})
	def.Consumes = append([]ContractRequirement(nil), def.Consumes...)
	sort.Slice(def.Consumes, func(i, j int) bool {
		if def.Consumes[i].ID != def.Consumes[j].ID {
			return def.Consumes[i].ID < def.Consumes[j].ID
		}
		if def.Consumes[i].Major != def.Consumes[j].Major {
			return def.Consumes[i].Major < def.Consumes[j].Major
		}
		return def.Consumes[i].Mode < def.Consumes[j].Mode
	})
	if len(def.ConfigSchema) != 0 {
		b, err := canonicalJSON(def.ConfigSchema)
		if err != nil {
			return PluginDefinition{}, fmt.Errorf("wago: plugin %q configSchema: %w", def.ID, err)
		}
		def.ConfigSchema = b
	}
	return def, nil
}

func canonicalJSON(raw []byte) ([]byte, error) {
	if err := jsonstrict.ValidateUniqueJSON(raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid trailing JSON")
	}
	return json.Marshal(value)
}

func validatePluginDefinition(def PluginDefinition) error {
	if len(def.ID) > 300 || len(def.Name) > 100 || len(def.Description) > 500 || len(def.ConfigSchema) > 256<<10 {
		return fmt.Errorf("plugin definition exceeds metadata limits")
	}
	if len(def.Requires) > 128 || len(def.Provides) > 128 || len(def.Consumes) > 128 || len(def.Authorities) > 64 || len(def.Provenance.Authors) > 64 || len(def.Compatibility.Platforms) > 128 || len(def.Compatibility.Engines) > 64 {
		return fmt.Errorf("plugin definition exceeds collection limits")
	}
	if !validCanonicalPath(def.ID) {
		return fmt.Errorf("plugin ID %q must be a canonical Go module or package path", def.ID)
	}
	if strings.HasPrefix(def.Version, "V") {
		return fmt.Errorf("plugin %q version %q must not use an uppercase V prefix", def.ID, def.Version)
	}
	if _, err := semver.Parse(def.Version); err != nil {
		return fmt.Errorf("plugin %q version %q: %w", def.ID, def.Version, err)
	}
	if err := validateStability(def.Stability); err != nil {
		return err
	}
	if err := validateCompatibility(def.Compatibility); err != nil {
		return err
	}
	if err := validateOpenSource(def); err != nil {
		return err
	}
	seenPlatforms := map[string]struct{}{}
	for _, platform := range def.Compatibility.Platforms {
		parts := strings.Split(platform, "/")
		if len(parts) != 2 || !validPlatformPart(parts[0]) || !validPlatformPart(parts[1]) {
			return fmt.Errorf("compatibility platform %q must be exact GOOS/GOARCH", platform)
		}
		if err := uniqueString(seenPlatforms, platform, "compatibility platform"); err != nil {
			return err
		}
	}
	seenAuthors := map[string]struct{}{}
	for _, author := range def.Provenance.Authors {
		if len(author) > 200 || strings.TrimSpace(author) != author {
			return fmt.Errorf("invalid author %q", author)
		}
		if err := uniqueString(seenAuthors, author, "author"); err != nil {
			return err
		}
	}
	seenRequires := map[string]struct{}{}
	for _, requirement := range def.Requires {
		if !validCanonicalPath(requirement.ID) || requirement.ID == def.ID {
			return fmt.Errorf("invalid plugin requirement %q", requirement.ID)
		}
		if err := uniqueString(seenRequires, requirement.ID, "plugin requirement"); err != nil {
			return err
		}
		if strings.TrimSpace(requirement.Version) == "" {
			return fmt.Errorf("plugin requirement %q has an empty version constraint", requirement.ID)
		}
		if _, err := semver.ParseConstraint(requirement.Version); err != nil {
			return fmt.Errorf("plugin requirement %q: %w", requirement.ID, err)
		}
	}
	seenAuthorities := map[string]struct{}{}
	for _, request := range def.Authorities {
		if !validAuthority(request.Name) {
			return fmt.Errorf("unknown authority %q", request.Name)
		}
		if request.Mode != AuthorityRequired && request.Mode != AuthorityOptional {
			return fmt.Errorf("authority %q has invalid mode %q", request.Name, request.Mode)
		}
		if strings.TrimSpace(request.Reason) == "" {
			return fmt.Errorf("authority %q has no reason", request.Name)
		}
		if len(request.Reason) > 500 {
			return fmt.Errorf("authority %q reason exceeds 500 bytes", request.Name)
		}
		if err := uniqueString(seenAuthorities, string(request.Name), "authority"); err != nil {
			return err
		}
		if err := validateAuthorityScope(request.Name, request.Scope); err != nil {
			return err
		}
	}
	seenProvides := map[string]struct{}{}
	for _, spec := range def.Provides {
		if err := validateContractSpec(spec); err != nil {
			return err
		}
		if err := uniqueString(seenProvides, contractKeyString(spec), "provided contract"); err != nil {
			return err
		}
	}
	seenConsumes := map[string]struct{}{}
	for _, requirement := range def.Consumes {
		if err := validateContractSpec(ContractSpec{ID: requirement.ID, Major: requirement.Major}); err != nil {
			return err
		}
		if requirement.Mode != ContractRequired && requirement.Mode != ContractOptional && requirement.Mode != ContractMany {
			return fmt.Errorf("contract %q has invalid mode %q", requirement.ID, requirement.Mode)
		}
		if err := uniqueString(seenConsumes, contractKeyString(ContractSpec{ID: requirement.ID, Major: requirement.Major}), "consumed contract"); err != nil {
			return err
		}
	}
	if len(def.ConfigSchema) != 0 {
		if err := validateConfigSchema(def.ConfigSchema); err != nil {
			return fmt.Errorf("configSchema: %w", err)
		}
	}
	return nil
}

func validateConfigSchema(raw json.RawMessage) error {
	var schema map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&schema); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("invalid trailing JSON")
	}
	if schema == nil {
		return errors.New("must be a JSON object")
	}
	if dialect, ok := schema["$schema"]; ok && dialect != "https://json-schema.org/draft/2020-12/schema" {
		return errors.New("$schema must be JSON Schema draft 2020-12")
	}
	if schema["type"] != "object" {
		return errors.New(`type must be "object"`)
	}
	if additional, ok := schema["additionalProperties"]; !ok || additional != false {
		return errors.New("additionalProperties must be false so unknown config fails closed")
	}
	return nil
}

func validateCompatibility(compat Compatibility) error {
	for engine, constraint := range compat.Engines {
		if !validSlug(engine) || strings.TrimSpace(constraint) == "" {
			return fmt.Errorf("compatibility engine and constraint must be non-empty")
		}
		if _, err := semver.ParseConstraint(constraint); err != nil {
			return fmt.Errorf("invalid %s version constraint %q: %w", engine, constraint, err)
		}
	}
	return nil
}

func validateStability(stability Stability) error {
	switch stability {
	case "", Experimental, Stable, Deprecated:
		return nil
	default:
		return fmt.Errorf("invalid plugin stability %q", stability)
	}
}

func validSlug(value string) bool {
	if value == "" || len(value) > 64 || !asciiLowerOrDigit(rune(value[0])) {
		return false
	}
	for _, char := range value {
		if asciiLowerOrDigit(char) || char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func asciiLowerOrDigit(value rune) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func validPlatformPart(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if !asciiLowerOrDigit(char) {
			return false
		}
	}
	return true
}

func uniqueString(seen map[string]struct{}, value, kind string) error {
	if value == "" {
		return fmt.Errorf("empty %s", kind)
	}
	if _, duplicate := seen[value]; duplicate {
		return fmt.Errorf("duplicate %s %q", kind, value)
	}
	seen[value] = struct{}{}
	return nil
}

func validateContractSpec(spec ContractSpec) error {
	if !validCanonicalPath(spec.ID) || spec.Major == 0 {
		return fmt.Errorf("invalid contract %q major %d", spec.ID, spec.Major)
	}
	return nil
}

func validCanonicalPath(id string) bool {
	return len(id) <= 300 && namecheck.CanonicalPath(id)
}

func validateAuthorityScope(authority Authority, scope AuthorityScope) error {
	if len(scope.Modules) > 128 {
		return fmt.Errorf("authority %q has too many scope modules", authority)
	}
	seen := map[string]struct{}{}
	for _, module := range scope.Modules {
		if len(module) > 200 || strings.TrimSpace(module) == "" || strings.TrimSpace(module) != module || strings.ContainsAny(module, "*\x00\r\n") {
			return fmt.Errorf("authority %q has invalid exact module %q", authority, module)
		}
		if err := uniqueString(seen, module, "scope module"); err != nil {
			return err
		}
	}
	switch authority {
	case AuthorityHostImportDefine, AuthorityCompilerTypeDefine, AuthorityCompilerInstructionDefine:
		if len(scope.Modules) == 0 {
			return fmt.Errorf("authority %q requires at least one exact module", authority)
		}
		if scope.MaxInstances != 0 || scope.MaxMemoryBytes != 0 {
			return fmt.Errorf("authority %q does not accept resource limits", authority)
		}
	case AuthorityInstanceManage, AuthorityCoreInstanceInstantiate:
		if len(scope.Modules) != 0 {
			return fmt.Errorf("authority %q does not accept modules", authority)
		}
		if scope.MaxInstances == 0 || scope.MaxMemoryBytes == 0 {
			return fmt.Errorf("authority %q requires positive maxInstances and maxMemoryBytes", authority)
		}
	default:
		if len(scope.Modules) != 0 || scope.MaxInstances != 0 || scope.MaxMemoryBytes != 0 {
			return fmt.Errorf("authority %q does not accept a scope", authority)
		}
	}
	return nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sortedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// Config strictly decodes the plugin's opaque configuration. Unknown fields in
// struct destinations and trailing JSON fail closed. An absent config is {}.
func (r *Registrar) Config(dst any) error {
	if err := r.ensureOpen(); err != nil {
		return err
	}
	b := r.config
	if len(b) == 0 {
		b = []byte("{}")
	}
	if err := jsonstrict.ValidateTypedJSON(b, dst); err != nil {
		return newPluginDiagnostic(PluginError{Plugin: r.definition.ID, Phase: PluginPhaseConfigure, Path: "config", Err: err})
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return newPluginDiagnostic(PluginError{Plugin: r.definition.ID, Phase: PluginPhaseConfigure, Path: "config", Err: err})
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return newPluginDiagnostic(PluginError{Plugin: r.definition.ID, Phase: PluginPhaseConfigure, Path: "config", Err: fmt.Errorf("trailing JSON value")})
	}
	return nil
}
