package wagobench

// Standalone exec bench for Impart's real libinjection SQLi detector
// (experimental/jack/libinjection/compare-build/sqli.wasm) — scalar AS byte code,
// the actual "rules" workload. Drives isSQLiBool over a fixed input to A/B wago
// codegen changes without the asbuilder/node pipeline.

import (
	"os"
	"testing"

	wago "github.com/wago-org/wago"
)

// sqliWasmPath points at Impart's prebuilt libinjection SQLi detector. Override
// with WAGO_SQLI_WASM; the bench/tests skip if it is not present.
func sqliWasmPath() string {
	if p := os.Getenv("WAGO_SQLI_WASM"); p != "" {
		return p
	}
	return "/home/hub/Code/Impart/impart/experimental/jack/libinjection/compare-build/sqli.wasm"
}

// asStringID is AssemblyScript's idof<string>, calibrated empirically by
// TestSqliCalibrate.
var asStringID = uint64(2)

func loadSqli(tb testing.TB) *wago.Instance {
	src, err := os.ReadFile(sqliWasmPath())
	if err != nil {
		tb.Skipf("sqli.wasm not present: %v", err)
	}
	c, err := wago.Compile(src)
	if err != nil {
		tb.Fatalf("compile: %v", err)
	}
	imp := wago.Imports{"env.abort": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})}
	in, err := wago.Instantiate(c, imp)
	if err != nil {
		tb.Fatalf("instantiate: %v", err)
	}
	return in
}

// newASString allocates an AssemblyScript string holding s (ASCII → UTF-16) and
// returns its pointer.
func newASString(tb testing.TB, in *wago.Instance, s string) uint64 {
	r, err := in.Invoke("__new", uint64(len(s)*2), asStringID)
	if err != nil {
		tb.Fatalf("__new: %v", err)
	}
	ptr := r[0]
	mem := in.Memory().Bytes()
	for i := 0; i < len(s); i++ {
		mem[ptr+uint64(2*i)] = s[i]
		mem[ptr+uint64(2*i)+1] = 0
	}
	return ptr
}

func isSQLi(tb testing.TB, in *wago.Instance, ptr uint64) bool {
	r, err := in.Invoke("isSQLiBool", ptr)
	if err != nil {
		tb.Fatalf("isSQLiBool: %v", err)
	}
	return r[0] != 0
}

// TestSqliCalibrate finds the string id that makes detection correct, and prints
// it. Run: go test -run TestSqliCalibrate -v .
func TestSqliCalibrate(t *testing.T) {
	src, err := os.ReadFile(sqliWasmPath())
	if err != nil {
		t.Skipf("sqli.wasm not present: %v", err)
	}
	c, _ := wago.Compile(src)
	for _, id := range []uint64{1, 2, 3, 4, 5} {
		imp := wago.Imports{"env.abort": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})}
		in, err := wago.Instantiate(c, imp)
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		asStringID = id
		attack := newASString(t, in, "1' OR '1'='1' -- ")
		benign := newASString(t, in, "the quick brown fox jumps over the lazy dog")
		gotAttack := isSQLi(t, in, attack)
		gotBenign := isSQLi(t, in, benign)
		t.Logf("id=%d  attack=%v benign=%v  %s", id, gotAttack, gotBenign,
			map[bool]string{true: "<-- CORRECT", false: ""}[gotAttack && !gotBenign])
	}
}

// benchInputs: a benign string (the slower full-scan path per RESULTS.md) and an
// attack. Fixed so codegen A/B is apples-to-apples.
var benignInput = "SELECT the quick brown fox from the lazy dog where id equals fortytwo and name is bob"

func BenchmarkSqliBenign(b *testing.B) {
	in := loadSqli(b)
	ptr := newASString(b, in, benignInput)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := in.Invoke("isSQLiBool", ptr)
		if err != nil {
			b.Fatal(err)
		}
	}
}
