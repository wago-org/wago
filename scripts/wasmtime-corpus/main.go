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
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wago-org/wago/internal/wasmtimecorpus"
)

type provenance = wasmtimecorpus.Provenance
type fixture = wasmtimecorpus.Fixture

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
	metadataRoot := filepath.Join(repoRoot, "testdata", "wasmtime")
	provPath := filepath.Join(metadataRoot, "PROVENANCE.json")
	manifestPath := filepath.Join(metadataRoot, "MANIFEST.tsv")
	rustPortsPath := filepath.Join(metadataRoot, "RUST_PORTS.tsv")
	directLedgerPath := filepath.Join(metadataRoot, "DIRECT_ARTIFACTS.tsv")
	coreRoot := filepath.Join(metadataRoot, "core")

	prov, err := wasmtimecorpus.LoadProvenance(provPath)
	if err != nil {
		return err
	}
	fixtures, err := wasmtimecorpus.LoadManifest(manifestPath)
	if err != nil {
		return err
	}
	if err := wasmtimecorpus.ValidateProvenanceFixtureSets(prov, fixtures); err != nil {
		return err
	}
	rustPorts, err := wasmtimecorpus.LoadRustPorts(rustPortsPath)
	if err != nil {
		return err
	}
	directEntries, err := wasmtimecorpus.LoadDirectArtifacts(directLedgerPath)
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
	if err := verifyRustPorts(rustPorts, wasmtimeRoot); err != nil {
		return err
	}

	workCore := coreRoot
	var stageParent string
	if write {
		stageParent, err = os.MkdirTemp(metadataRoot, ".core-stage-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stageParent)
		workCore = filepath.Join(stageParent, "core")
		if err := copyTree(coreRoot, workCore); err != nil {
			return fmt.Errorf("stage current core tree: %w", err)
		}
	}

	legacyCoreSourceFilename := fixtureSet(prov.LegacyCoreSourceFilenamePaths)
	normalizedWABTJSON := fixtureSet(prov.NormalizedWABTJSONFixtures)
	directByPath, err := directArtifactIndex(fixtures, directEntries)
	if err != nil {
		return err
	}

	for _, fixture := range fixtures {
		upstreamPath, err := joinUnder(wasmtimeRoot, pathJoin(prov.SourceRoot, fixture.Path))
		if err != nil {
			return err
		}
		upstream, err := os.ReadFile(upstreamPath)
		if err != nil {
			return fmt.Errorf("read upstream %s: %w", fixture.Path, err)
		}
		source, err := adaptSource(fixture.Path, upstream)
		if err != nil {
			return err
		}
		localDir, err := joinUnder(workCore, strings.TrimSuffix(fixture.Path, ".wast"))
		if err != nil {
			return err
		}
		if fixture.Mode == wasmtimecorpus.ModeWASTJSON {
			if err := syncBytes(filepath.Join(localDir, "source.wast"), source, write); err != nil {
				return fmt.Errorf("%s source: %w", fixture.Path, err)
			}
			if err := syncWABTArtifacts(localDir, fixture.Path, source, wast2json, legacyCoreSourceFilename[fixture.Path], normalizedWABTJSON[fixture.Path], write); err != nil {
				return fmt.Errorf("%s generated artifacts: %w", fixture.Path, err)
			}
			continue
		}
		entry := directByPath[fixture.Path]
		if err := verifyDirectFixture(localDir, source, entry, write); err != nil {
			return fmt.Errorf("%s direct artifacts: %w", fixture.Path, err)
		}
	}

	digest, err := treeDigest(workCore)
	if err != nil {
		return err
	}
	if !write {
		if digest != prov.FixtureTreeSHA256 {
			return fmt.Errorf("fixture tree SHA-256 = %s, want %s", digest, prov.FixtureTreeSHA256)
		}
		fmt.Printf("Wasmtime corpus verified: %d fixtures, tree %s\n", len(fixtures), digest)
		return nil
	}

	prov.FixtureTreeSHA256 = digest
	provData, err := marshalProvenance(prov)
	if err != nil {
		return err
	}
	directData := marshalDirectArtifacts(directEntries)
	if err := commitCorpus(coreRoot, workCore, map[string][]byte{
		provPath:         provData,
		directLedgerPath: directData,
	}); err != nil {
		return err
	}
	fmt.Printf("Wasmtime corpus refreshed: %d fixtures, tree %s\n", len(fixtures), digest)
	return nil
}

func marshalProvenance(p provenance) ([]byte, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
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
	if got := strings.TrimSpace(string(out)); got != want {
		return fmt.Errorf("wast2json version = %q, want exactly %q", got, want)
	}
	return nil
}

func pathJoin(parts ...string) string {
	return pathpkg.Join(parts...)
}

func joinUnder(root, relative string) (string, error) {
	if err := wasmtimecorpus.ValidateRelativePath(relative); err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(rootAbs, filepath.FromSlash(relative))
	rel, err := filepath.Rel(rootAbs, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes root %q", relative, root)
	}
	return joined, nil
}

func fixtureSet(paths []string) map[string]bool {
	set := make(map[string]bool, len(paths))
	for _, path := range paths {
		set[path] = true
	}
	return set
}

func verifyRustPorts(ports []wasmtimecorpus.RustPort, wasmtimeRoot string) error {
	for _, port := range ports {
		source, err := os.ReadFile(filepath.Join(wasmtimeRoot, filepath.FromSlash(port.File)))
		if err != nil {
			return fmt.Errorf("read Rust port source %s: %w", port.File, err)
		}
		if !bytes.Contains(source, []byte("fn "+port.Selector+"(")) {
			return fmt.Errorf("rust port scope %s no longer names a function in the pinned source", port.Scope)
		}
	}
	return nil
}

func directArtifactIndex(fixtures []fixture, entries []wasmtimecorpus.DirectArtifact) (map[string]*wasmtimecorpus.DirectArtifact, error) {
	directModes := map[string]bool{
		wasmtimecorpus.ModeDirectGo:          true,
		wasmtimecorpus.ModeDirectInvalid:     true,
		wasmtimecorpus.ModeDirectConcurrency: true,
	}
	modes := make(map[string]string, len(fixtures))
	for _, fixture := range fixtures {
		modes[fixture.Path] = fixture.Mode
	}
	indexed := make(map[string]*wasmtimecorpus.DirectArtifact, len(entries))
	for i := range entries {
		entry := &entries[i]
		if !directModes[modes[entry.Path]] {
			return nil, fmt.Errorf("direct artifact ledger path %q is not a direct fixture", entry.Path)
		}
		indexed[entry.Path] = entry
	}
	for _, fixture := range fixtures {
		if directModes[fixture.Mode] && indexed[fixture.Path] == nil {
			return nil, fmt.Errorf("direct artifact ledger is missing %q", fixture.Path)
		}
	}
	return indexed, nil
}

func verifyDirectFixture(localDir string, upstreamSource []byte, entry *wasmtimecorpus.DirectArtifact, write bool) error {
	localSource, err := os.ReadFile(filepath.Join(localDir, "source.wast"))
	if err != nil {
		return err
	}
	if !bytes.Equal(localSource, upstreamSource) {
		return fmt.Errorf("upstream source changed; manually review and replace source.wast and module.*.wasm together before syncing")
	}
	sourceSum := sha256.Sum256(localSource)
	sourceDigest := hex.EncodeToString(sourceSum[:])
	artifactDigest, err := wasmtimecorpus.DirectArtifactsSHA256(localDir)
	if err != nil {
		return err
	}
	sourceChanged := sourceDigest != entry.SourceSHA256
	artifactsChanged := artifactDigest != entry.ArtifactsSHA256
	if !sourceChanged && !artifactsChanged {
		return nil
	}
	if !write {
		return fmt.Errorf("ledger hashes differ: source=%s artifacts=%s", sourceDigest, artifactDigest)
	}
	if sourceChanged && !artifactsChanged {
		return fmt.Errorf("source changed but direct artifacts did not; rebuild or deliberately update the ledger after review")
	}
	entry.SourceSHA256 = sourceDigest
	entry.ArtifactsSHA256 = artifactDigest
	return nil
}

func marshalDirectArtifacts(entries []wasmtimecorpus.DirectArtifact) []byte {
	var out strings.Builder
	out.WriteString("# fixture-path\tsource-sha256\tmodule-artifacts-sha256\tadaptation\n")
	for _, entry := range entries {
		fmt.Fprintf(&out, "%s\t%s\t%s\t%s\n", entry.Path, entry.SourceSHA256, entry.ArtifactsSHA256, entry.Adaptation)
	}
	return []byte(out.String())
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

type normalizedWABTDocument struct {
	SourceFilename string                  `json:"source_filename"`
	Commands       []normalizedWABTCommand `json:"commands"`
}

type normalizedWABTCommand struct {
	Type     string               `json:"type"`
	Line     int                  `json:"line"`
	Filename string               `json:"filename,omitempty"`
	Action   normalizedWABTAction `json:"action,omitempty"`
	Text     string               `json:"text,omitempty"`
}

type normalizedWABTAction struct {
	Type  string            `json:"type"`
	Field string            `json:"field"`
	Args  []json.RawMessage `json:"args"`
}

func normalizeWABTJSON(fixturePath, sourceRel, commandsPath string) error {
	if fixturePath != "winch/use-innermost-frame.wast" {
		return fmt.Errorf("no WABT JSON normalization is defined for %s", fixturePath)
	}
	raw, err := os.ReadFile(commandsPath)
	if err != nil {
		return err
	}
	// WABT 1.0.41 emits malformed JSON for this deeply nested function's
	// multi-result assert_trap: the expected entries are adjacent without
	// commas. Remove only that unusable final field, then parse and validate all
	// command metadata instead of hardcoding line numbers or action details.
	marker := []byte(`, "expected": [`)
	if bytes.Count(raw, marker) != 1 {
		return fmt.Errorf("%s malformed expected-result marker count = %d, want 1; review or remove the normalization", fixturePath, bytes.Count(raw, marker))
	}
	prefix, _, _ := bytes.Cut(raw, marker)
	repaired := append(append([]byte(nil), prefix...), []byte("}]}\n")...)
	dec := json.NewDecoder(bytes.NewReader(repaired))
	dec.DisallowUnknownFields()
	var doc normalizedWABTDocument
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("repair %s commands JSON: %w", fixturePath, err)
	}
	if doc.SourceFilename != sourceRel || len(doc.Commands) != 2 ||
		doc.Commands[0].Type != "module" || doc.Commands[0].Filename != "commands.0.wasm" ||
		doc.Commands[1].Type != "assert_trap" || doc.Commands[1].Action.Type != "invoke" ||
		doc.Commands[1].Action.Field == "" || len(doc.Commands[1].Action.Args) != 0 || doc.Commands[1].Text == "" {
		return fmt.Errorf("%s repaired command shape changed: %+v", fixturePath, doc)
	}
	action, err := json.Marshal(doc.Commands[1].Action)
	if err != nil {
		return err
	}
	normalized := []byte(fmt.Sprintf("{\"source_filename\":%q,\"commands\":[\n  {\"type\":\"module\",\"line\":%d,\"filename\":%q},\n  {\"type\":\"assert_trap\",\"line\":%d,\"action\":%s,\"text\":%q}\n]}\n",
		doc.SourceFilename, doc.Commands[0].Line, doc.Commands[0].Filename,
		doc.Commands[1].Line, action, doc.Commands[1].Text))
	return os.WriteFile(commandsPath, normalized, 0o644)
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

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse to stage symlink %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func commitCorpus(coreRoot, stagedCore string, metadata map[string][]byte) error {
	paths := make([]string, 0, len(metadata))
	oldData := make(map[string][]byte, len(metadata))
	temps := make(map[string]string, len(metadata))
	defer func() {
		for _, tmp := range temps {
			_ = os.Remove(tmp)
		}
	}()
	for path, data := range metadata {
		paths = append(paths, path)
		old, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		oldData[path] = old
		tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
		if err != nil {
			return err
		}
		temps[path] = tmp.Name()
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		if err := os.Chmod(tmp.Name(), 0o644); err != nil {
			return err
		}
	}
	sort.Strings(paths)

	backupParent, err := os.MkdirTemp(filepath.Dir(coreRoot), ".core-backup-")
	if err != nil {
		return err
	}
	if err := os.Remove(backupParent); err != nil {
		return err
	}
	if err := os.Rename(coreRoot, backupParent); err != nil {
		return err
	}
	rollbackCore := func() {
		_ = os.RemoveAll(coreRoot)
		_ = os.Rename(backupParent, coreRoot)
	}
	if err := os.Rename(stagedCore, coreRoot); err != nil {
		_ = os.Rename(backupParent, coreRoot)
		return err
	}
	for _, path := range paths {
		if err := os.Rename(temps[path], path); err != nil {
			rollbackCore()
			for _, restorePath := range paths {
				_ = atomicWriteFile(restorePath, oldData[restorePath], 0o644)
			}
			return fmt.Errorf("commit metadata %s: %w", path, err)
		}
		delete(temps, path)
	}
	_ = os.RemoveAll(backupParent)
	return nil
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, path)
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
