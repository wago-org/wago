//go:build linux && !tinygo

package riscv64

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
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Test-only WABT JSON transport. Public SIMD admission is unchanged. Prepare
// fixtures with scripts/prepare-riscv64-simd-spec.py; the native opt-in test
// reports execution coverage separately from validation/linking exclusions.
const swarSpecCommit = "05ca4182176763112561ae20153975c12bd689e4"

// Hash sorted basename + NUL + SHA256 + newline rows. Prepared JSON uses source
// basenames rather than absolute checkout paths. Binary-tool hashes remain
// platform-specific; these fixture aggregates are reproducible across hosts.
const swarSpecSourcesSHA256 = "b8892140ce454ba04d801cda7084087138aec58ba3817a7280869c76afe8350f"
const swarSpecFilesSHA256 = "1ab236a3015f301d9baf0e38954172b9f2e8377e490cd1ca8d2f7b495eb3a456"

type swarSpecManifest struct {
	SpecCommit        string            `json:"spec_commit"`
	WabtVersion       string            `json:"wabt_version"`
	WabtArchiveSHA256 string            `json:"wabt_archive_sha256"`
	WabtBinarySHA256  string            `json:"wabt_binary_sha256"`
	Sources           map[string]string `json:"sources"`
	Files             map[string]string `json:"files"`
}

type swarSpecDocument struct {
	SourceFilename string            `json:"source_filename"`
	Commands       []swarSpecCommand `json:"commands"`
}

type swarSpecCommand struct {
	Type       string          `json:"type"`
	Line       int             `json:"line"`
	Filename   string          `json:"filename"`
	ModuleType string          `json:"module_type"`
	As         string          `json:"as"`
	Text       string          `json:"text"`
	Action     swarSpecAction  `json:"action"`
	Expected   []swarSpecValue `json:"expected"`
}

type swarSpecAction struct {
	Type  string          `json:"type"`
	Field string          `json:"field"`
	Args  []swarSpecValue `json:"args"`
}

type swarSpecValue struct {
	Type     string          `json:"type"`
	LaneType string          `json:"lane_type"`
	Value    json.RawMessage `json:"value"`
}

func swarSpecDecode(data []byte, out any) error {
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

func swarSpecRead(dir, name string, limit int64) ([]byte, error) {
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

func (m *swarSpecManifest) read(dir, name string, limit int64) ([]byte, error) {
	want, ok := m.Files[name]
	if !ok {
		return nil, fmt.Errorf("fixture missing from manifest: %s", name)
	}
	data, err := swarSpecRead(dir, name, limit)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != want {
		return nil, fmt.Errorf("fixture digest mismatch: %s", name)
	}
	return data, nil
}

func swarSpecHashRows(rows map[string]string) (string, error) {
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

func swarSpecLoadManifest(dir string) (*swarSpecManifest, error) {
	data, err := swarSpecRead(dir, "rv64-simd-manifest.json", 2<<20)
	if err != nil {
		return nil, err
	}
	var m swarSpecManifest
	if err := swarSpecDecode(data, &m); err != nil {
		return nil, err
	}
	archives := map[string]bool{
		"83f8122e924745fcd70636e3594bc01c4c47f2d4c8f3c63b5d70d3f83a482677": true, // Linux amd64
		"5e35416ee8725dc7cc0572e4392a8117cbf008b0e34c0db65c75506b0299cdbf": true, // Linux arm64
		"e5269d6bbe05dfeb179e4f21111b3a641d6ccaa38b0b21d472ae5c65f8c4ff5d": true, // Darwin arm64
		"ca9e69cc1de13b4633a3c74fd697319303b21108529d4f10960af4e1f4a65893": true, // Darwin amd64 source
	}
	binaryHash, hashErr := hex.DecodeString(m.WabtBinarySHA256)
	if m.SpecCommit != swarSpecCommit || m.WabtVersion != "1.0.41" || !archives[m.WabtArchiveSHA256] || hashErr != nil || len(binaryHash) != sha256.Size || len(m.Sources) != 57 || len(m.Files) != 1708 {
		return nil, fmt.Errorf("unexpected pinned source/tool provenance")
	}
	for _, expected := range []struct {
		rows map[string]string
		hash string
	}{{m.Sources, swarSpecSourcesSHA256}, {m.Files, swarSpecFilesSHA256}} {
		actual, err := swarSpecHashRows(expected.rows)
		if err != nil || actual != expected.hash {
			return nil, fmt.Errorf("pinned fixture manifest differs: got %s, want %s (%v)", actual, expected.hash, err)
		}
	}
	return &m, nil
}

func swarSpecLayout(v swarSpecValue) (wasm.ValType, int, int, bool) {
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

func swarSpecLanes(v swarSpecValue) ([]string, error) {
	_, bits, count, _ := swarSpecLayout(v)
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

func swarSpecBits(value string, bits int) (uint64, error) {
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

func swarSpecInput(v swarSpecValue) ([]byte, error) {
	values, err := swarSpecLanes(v)
	if err != nil {
		return nil, err
	}
	_, bits, _, _ := swarSpecLayout(v)
	n := 8
	if v.Type == "v128" {
		n = 16
	}
	result := make([]byte, n)
	for i, value := range values {
		lane, err := swarSpecBits(value, bits)
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

func swarSpecNaN(lane uint64, bits int, canonical bool) bool {
	exponent, quiet, payload := uint64(0x7ff0000000000000), uint64(0x8000000000000), uint64(0xfffffffffffff)
	if bits == 32 {
		exponent, quiet, payload = 0x7f800000, 0x400000, 0x7fffff
	} else if bits != 64 {
		return false
	}
	return lane&exponent == exponent && (canonical && lane&payload == quiet || !canonical && lane&quiet != 0)
}

func swarSpecMatch(got []byte, v swarSpecValue) error {
	values, err := swarSpecLanes(v)
	if err != nil {
		return err
	}
	_, bits, _, floating := swarSpecLayout(v)
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
			if swarSpecNaN(lane, bits, value == "nan:canonical") {
				continue
			}
			return fmt.Errorf("lane %d: %x is not %s", i, lane, value)
		}
		want, err := swarSpecBits(value, bits)
		if err != nil {
			return err
		}
		if lane != want {
			return fmt.Errorf("lane %d: got %x, want %x", i, lane, want)
		}
	}
	return nil
}

func TestSWARSpecTransportExactBitsAndNaNClasses(t *testing.T) {
	for _, raw := range []string{
		`{"type":"i32","value":"4294967295"}`,
		`{"type":"i64","value":"-9223372036854775808"}`,
		`{"type":"f32","value":"2141266757"}`,
		`{"type":"f64","value":"18442240474082181121"}`,
		`{"type":"v128","lane_type":"i8","value":["-1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16"]}`,
		`{"type":"v128","lane_type":"i16","value":["65535","-32768","2","3","4","5","6","7"]}`,
		`{"type":"v128","lane_type":"f32","value":["0","2147483648","2141266757","4290772993"]}`,
		`{"type":"v128","lane_type":"f64","value":["18442240474082181121","9223372036854775808"]}`,
	} {
		var v swarSpecValue
		if err := swarSpecDecode([]byte(raw), &v); err != nil {
			t.Fatal(err)
		}
		got, err := swarSpecInput(v)
		if err != nil || swarSpecMatch(got, v) != nil {
			t.Fatal(raw, got, err)
		}
		got[0] ^= 1
		if swarSpecMatch(got, v) == nil {
			t.Fatal("accepted altered raw lane", raw)
		}
	}
	// Independent byte goldens exercise lane order and signed-lane masking,
	// rather than relying only on the input/output parser round trip above.
	for _, golden := range []struct{ raw, bytes string }{
		{`{"type":"i32","value":"4294967295"}`, "ffffffff00000000"},
		{`{"type":"i64","value":"-9223372036854775808"}`, "0000000000000080"},
		{`{"type":"v128","lane_type":"i64","value":["-1","72623859790382856"]}`, "ffffffffffffffff0807060504030201"},
		{`{"type":"v128","lane_type":"i32","value":["-1","-2147483648","16909060","5"]}`, "ffffffff000000800403020105000000"},
	} {
		var value swarSpecValue
		if err := swarSpecDecode([]byte(golden.raw), &value); err != nil {
			t.Fatal(err)
		}
		got, err := swarSpecInput(value)
		if err != nil || hex.EncodeToString(got) != golden.bytes {
			t.Fatal("exact little-endian lane golden", golden, got, err)
		}
	}
	for _, bits := range []int{32, 64} {
		quiet, exponent := uint64(0x400000), uint64(0x7f800000)
		if bits == 64 {
			quiet, exponent = 0x8000000000000, 0x7ff0000000000000
		}
		for _, sign := range []uint64{0, uint64(1) << (bits - 1)} {
			if !swarSpecNaN(sign|exponent|quiet, bits, true) || !swarSpecNaN(sign|exponent|quiet|1, bits, false) || swarSpecNaN(sign|exponent|quiet|1, bits, true) || swarSpecNaN(sign|exponent|1, bits, false) || swarSpecNaN(sign|exponent, bits, false) || swarSpecNaN(sign|quiet, bits, false) {
				t.Fatal("integer-only NaN class oracle", bits, sign)
			}
		}
	}
	v := swarSpecValue{Type: "i32", Value: json.RawMessage(`"0"`)}
	if swarSpecMatch([]byte{0, 0, 0, 0, 1, 0, 0, 0}, v) == nil {
		t.Fatal("accepted raw upper result bits")
	}
	for _, raw := range []string{`{"type":"v128","lane_type":"i64","value":["0"]}`, `{"type":"v128","lane_type":"i8","value":["256"]}`, `{"type":"f32","value":"nan:arithmetic"}`, `{"type":"ref","value":"0"}`} {
		var value swarSpecValue
		if err := swarSpecDecode([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		if _, err := swarSpecInput(value); err == nil {
			t.Fatal("accepted invalid input", raw)
		}
	}
}

func TestSWARSpecFixtureIntegrityAndStrictJSON(t *testing.T) {
	dir := t.TempDir()
	data := []byte("wasm fixture")
	if err := os.WriteFile(filepath.Join(dir, "fixture.wasm"), data, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	m := swarSpecManifest{Files: map[string]string{"fixture.wasm": hex.EncodeToString(hash[:])}}
	if _, err := m.read(dir, "fixture.wasm", 32); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../fixture.wasm", "..", "", "missing.wasm"} {
		if _, err := m.read(dir, name, 32); err == nil {
			t.Fatal("accepted bad fixture path", name)
		}
	}
	if _, err := m.read(dir, "fixture.wasm", 2); err == nil {
		t.Fatal("ignored byte bound")
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture.wasm"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.read(dir, "fixture.wasm", 32); err == nil {
		t.Fatal("ignored changed fixture")
	}
	for _, raw := range []string{`{"unknown":true}`, `{} {}`, `{} trailing`} {
		var d swarSpecDocument
		if swarSpecDecode([]byte(raw), &d) == nil {
			t.Fatal("accepted non-strict JSON", raw)
		}
	}
	for _, rows := range []map[string]string{{"../fixture.wasm": strings.Repeat("a", 64)}, {"fixture.wasm": strings.Repeat("z", 64)}, {"fixture.wasm": "00"}} {
		if _, err := swarSpecHashRows(rows); err == nil {
			t.Fatal("accepted malformed manifest row", rows)
		}
	}
}

func TestSWARSpecPreparedArtifactIntegrity(t *testing.T) {
	dir := os.Getenv("WAGO_RV64_SPEC_JSON_DIR")
	if dir == "" {
		t.Skip("set WAGO_RV64_SPEC_JSON_DIR to check prepared pinned fixture bytes")
	}
	manifest, err := swarSpecLoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name := range manifest.Files {
		limit := int64(MaxModuleBytes)
		if strings.HasSuffix(name, ".json") {
			limit = 16 << 20
		}
		if _, err := manifest.read(dir, name, limit); err != nil {
			t.Fatal(err)
		}
	}
	// Even self-consistent replacement fixture digests must differ from the
	// reviewed pinned aggregate. This catches manifest edits and reductions.
	for _, change := range []func(*swarSpecManifest){
		func(m *swarSpecManifest) { m.SpecCommit = strings.Repeat("0", 40) },
		func(m *swarSpecManifest) { m.WabtVersion = "1.0.40" },
		func(m *swarSpecManifest) { m.WabtArchiveSHA256 = strings.Repeat("a", 64) },
		func(m *swarSpecManifest) { m.WabtBinarySHA256 = strings.Repeat("z", 64) },
		func(m *swarSpecManifest) { m.Sources["simd_load.wast"] = strings.Repeat("a", 64) },
		func(m *swarSpecManifest) { m.Files["simd_load.json"] = strings.Repeat("a", 64) },
		func(m *swarSpecManifest) { delete(m.Sources, "simd_load.wast") },
		func(m *swarSpecManifest) { delete(m.Files, "simd_load.json") },
	} {
		original, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		var altered swarSpecManifest
		if err := swarSpecDecode(original, &altered); err != nil {
			t.Fatal(err)
		}
		change(&altered)
		data, err := json.Marshal(altered)
		if err != nil {
			t.Fatal(err)
		}
		candidate := t.TempDir()
		if err := os.WriteFile(filepath.Join(candidate, "rv64-simd-manifest.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := swarSpecLoadManifest(candidate); err == nil {
			t.Fatal("accepted replaced/abridged pinned manifest")
		}
	}
}
