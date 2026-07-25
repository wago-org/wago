// Command wasmtime-corpus verifies or refreshes the checked-in Wasmtime core
// regression fixtures from the exact revision recorded in PROVENANCE.json.
//
// By default it is read-only. Pass -write to replace source.wast and generated
// WABT artifacts, and -fetch to clone/fetch the pinned Wasmtime revision first.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type provenance struct {
	UpstreamRepo                  string   `json:"upstream_repo"`
	Revision                      string   `json:"revision"`
	RevisionDate                  string   `json:"revision_date"`
	SourceRoot                    string   `json:"source_root"`
	WABTVersion                   string   `json:"wabt_version"`
	LegacyCoreSourceFilenamePaths []string `json:"legacy_core_source_filenames"`
	NormalizedWABTJSONFixtures    []string `json:"normalized_wabt_json_fixtures"`
	FixtureTreeSHA256             string   `json:"fixture_tree_sha256"`
}

type fixture struct {
	Path string
	Mode string
}

func main() {
	var (
		repoRoot     = flag.String("repo", ".", "wago repository root")
		wasmtimeRoot = flag.String("wasmtime", ".tmp/wasmtime-corpus-upstream", "Wasmtime checkout")
		wast2json    = flag.String("wast2json", "wast2json", "path to the pinned wast2json executable")
		fetch        = flag.Bool("fetch", false, "clone/fetch and check out the pinned Wasmtime revision")
		write        = flag.Bool("write", false, "replace checked-in sources/artifacts and update the tree digest")
	)
	flag.Parse()
	if err := run(filepath.Clean(*repoRoot), filepath.Clean(*wasmtimeRoot), *wast2json, *fetch, *write); err != nil {
		fmt.Fprintln(os.Stderr, "wasmtime-corpus:", err)
		os.Exit(1)
	}
}

func run(repoRoot, wasmtimeRoot, wast2json string, fetch, write bool) error {
	provPath := filepath.Join(repoRoot, "testdata", "wasmtime", "PROVENANCE.json")
	prov, err := loadProvenance(provPath)
	if err != nil {
		return err
	}
	if fetch {
		if err := fetchRevision(wasmtimeRoot, prov); err != nil {
			return err
		}
	}
	if err := verifyRevision(wasmtimeRoot, prov); err != nil {
		return err
	}
	if err := verifyWABTVersion(wast2json, prov.WABTVersion); err != nil {
		return err
	}
	fixtures, err := loadManifest(filepath.Join(repoRoot, "testdata", "wasmtime", "MANIFEST.tsv"))
	if err != nil {
		return err
	}
	if err := verifyRustPorts(filepath.Join(repoRoot, "testdata", "wasmtime", "RUST_PORTS.tsv"), wasmtimeRoot); err != nil {
		return err
	}
	fixtureModes := make(map[string]string, len(fixtures))
	for _, fixture := range fixtures {
		fixtureModes[fixture.Path] = fixture.Mode
	}
	legacyCoreSourceFilename, err := provenanceFixtureSet("legacy core source filename", prov.LegacyCoreSourceFilenamePaths, fixtureModes)
	if err != nil {
		return err
	}
	normalizedWABTJSON, err := provenanceFixtureSet("normalized WABT JSON", prov.NormalizedWABTJSONFixtures, fixtureModes)
	if err != nil {
		return err
	}

	for _, fixture := range fixtures {
		upstreamPath := filepath.Join(wasmtimeRoot, filepath.FromSlash(prov.SourceRoot), filepath.FromSlash(fixture.Path))
		upstream, err := os.ReadFile(upstreamPath)
		if err != nil {
			return fmt.Errorf("read upstream %s: %w", fixture.Path, err)
		}
		source, err := adaptSource(fixture.Path, upstream)
		if err != nil {
			return err
		}
		localDir := filepath.Join(repoRoot, "testdata", "wasmtime", "core", filepath.FromSlash(strings.TrimSuffix(fixture.Path, ".wast")))
		if err := syncBytes(filepath.Join(localDir, "source.wast"), source, write); err != nil {
			return fmt.Errorf("%s source: %w", fixture.Path, err)
		}
		if fixture.Mode == "wast-json" {
			if err := syncWABTArtifacts(localDir, fixture.Path, source, wast2json, legacyCoreSourceFilename[fixture.Path], normalizedWABTJSON[fixture.Path], write); err != nil {
				return fmt.Errorf("%s generated artifacts: %w", fixture.Path, err)
			}
		}
	}

	coreRoot := filepath.Join(repoRoot, "testdata", "wasmtime", "core")
	digest, err := treeDigest(coreRoot)
	if err != nil {
		return err
	}
	if write {
		prov.FixtureTreeSHA256 = digest
		if err := writeProvenance(provPath, prov); err != nil {
			return err
		}
		fmt.Printf("Wasmtime corpus refreshed: %d fixtures, tree %s\n", len(fixtures), digest)
		return nil
	}
	if digest != prov.FixtureTreeSHA256 {
		return fmt.Errorf("fixture tree SHA-256 = %s, want %s", digest, prov.FixtureTreeSHA256)
	}
	fmt.Printf("Wasmtime corpus verified: %d fixtures, tree %s\n", len(fixtures), digest)
	return nil
}

func loadProvenance(path string) (provenance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return provenance{}, err
	}
	var p provenance
	if err := json.Unmarshal(data, &p); err != nil {
		return provenance{}, fmt.Errorf("decode provenance: %w", err)
	}
	if p.UpstreamRepo == "" || p.Revision == "" || p.RevisionDate == "" || p.SourceRoot == "" || p.WABTVersion == "" || p.FixtureTreeSHA256 == "" {
		return provenance{}, fmt.Errorf("provenance has empty required fields")
	}
	return p, nil
}

func writeProvenance(path string, p provenance) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func fetchRevision(root string, p provenance) error {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
			return err
		}
		if out, err := exec.Command("git", "clone", "--filter=blob:none", "--no-checkout", p.UpstreamRepo, root).CombinedOutput(); err != nil {
			return fmt.Errorf("git clone: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	if out, err := exec.Command("git", "-C", root, "fetch", "--depth=1", "origin", p.Revision).CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch %s: %w: %s", p.Revision, err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "-C", root, "checkout", "--detach", p.Revision).CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s: %w: %s", p.Revision, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func verifyRevision(root string, p provenance) error {
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect Wasmtime checkout: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if got := strings.TrimSpace(string(out)); got != p.Revision {
		return fmt.Errorf("wasmtime checkout revision = %s, want %s", got, p.Revision)
	}
	out, err = exec.Command("git", "-C", root, "show", "-s", "--format=%cs", "HEAD").CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect Wasmtime revision date: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if got := strings.TrimSpace(string(out)); got != p.RevisionDate {
		return fmt.Errorf("wasmtime revision date = %s, want %s", got, p.RevisionDate)
	}
	return nil
}

func verifyWABTVersion(wast2json, want string) error {
	out, err := exec.Command(wast2json, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s --version: %w: %s", wast2json, err, strings.TrimSpace(string(out)))
	}
	if !strings.Contains(string(out), want) {
		return fmt.Errorf("wast2json version = %q, want %s", strings.TrimSpace(string(out)), want)
	}
	return nil
}

func loadManifest(path string) ([]fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fixtures []fixture
	for lineNo, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("manifest line %d has %d fields", lineNo+1, len(fields))
		}
		fixtures = append(fixtures, fixture{Path: fields[0], Mode: fields[2]})
	}
	return fixtures, nil
}

func provenanceFixtureSet(name string, paths []string, fixtureModes map[string]string) (map[string]bool, error) {
	set := make(map[string]bool, len(paths))
	previous := ""
	for _, path := range paths {
		if path <= previous {
			return nil, fmt.Errorf("%s fixture paths are not strictly sorted: %q after %q", name, path, previous)
		}
		if fixtureModes[path] != "wast-json" {
			return nil, fmt.Errorf("%s fixture path %q is not a wast-json fixture", name, path)
		}
		set[path] = true
		previous = path
	}
	return set, nil
}

func verifyRustPorts(ledgerPath, wasmtimeRoot string) error {
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		return err
	}
	for lineNo, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return fmt.Errorf("rust port ledger line %d has %d fields", lineNo+1, len(fields))
		}
		file, selector, ok := strings.Cut(fields[0], "::")
		if !ok {
			return fmt.Errorf("rust port ledger line %d has invalid scope %q", lineNo+1, fields[0])
		}
		source, err := os.ReadFile(filepath.Join(wasmtimeRoot, filepath.FromSlash(file)))
		if err != nil {
			return fmt.Errorf("read Rust port source %s: %w", file, err)
		}
		if !strings.HasPrefix(selector, "portable-") && !bytes.Contains(source, []byte("fn "+selector+"(")) {
			return fmt.Errorf("rust port scope %s no longer names a function in the pinned source", fields[0])
		}
	}
	return nil
}

func adaptSource(path string, upstream []byte) ([]byte, error) {
	if !strings.HasPrefix(path, "embenchen_") {
		return upstream, nil
	}
	const notice = ";; Wago port modification: register the named env provider explicitly so the standard WAST replay can resolve its imports.\n"
	const moduleBoundary = "\n)\n\n(module\n"
	if bytes.Contains(upstream, []byte("(register \"env\" $env)")) {
		return nil, fmt.Errorf("%s upstream now registers env; remove the Wago adaptation", path)
	}
	if !bytes.Contains(upstream, []byte(moduleBoundary)) {
		return nil, fmt.Errorf("%s env-module boundary not found", path)
	}
	adapted := bytes.Replace(upstream, []byte(moduleBoundary), []byte("\n)\n\n(register \"env\" $env)\n\n(module\n"), 1)
	return append([]byte(notice), adapted...), nil
}

func normalizeWABTJSON(fixturePath, sourceRel, commandsPath string) error {
	var data []byte
	switch fixturePath {
	case "winch/use-innermost-frame.wast":
		// WABT 1.0.41 emits malformed JSON for this deeply nested function's
		// multi-result assert_trap (adjacent expected entries without commas).
		// The assertion only observes the trap, so preserve the valid historical
		// replay command with the unusable expected-result metadata omitted.
		data = []byte(fmt.Sprintf("{\"source_filename\":%q,\"commands\":[\n  {\"type\":\"module\",\"line\":1,\"filename\":\"commands.0.wasm\"},\n  {\"type\":\"assert_trap\",\"line\":1944,\"action\":{\"type\":\"invoke\",\"field\":\"main\",\"args\":[]},\"text\":\"unreachable\"}\n]}\n", sourceRel))
	default:
		return fmt.Errorf("no WABT JSON normalization is defined for %s", fixturePath)
	}
	return os.WriteFile(commandsPath, data, 0o644)
}

func syncWABTArtifacts(localDir, fixturePath string, source []byte, wast2json string, legacyCoreSourceFilename, normalizeJSON, write bool) error {
	tmp, err := os.MkdirTemp("", "wago-wasmtime-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	// WABT records the input spelling in source_filename. Recreate the original
	// repository-relative path so regeneration is byte-for-byte deterministic
	// rather than embedding a temporary absolute directory. A small, explicitly
	// recorded legacy subset was originally generated after moving into core/.
	layout := "wasm2"
	if legacyCoreSourceFilename {
		layout = "core"
	}
	sourceRel := filepath.ToSlash(filepath.Join("testdata", "wasmtime", layout, strings.TrimSuffix(fixturePath, ".wast"), "source.wast"))
	sourcePath := filepath.Join(tmp, filepath.FromSlash(sourceRel))
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		return err
	}
	cmd := exec.Command(wast2json, sourceRel, "-o", "commands.json")
	cmd.Dir = tmp
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wast2json: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if normalizeJSON {
		if err := normalizeWABTJSON(fixturePath, sourceRel, filepath.Join(tmp, "commands.json")); err != nil {
			return err
		}
	}
	generated, err := artifactNames(tmp)
	if err != nil {
		return err
	}
	checkedIn, err := artifactNames(localDir)
	if err != nil {
		return err
	}
	if write {
		for _, name := range checkedIn {
			if err := os.Remove(filepath.Join(localDir, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		for _, name := range generated {
			data, err := os.ReadFile(filepath.Join(tmp, name))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(localDir, name), data, 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	if strings.Join(generated, "\n") != strings.Join(checkedIn, "\n") {
		return fmt.Errorf("artifact set differs: generated=%v checked-in=%v", generated, checkedIn)
	}
	for _, name := range generated {
		generatedData, err := os.ReadFile(filepath.Join(tmp, name))
		if err != nil {
			return err
		}
		if err := syncBytes(filepath.Join(localDir, name), generatedData, false); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func artifactNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if name == "commands.json" || strings.HasPrefix(name, "commands.") && strings.HasSuffix(name, ".wasm") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func syncBytes(path string, want []byte, write bool) error {
	got, err := os.ReadFile(path)
	if write {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err == nil && bytes.Equal(got, want) {
			return nil
		}
		return os.WriteFile(path, want, 0o644)
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("content differs; run with -write")
	}
	return nil
}

func treeDigest(root string) (string, error) {
	var paths []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
