//go:build linux && !tinygo

package simdfixture

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
		var v Value
		if err := Decode([]byte(raw), &v); err != nil {
			t.Fatal(err)
		}
		got, err := Input(v)
		if err != nil || Match(got, v) != nil {
			t.Fatal(raw, got, err)
		}
		got[0] ^= 1
		if Match(got, v) == nil {
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
		var value Value
		if err := Decode([]byte(golden.raw), &value); err != nil {
			t.Fatal(err)
		}
		got, err := Input(value)
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
			if !NaN(sign|exponent|quiet, bits, true) || !NaN(sign|exponent|quiet|1, bits, false) || NaN(sign|exponent|quiet|1, bits, true) || NaN(sign|exponent|1, bits, false) || NaN(sign|exponent, bits, false) || NaN(sign|quiet, bits, false) {
				t.Fatal("integer-only NaN class oracle", bits, sign)
			}
		}
	}
	v := Value{Type: "i32", Value: json.RawMessage(`"0"`)}
	if Match([]byte{0, 0, 0, 0, 1, 0, 0, 0}, v) == nil {
		t.Fatal("accepted raw upper result bits")
	}
	for _, raw := range []string{`{"type":"v128","lane_type":"i64","value":["0"]}`, `{"type":"v128","lane_type":"i8","value":["256"]}`, `{"type":"f32","value":"nan:arithmetic"}`, `{"type":"ref","value":"0"}`} {
		var value Value
		if err := Decode([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		if _, err := Input(value); err == nil {
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
	m := Manifest{Files: map[string]string{"fixture.wasm": hex.EncodeToString(hash[:])}}
	if _, err := m.Read(dir, "fixture.wasm", 32); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../fixture.wasm", "..", "", "missing.wasm"} {
		if _, err := m.Read(dir, name, 32); err == nil {
			t.Fatal("accepted bad fixture path", name)
		}
	}
	if _, err := m.Read(dir, "fixture.wasm", 2); err == nil {
		t.Fatal("ignored byte bound")
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture.wasm"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Read(dir, "fixture.wasm", 32); err == nil {
		t.Fatal("ignored changed fixture")
	}
	for _, raw := range []string{`{"unknown":true}`, `{} {}`, `{} trailing`} {
		var d Document
		if Decode([]byte(raw), &d) == nil {
			t.Fatal("accepted non-strict JSON", raw)
		}
	}
	for _, rows := range []map[string]string{{"../fixture.wasm": strings.Repeat("a", 64)}, {"fixture.wasm": strings.Repeat("z", 64)}, {"fixture.wasm": "00"}} {
		if _, err := HashRows(rows); err == nil {
			t.Fatal("accepted malformed manifest row", rows)
		}
	}
}

func TestSWARSpecEitherRequiresCompleteAllowedVector(t *testing.T) {
	var command Command
	if err := Decode([]byte(`{"type":"assert_return","either":[{"type":"v128","lane_type":"i64","value":["1","2"]},{"type":"v128","lane_type":"i64","value":["3","4"]}]}`), &command); err != nil {
		t.Fatal(err)
	}
	patterns, err := Patterns(command)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]uint64{{1, 2}, {3, 4}, {1, 4}, {3, 2}, {0, 0}} {
		var result [16]byte
		binary.LittleEndian.PutUint64(result[:], pair[0])
		binary.LittleEndian.PutUint64(result[8:], pair[1])
		allowed := pair == [2]uint64{1, 2} || pair == [2]uint64{3, 4}
		if (MatchPatterns(result[:], patterns) == nil) != allowed {
			t.Fatal("mixed-lane/entire-vector alternative oracle", pair)
		}
	}
	if MatchPatterns(make([]byte, 24), patterns) == nil || MatchPatterns(nil, patterns) == nil {
		t.Fatal("accepted wrong result width")
	}
	command.Expected = command.Either[:1]
	if _, err := Patterns(command); err == nil {
		t.Fatal("accepted both expected and either")
	}
	command.Expected = nil
	command.Type = "assert_trap"
	if _, err := Patterns(command); err == nil {
		t.Fatal("accepted trap alternatives")
	}
	command.Type = "assert_return"
	command.Either[1].Type = "i64"
	if _, err := Patterns(command); err == nil {
		t.Fatal("accepted inconsistent alternative types")
	}
	for _, raw := range []string{
		`{"type":"assert_return","either":[]}`,
		`{"type":"assert_return","expected":[],"either":[{"type":"i64","value":"1"}]}`,
	} {
		var malformed Command
		if err := Decode([]byte(raw), &malformed); err != nil {
			t.Fatal(err)
		}
		if _, err := Patterns(malformed); err == nil {
			t.Fatal("accepted empty-present alternative schema", raw)
		}
	}
}

func TestSWARSpecPreparedArtifactIntegrity(t *testing.T) {
	swarSpecPreparedArtifactIntegrity(t, "WAGO_RV64_SPEC_JSON_DIR", false)
}

func TestSWARSpecPreparedRelaxedArtifactIntegrity(t *testing.T) {
	swarSpecPreparedArtifactIntegrity(t, "WAGO_RV64_RELAXED_SPEC_JSON_DIR", true)
}

func swarSpecPreparedArtifactIntegrity(t *testing.T, variable string, relaxed bool) {
	t.Helper()
	dir := os.Getenv(variable)
	if dir == "" {
		t.Skip("set " + variable + " to check prepared pinned fixture bytes")
	}
	manifest, err := LoadManifest(dir, relaxed)
	if err != nil {
		t.Fatal(err)
	}
	for name := range manifest.Files {
		limit := int64(maxModuleBytes)
		if strings.HasSuffix(name, ".json") {
			limit = 16 << 20
		}
		if _, err := manifest.Read(dir, name, limit); err != nil {
			t.Fatal(err)
		}
	}
	// Even self-consistent replacement fixture digests must differ from the
	// reviewed pinned aggregate. This catches manifest edits and reductions.
	var source, artifact string
	for name := range manifest.Sources {
		source = name
		break
	}
	for name := range manifest.Files {
		artifact = name
		break
	}
	for _, change := range []func(*Manifest){
		func(m *Manifest) { m.SpecCommit = strings.Repeat("0", 40) },
		func(m *Manifest) { m.WabtVersion = "1.0.40" },
		func(m *Manifest) { m.WabtArchiveSHA256 = strings.Repeat("a", 64) },
		func(m *Manifest) { m.WabtBinarySHA256 = strings.Repeat("z", 64) },
		func(m *Manifest) { m.Sources[source] = strings.Repeat("a", 64) },
		func(m *Manifest) { m.Files[artifact] = strings.Repeat("a", 64) },
		func(m *Manifest) { delete(m.Sources, source) },
		func(m *Manifest) { delete(m.Files, artifact) },
	} {
		original, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		var altered Manifest
		if err := Decode(original, &altered); err != nil {
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
		if _, err := LoadManifest(candidate, relaxed); err == nil {
			t.Fatal("accepted replaced/abridged pinned manifest")
		}
	}
}
