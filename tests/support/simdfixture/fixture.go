//go:build linux && !tinygo

package simdfixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Package simdfixture supplies strict pinned WABT JSON transport for both
// private backend and public execution qualification. Prepare fixtures with
// scripts/prepare-riscv64-simd-spec.py; runners report exclusions explicitly.
const maxModuleBytes = 1 << 20

const swarSpecCommit = "05ca4182176763112561ae20153975c12bd689e4"

// Hash sorted basename + NUL + SHA256 + newline rows. Prepared JSON uses source
// basenames rather than absolute checkout paths. Binary-tool hashes remain
// platform-specific; these fixture aggregates are reproducible across hosts.
const swarSpecSourcesSHA256 = "b8892140ce454ba04d801cda7084087138aec58ba3817a7280869c76afe8350f"
const swarSpecFilesSHA256 = "1ab236a3015f301d9baf0e38954172b9f2e8377e490cd1ca8d2f7b495eb3a456"
const swarRelaxedSpecCommit = "9d36019973201a19f9c9ebb0f10828b2fe2374aa"
const swarRelaxedSpecSourcesSHA256 = "4189bbc768f1b23d7b7db2986235aa9ddd847181240b1884e93f27d6a1e2ed9a"
const swarRelaxedSpecFilesSHA256 = "317e94dc858ff574b36e1b3de057f524c5fdeda0cb6fd93869de7f58495c3b05"

type Manifest struct {
	SpecCommit        string            `json:"spec_commit"`
	WabtVersion       string            `json:"wabt_version"`
	WabtArchiveSHA256 string            `json:"wabt_archive_sha256"`
	WabtBinarySHA256  string            `json:"wabt_binary_sha256"`
	Sources           map[string]string `json:"sources"`
	Files             map[string]string `json:"files"`
}

type Document struct {
	SourceFilename string    `json:"source_filename"`
	Commands       []Command `json:"commands"`
}

type Command struct {
	Type       string  `json:"type"`
	Line       int     `json:"line"`
	Filename   string  `json:"filename"`
	ModuleType string  `json:"module_type"`
	As         string  `json:"as"`
	Text       string  `json:"text"`
	Action     Action  `json:"action"`
	Expected   []Value `json:"expected"`
	Either     []Value `json:"either"`
}

type Action struct {
	Type  string  `json:"type"`
	Field string  `json:"field"`
	Args  []Value `json:"args"`
}

type Value struct {
	Type     string          `json:"type"`
	LaneType string          `json:"lane_type"`
	Value    json.RawMessage `json:"value"`
}

func Decode(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("fixture has trailing JSON: %v", err)
	}
	return nil
}

func Read(dir, name string, limit int64) ([]byte, error) {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return nil, fmt.Errorf("invalid fixture basename %q", name)
	}
	p := filepath.Join(dir, name)
	info, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("fixture is not a bounded regular file: %s", name)
	}
	return os.ReadFile(p)
}

func (m *Manifest) Read(dir, name string, limit int64) ([]byte, error) {
	want, ok := m.Files[name]
	if !ok {
		return nil, fmt.Errorf("fixture missing from manifest: %s", name)
	}
	data, err := Read(dir, name, limit)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != want {
		return nil, fmt.Errorf("fixture digest mismatch: %s", name)
	}
	return data, nil
}

func HashRows(rows map[string]string) (string, error) {
	names := make([]string, 0, len(rows))
	for name, digest := range rows {
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size || name == "" || name != filepath.Base(name) || strings.ContainsAny(name, "/\\\x00\n") || name == "." || name == ".." {
			return "", fmt.Errorf("invalid manifest row %q", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		_, _ = io.WriteString(hash, name+"\x00"+rows[name]+"\n")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func LoadManifest(dir string, relaxed bool) (*Manifest, error) {
	data, err := Read(dir, "rv64-simd-manifest.json", 2<<20)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := Decode(data, &m); err != nil {
		return nil, err
	}
	archives := map[string]bool{
		"83f8122e924745fcd70636e3594bc01c4c47f2d4c8f3c63b5d70d3f83a482677": true, // Linux amd64
		"5e35416ee8725dc7cc0572e4392a8117cbf008b0e34c0db65c75506b0299cdbf": true, // Linux arm64
		"e5269d6bbe05dfeb179e4f21111b3a641d6ccaa38b0b21d472ae5c65f8c4ff5d": true, // Darwin arm64
		"ca9e69cc1de13b4633a3c74fd697319303b21108529d4f10960af4e1f4a65893": true, // Darwin amd64 source
	}
	binaryHash, hashErr := hex.DecodeString(m.WabtBinarySHA256)
	commit, sourceHash, filesHash, sources, files := swarSpecCommit, swarSpecSourcesSHA256, swarSpecFilesSHA256, 57, 1708
	if relaxed {
		commit, sourceHash, filesHash, sources, files = swarRelaxedSpecCommit, swarRelaxedSpecSourcesSHA256, swarRelaxedSpecFilesSHA256, 7, 15
	}
	if m.SpecCommit != commit || m.WabtVersion != "1.0.41" || !archives[m.WabtArchiveSHA256] || hashErr != nil || len(binaryHash) != sha256.Size || len(m.Sources) != sources || len(m.Files) != files {
		return nil, fmt.Errorf("unexpected pinned source/tool provenance")
	}
	for _, expected := range []struct {
		rows map[string]string
		hash string
	}{{m.Sources, sourceHash}, {m.Files, filesHash}} {
		actual, err := HashRows(expected.rows)
		if err != nil || actual != expected.hash {
			return nil, fmt.Errorf("pinned fixture manifest differs: got %s, want %s (%v)", actual, expected.hash, err)
		}
	}
	return &m, nil
}

func Layout(v Value) (wasm.ValType, int, int, bool) {
	switch v.Type {
	case "i32":
		return wasm.I32, 32, 1, false
	case "i64":
		return wasm.I64, 64, 1, false
	case "f32":
		return wasm.F32, 32, 1, true
	case "f64":
		return wasm.F64, 64, 1, true
	case "v128":
		switch v.LaneType {
		case "i8", "i8x16":
			return wasm.V128, 8, 16, false
		case "i16", "i16x8":
			return wasm.V128, 16, 8, false
		case "i32", "i32x4":
			return wasm.V128, 32, 4, false
		case "f32", "f32x4":
			return wasm.V128, 32, 4, true
		case "i64", "i64x2":
			return wasm.V128, 64, 2, false
		case "f64", "f64x2":
			return wasm.V128, 64, 2, true
		}
	}
	return wasm.ValType{}, 0, 0, false
}

func Lanes(v Value) ([]string, error) {
	_, bits, count, _ := Layout(v)
	if bits == 0 {
		return nil, fmt.Errorf("unsupported fixture type %s/%s", v.Type, v.LaneType)
	}
	var values []string
	if v.Type == "v128" {
		if err := json.Unmarshal(v.Value, &values); err != nil {
			return nil, err
		}
	} else {
		var value string
		if err := json.Unmarshal(v.Value, &value); err != nil {
			return nil, err
		}
		values = []string{value}
	}
	if len(values) != count {
		return nil, fmt.Errorf("fixture lane count %d, want %d", len(values), count)
	}
	return values, nil
}

func Bits(value string, bits int) (uint64, error) {
	if n, err := strconv.ParseUint(value, 10, bits); err == nil {
		return n, nil
	}
	n, err := strconv.ParseInt(value, 10, bits)
	if err != nil {
		return 0, err
	}
	if bits < 64 {
		return uint64(n) & (uint64(1)<<bits - 1), nil
	}
	return uint64(n), nil
}

func Input(v Value) ([]byte, error) {
	values, err := Lanes(v)
	if err != nil {
		return nil, err
	}
	_, bits, _, _ := Layout(v)
	n := 8
	if v.Type == "v128" {
		n = 16
	}
	result := make([]byte, n)
	for i, value := range values {
		lane, err := Bits(value, bits)
		if err != nil {
			return nil, err
		}
		if v.Type != "v128" {
			binary.LittleEndian.PutUint64(result, lane)
			break
		}
		for j := range bits / 8 {
			result[i*(bits/8)+j] = byte(lane >> (8 * j))
		}
	}
	return result, nil
}

func NaN(lane uint64, bits int, canonical bool) bool {
	exponent, quiet, payload := uint64(0x7ff0000000000000), uint64(0x8000000000000), uint64(0xfffffffffffff)
	if bits == 32 {
		exponent, quiet, payload = 0x7f800000, 0x400000, 0x7fffff
	} else if bits != 64 {
		return false
	}
	return lane&exponent == exponent && (canonical && lane&payload == quiet || !canonical && lane&quiet != 0)
}

func Match(got []byte, v Value) error {
	values, err := Lanes(v)
	if err != nil {
		return err
	}
	_, bits, _, floating := Layout(v)
	n := 8
	if v.Type == "v128" {
		n = 16
	}
	if len(got) != n {
		return fmt.Errorf("result width %d, want %d", len(got), n)
	}
	if v.Type != "v128" && bits == 32 && binary.LittleEndian.Uint64(got)>>32 != 0 {
		return fmt.Errorf("non-normalized narrow carrier %x", got)
	}
	for i, value := range values {
		var lane uint64
		for j := range bits / 8 {
			lane |= uint64(got[i*(bits/8)+j]) << (8 * j)
		}
		if floating && (value == "nan:canonical" || value == "nan:arithmetic") {
			if NaN(lane, bits, value == "nan:canonical") {
				continue
			}
			return fmt.Errorf("lane %d: %x is not %s", i, lane, value)
		}
		want, err := Bits(value, bits)
		if err != nil {
			return err
		}
		if lane != want {
			return fmt.Errorf("lane %d: got %x, want %x", i, lane, want)
		}
	}
	return nil
}

func Patterns(command Command) ([][]Value, error) {
	if command.Either == nil {
		return [][]Value{command.Expected}, nil
	}
	if len(command.Either) == 0 || command.Expected != nil || command.Type != "assert_return" {
		return nil, fmt.Errorf("ambiguous expected/either result pattern")
	}
	patterns := make([][]Value, len(command.Either))
	var typ wasm.ValType
	for i, alternative := range command.Either {
		actual, bits, _, _ := Layout(alternative)
		if bits == 0 || i != 0 && actual != typ {
			return nil, fmt.Errorf("alternative result type differs or is unsupported")
		}
		if _, err := Lanes(alternative); err != nil {
			return nil, err
		}
		typ = actual
		patterns[i] = []Value{alternative}
	}
	return patterns, nil
}

func MatchPatterns(got []byte, patterns [][]Value) error {
	// Either matches an entire result pattern; lanes from different alternatives
	// cannot be combined. These pinned relaxed fixtures have one vector result.
	for _, pattern := range patterns {
		offset, matched := 0, true
		for _, value := range pattern {
			width := 8
			if value.Type == "v128" {
				width = 16
			}
			if width > len(got)-offset || Match(got[offset:offset+width], value) != nil {
				matched = false
				break
			}
			offset += width
		}
		if matched && offset == len(got) {
			return nil
		}
	}
	return fmt.Errorf("raw result %x matches no complete allowed pattern", got)
}
