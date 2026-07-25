package wasmtimecorpus

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	pathpkg "path"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	ModeWASTJSON          = "wast-json"
	ModeDirectGo          = "direct-go"
	ModeDirectInvalid     = "direct-invalid"
	ModeDirectConcurrency = "direct-concurrency"

	InventoryPorted     = "ported"
	InventoryExcluded   = "excluded"
	InventoryOutOfScope = "out-of-scope"
)

var knownCoverage = map[string]bool{
	"branch-hinting":                true,
	"bulk-memory":                   true,
	"compile-link-workload":         true,
	"concurrent-instance-lifecycle": true,
	"malformed-validation":          true,
	"memory-reuse-bounds":           true,
	"multi-value":                   true,
	"nontrapping-float-to-int":      true,
	"reference-types":               true,
	"runtime-regression":            true,
	"sign-extension":                true,
	"simd":                          true,
}

var knownModes = map[string]bool{
	ModeWASTJSON:          true,
	ModeDirectGo:          true,
	ModeDirectInvalid:     true,
	ModeDirectConcurrency: true,
}

type Provenance struct {
	UpstreamRepo                  string   `json:"upstream_repo"`
	Revision                      string   `json:"revision"`
	RevisionDate                  string   `json:"revision_date"`
	SourceRoot                    string   `json:"source_root"`
	WABTRepo                      string   `json:"wabt_repo"`
	WABTRevision                  string   `json:"wabt_revision"`
	WABTVersion                   string   `json:"wabt_version"`
	LegacyCoreSourceFilenamePaths []string `json:"legacy_core_source_filenames"`
	NormalizedWABTJSONFixtures    []string `json:"normalized_wabt_json_fixtures"`
	FixtureTreeSHA256             string   `json:"fixture_tree_sha256"`
}

type Fixture struct {
	Path     string
	Coverage string
	Mode     string
}

type RustPort struct {
	Scope        string
	File         string
	Selector     string
	LocalTest    string
	Adaptation   string
	SourceSHA256 string
}

type InventoryEntry struct {
	Path   string
	Status string
	Reason string
}

type DirectArtifact struct {
	Path            string
	SourceSHA256    string
	ArtifactsSHA256 string
	Adaptation      string
}

func LoadProvenance(path string) (Provenance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Provenance{}, err
	}
	var p Provenance
	if err := decodeStrictJSON(data, &p); err != nil {
		return Provenance{}, fmt.Errorf("decode provenance: %w", err)
	}
	if err := ValidateProvenance(p); err != nil {
		return Provenance{}, err
	}
	return p, nil
}

func ValidateProvenance(p Provenance) error {
	for name, raw := range map[string]string{
		"upstream_repo":       p.UpstreamRepo,
		"revision":            p.Revision,
		"revision_date":       p.RevisionDate,
		"source_root":         p.SourceRoot,
		"wabt_repo":           p.WABTRepo,
		"wabt_revision":       p.WABTRevision,
		"wabt_version":        p.WABTVersion,
		"fixture_tree_sha256": p.FixtureTreeSHA256,
	} {
		if raw == "" {
			return fmt.Errorf("provenance field %s is empty", name)
		}
	}
	if err := validateHTTPSRepo("upstream_repo", p.UpstreamRepo); err != nil {
		return err
	}
	if err := validateHTTPSRepo("wabt_repo", p.WABTRepo); err != nil {
		return err
	}
	if err := validateHex("revision", p.Revision, 20); err != nil {
		return err
	}
	if err := validateHex("wabt_revision", p.WABTRevision, 20); err != nil {
		return err
	}
	if err := validateHex("fixture_tree_sha256", p.FixtureTreeSHA256, 32); err != nil {
		return err
	}
	if parsed, err := time.Parse("2006-01-02", p.RevisionDate); err != nil || parsed.Format("2006-01-02") != p.RevisionDate {
		return fmt.Errorf("invalid revision_date %q", p.RevisionDate)
	}
	if err := ValidateRelativePath(p.SourceRoot); err != nil {
		return fmt.Errorf("invalid source_root %q: %w", p.SourceRoot, err)
	}
	parts := strings.Split(p.WABTVersion, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid wabt_version %q", p.WABTVersion)
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("invalid wabt_version %q", p.WABTVersion)
		}
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return fmt.Errorf("invalid wabt_version %q", p.WABTVersion)
		}
	}
	return nil
}

func LoadManifest(path string) ([]Fixture, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var fixtures []Fixture
	previous := ""
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("manifest line %d has %d fields", lineNo, len(fields))
		}
		fixture := Fixture{Path: fields[0], Coverage: fields[1], Mode: fields[2]}
		if err := ValidateRelativePath(fixture.Path); err != nil || !strings.HasSuffix(fixture.Path, ".wast") {
			return nil, fmt.Errorf("manifest line %d has invalid fixture path %q", lineNo, fixture.Path)
		}
		if fixture.Path <= previous {
			return nil, fmt.Errorf("manifest paths are not strictly sorted: %q after %q", fixture.Path, previous)
		}
		if seen[fixture.Path] {
			return nil, fmt.Errorf("manifest contains duplicate path %q", fixture.Path)
		}
		if !knownModes[fixture.Mode] {
			return nil, fmt.Errorf("manifest line %d has unknown mode %q", lineNo, fixture.Mode)
		}
		labels := strings.Split(fixture.Coverage, ",")
		if len(labels) == 0 {
			return nil, fmt.Errorf("manifest line %d has no coverage labels", lineNo)
		}
		previousLabel := ""
		for _, label := range labels {
			if !knownCoverage[label] {
				return nil, fmt.Errorf("manifest line %d has unknown coverage label %q", lineNo, label)
			}
			if label <= previousLabel {
				return nil, fmt.Errorf("manifest line %d coverage labels are not strictly sorted: %q after %q", lineNo, label, previousLabel)
			}
			previousLabel = label
		}
		fixtures = append(fixtures, fixture)
		seen[fixture.Path] = true
		previous = fixture.Path
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(fixtures) == 0 {
		return nil, fmt.Errorf("manifest has no fixtures")
	}
	return fixtures, nil
}

func LoadRustPorts(path string) ([]RustPort, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var ports []RustPort
	previous := ""
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return nil, fmt.Errorf("rust port ledger line %d has %d fields", lineNo, len(fields))
		}
		file, selector, ok := strings.Cut(fields[0], "::")
		if !ok || strings.Contains(selector, "::") || !isIdentifier(selector) {
			return nil, fmt.Errorf("rust port ledger line %d has invalid scope %q", lineNo, fields[0])
		}
		if err := ValidateRelativePath(file); err != nil || !strings.HasPrefix(file, "tests/all/") || !strings.HasSuffix(file, ".rs") {
			return nil, fmt.Errorf("rust port ledger line %d has invalid source path %q", lineNo, file)
		}
		if fields[0] <= previous {
			return nil, fmt.Errorf("rust port scopes are not strictly sorted: %q after %q", fields[0], previous)
		}
		if seen[fields[0]] {
			return nil, fmt.Errorf("rust port ledger contains duplicate scope %q", fields[0])
		}
		if !strings.HasPrefix(fields[1], "TestWasmtimePort") || !isIdentifier(fields[1]) {
			return nil, fmt.Errorf("rust port ledger line %d has invalid local test %q", lineNo, fields[1])
		}
		if strings.TrimSpace(fields[2]) == "" {
			return nil, fmt.Errorf("rust port ledger line %d has no adaptation", lineNo)
		}
		if err := validateHex("source_sha256", fields[3], 32); err != nil {
			return nil, fmt.Errorf("rust port ledger line %d: %w", lineNo, err)
		}
		ports = append(ports, RustPort{Scope: fields[0], File: file, Selector: selector, LocalTest: fields[1], Adaptation: fields[2], SourceSHA256: fields[3]})
		seen[fields[0]] = true
		previous = fields[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("rust port ledger has no entries")
	}
	return ports, nil
}

func LoadInventory(path string) ([]InventoryEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []InventoryEntry
	previous := ""
	scanner := bufio.NewScanner(f)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("inventory line %d has %d fields", lineNo, len(fields))
		}
		if err := ValidateRelativePath(fields[0]); err != nil || !strings.HasSuffix(fields[0], ".wast") {
			return nil, fmt.Errorf("inventory line %d has invalid path %q", lineNo, fields[0])
		}
		if fields[0] <= previous {
			return nil, fmt.Errorf("inventory paths are not strictly sorted: %q after %q", fields[0], previous)
		}
		switch fields[1] {
		case InventoryPorted, InventoryExcluded, InventoryOutOfScope:
		default:
			return nil, fmt.Errorf("inventory line %d has unknown status %q", lineNo, fields[1])
		}
		if strings.TrimSpace(fields[2]) == "" {
			return nil, fmt.Errorf("inventory line %d has no reason", lineNo)
		}
		entries = append(entries, InventoryEntry{Path: fields[0], Status: fields[1], Reason: fields[2]})
		previous = fields[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("inventory has no entries")
	}
	return entries, nil
}

func ValidateInventory(entries []InventoryEntry, fixtures []Fixture, upstreamPaths []string) error {
	fixtureSet := make(map[string]bool, len(fixtures))
	for _, fixture := range fixtures {
		fixtureSet[fixture.Path] = true
	}
	upstreamSet := make(map[string]bool, len(upstreamPaths))
	previous := ""
	for _, path := range upstreamPaths {
		if err := ValidateRelativePath(path); err != nil || !strings.HasSuffix(path, ".wast") {
			return fmt.Errorf("upstream inventory has invalid path %q", path)
		}
		if path <= previous {
			return fmt.Errorf("upstream inventory paths are not strictly sorted: %q after %q", path, previous)
		}
		upstreamSet[path] = true
		previous = path
	}
	classified := make(map[string]InventoryEntry, len(entries))
	for _, entry := range entries {
		if !upstreamSet[entry.Path] {
			return fmt.Errorf("inventory path %q no longer exists upstream", entry.Path)
		}
		if entry.Status == InventoryPorted && !fixtureSet[entry.Path] {
			return fmt.Errorf("ported inventory path %q is missing from MANIFEST.tsv", entry.Path)
		}
		if entry.Status != InventoryPorted && fixtureSet[entry.Path] {
			return fmt.Errorf("manifest path %q is classified as %s", entry.Path, entry.Status)
		}
		classified[entry.Path] = entry
	}
	for path := range upstreamSet {
		if _, ok := classified[path]; !ok {
			return fmt.Errorf("upstream fixture %q is unclassified", path)
		}
	}
	for path := range fixtureSet {
		if entry, ok := classified[path]; !ok || entry.Status != InventoryPorted {
			return fmt.Errorf("manifest path %q is not classified as ported", path)
		}
	}
	return nil
}

func LoadDirectArtifacts(path string) ([]DirectArtifact, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []DirectArtifact
	previous := ""
	scanner := bufio.NewScanner(f)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return nil, fmt.Errorf("direct artifact ledger line %d has %d fields", lineNo, len(fields))
		}
		if err := ValidateRelativePath(fields[0]); err != nil || !strings.HasSuffix(fields[0], ".wast") {
			return nil, fmt.Errorf("direct artifact ledger line %d has invalid path %q", lineNo, fields[0])
		}
		if fields[0] <= previous {
			return nil, fmt.Errorf("direct artifact paths are not strictly sorted: %q after %q", fields[0], previous)
		}
		if err := validateHex("source_sha256", fields[1], 32); err != nil {
			return nil, fmt.Errorf("direct artifact ledger line %d: %w", lineNo, err)
		}
		if err := validateHex("artifacts_sha256", fields[2], 32); err != nil {
			return nil, fmt.Errorf("direct artifact ledger line %d: %w", lineNo, err)
		}
		if strings.TrimSpace(fields[3]) == "" {
			return nil, fmt.Errorf("direct artifact ledger line %d has no adaptation", lineNo)
		}
		entries = append(entries, DirectArtifact{Path: fields[0], SourceSHA256: fields[1], ArtifactsSHA256: fields[2], Adaptation: fields[3]})
		previous = fields[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("direct artifact ledger has no entries")
	}
	return entries, nil
}

func ValidateProvenanceFixtureSets(p Provenance, fixtures []Fixture) error {
	modes := make(map[string]string, len(fixtures))
	for _, fixture := range fixtures {
		modes[fixture.Path] = fixture.Mode
	}
	for name, paths := range map[string][]string{
		"legacy core source filename": p.LegacyCoreSourceFilenamePaths,
		"normalized WABT JSON":        p.NormalizedWABTJSONFixtures,
	} {
		previous := ""
		for _, fixturePath := range paths {
			if fixturePath <= previous {
				return fmt.Errorf("%s fixture paths are not strictly sorted: %q after %q", name, fixturePath, previous)
			}
			if modes[fixturePath] != ModeWASTJSON {
				return fmt.Errorf("%s fixture path %q is not a wast-json fixture", name, fixturePath)
			}
			previous = fixturePath
		}
	}
	return nil
}

func ValidateRelativePath(raw string) error {
	if raw == "" || raw == "." || strings.Contains(raw, "\\") || strings.ContainsRune(raw, '\x00') {
		return fmt.Errorf("path is empty, dot, or non-canonical")
	}
	if pathpkg.IsAbs(raw) || pathpkg.Clean(raw) != raw || raw == ".." || strings.HasPrefix(raw, "../") {
		return fmt.Errorf("path is absolute or escapes its root")
	}
	first, _, _ := strings.Cut(raw, "/")
	if strings.Contains(first, ":") {
		return fmt.Errorf("path has a volume-like prefix")
	}
	return nil
}

func decodeStrictJSON(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateHTTPSRepo(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid %s %q", name, raw)
	}
	return nil
}

func validateHex(name, raw string, bytesLen int) error {
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != bytesLen || raw != strings.ToLower(raw) {
		return fmt.Errorf("invalid %s %q", name, raw)
	}
	return nil
}

func isIdentifier(raw string) bool {
	if raw == "" {
		return false
	}
	for i, r := range raw {
		if r == '_' || unicode.IsLetter(r) || i > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}
