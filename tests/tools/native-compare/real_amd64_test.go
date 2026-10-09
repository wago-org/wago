//go:build linux && amd64 && wago_profile

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/wago"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRealScalarSourceRegion(t *testing.T) {
	// Same integer hot-leaf shape as ALU workloads: two parameters, one XOR.
	// No memory, imports, loops, FP, SIMD or trap-producing operations.
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("xor", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x73, 0x0b}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var stats backend.ModuleStats
	cm, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, Profile: true, SourceMaps: true, DeferCodeMapping: true, Workers: 1, AMD64FeaturesSet: true, AMD64Features: 0})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	ordinary, err := backend.CompileModuleWith(m, backend.CompileOptions{DeferCodeMapping: true, Workers: 1, AMD64FeaturesSet: true, AMD64Features: 0})
	if err != nil {
		t.Fatal(err)
	}
	if ordinary.CodeImage != nil {
		defer ordinary.CodeImage.Close()
	}
	if hex.EncodeToString(cm.Code) != hex.EncodeToString(ordinary.Code) {
		t.Fatal("profiling changed fixture code; cannot substitute ordinary attribution")
	}
	binaryPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binaryBytes, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	s := Snapshot{Architecture: "amd64", Provenance: Provenance{"unit-test-fixture/source-revision-unattested", fmt.Sprintf("%x", sha256.Sum256(binaryBytes)), fmt.Sprintf("%x", sha256.Sum256(data)), fmt.Sprintf("configured-mask=00000000;required-mask=%08x", cm.RequiredAMD64Features), "explicit", "wago_profile", "established/direct-backend"}}
	path := filepath.Join(t.TempDir(), "code.bin")
	if err = os.WriteFile(path, cm.Code, 0600); err != nil {
		t.Fatal(err)
	}
	dump, err := exec.Command("objdump", "-D", "-b", "binary", "-m", "i386:x86-64", "-Mintel", "--insn-width=16", path).CombinedOutput()
	if err != nil {
		t.Fatalf("objdump %v: %s", err, dump)
	}
	var instructions []Instruction
	for _, line := range strings.Split(string(dump), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		offset, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 16, 64)
		if err != nil {
			continue
		}
		fields := strings.Fields(parts[1])
		var code string
		for _, field := range fields {
			if len(field) != 2 {
				break
			}
			if _, err := hex.DecodeString(field); err != nil {
				break
			}
			code += field
		}
		if code != "" {
			instructions = append(instructions, Instruction{offset, code, ""})
		}
	}
	var xorListing string
	for i, source := range stats.SourceRanges {
		pc := source.WasmOffset
		r := Region{ID: fmt.Sprintf("f%d.pc%d.region%d", source.Function, pc, i), Function: source.Function, WasmOffset: &pc}
		for _, in := range instructions {
			if in.Offset >= source.Offset && in.Offset+uint64(len(in.Hex)/2) <= source.Offset+source.Size {
				r.Instructions = append(r.Instructions, in)
			}
		}
		if len(r.Instructions) > 0 {
			s.Regions = append(s.Regions, r)
		}
	}
	if len(s.Regions) == 0 {
		t.Fatal("missing source regions")
	}
	found := false
	mutated := s
	mutated.Regions = append([]Region(nil), s.Regions...)
	for ri, r := range s.Regions {
		for ii, in := range r.Instructions {
			decoded, err := decode("amd64", in)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Opcode == "xor-register" {
				if r.WasmOffset == nil || *r.WasmOffset != 5 || r.Function != 0 {
					t.Fatalf("wrong XOR source attribution %+v", r)
				}
				for _, line := range strings.Split(string(dump), "\n") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						at, e := strconv.ParseUint(strings.TrimSpace(parts[0]), 16, 64)
						if e == nil && at == in.Offset && strings.Contains(line, "xor") {
							xorListing = line
						}
					}
				}
				if xorListing == "" {
					t.Fatal("decoder does not agree with human-readable objdump")
				}
				b := unhex(in.Hex)
				b[len(b)-1] ^= 8 // test-only same-opcode source-register change
				mutated.Regions[ri].Instructions = append([]Instruction(nil), r.Instructions...)
				mutated.Regions[ri].Instructions[ii].Hex = hex.EncodeToString(b)
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("missing mapped supported XOR")
	}
	report, err := Compare(s, mutated)
	if err != nil || len(report.Changes) != 1 {
		t.Fatal(report, err)
	}
	if report.Changes[0].Before.Opcode != "xor-register" || report.Changes[0].After.Opcode != "xor-register" {
		t.Fatal("control was not same-opcode")
	}

	// Execute only unmodified, validated Wasm through current Wago. The changed
	// diagnostic bytes above are never mapped or executed.
	compiled, err := wago.Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := wago.Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	pairs := [][2]int32{{0, 0}, {0, 1}, {42, 17}, {-1, 0}, {-2147483648, 2147483647}, {123456, -654321}}
	for _, pair := range pairs {
		out, err := instance.Invoke("xor", wago.I32(pair[0]), wago.I32(pair[1]))
		if err != nil || len(out) != 1 || int32(out[0]) != pair[0]^pair[1] {
			t.Fatalf("XOR oracle mismatch %v %v %v", pair, out, err)
		}
	}
	t.Logf("native bytes=%d; mapped regions=%d; compared=%d; known=%d; unknown=%d; complete=%v; trusted executions=%d; XOR=%s", len(cm.Code), len(s.Regions), report.Compared, report.Known, report.Unknown, report.Complete, len(pairs), xorListing)
	if dir := os.Getenv("WAGO_815_EVIDENCE_PATH"); dir != "" {
		for name, item := range map[string]any{"real-before.json": s, "real-after-control.json": mutated, "real-report.json": report, "real-source-ranges.json": stats.SourceRanges} {
			b, err := json.MarshalIndent(item, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
				t.Fatal(err)
			}
		}
		for name, b := range map[string][]byte{"real.objdump.txt": dump, "real.wasm": data, "real.native.bin": cm.Code} {
			if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Existing checked-in fib fixture gives honest coverage on a current workload;
// unknown instructions stay incomplete instead of expanding decoder claims.
func TestExistingFibSourceRegions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "wasm", "fib.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var stats backend.ModuleStats
	cm, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, Profile: true, SourceMaps: true, DeferCodeMapping: true, Workers: 1, AMD64FeaturesSet: true, AMD64Features: 0})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	executable, _ := os.Executable()
	executableBytes, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	s := Snapshot{Architecture: "amd64", Provenance: Provenance{"unit-test-fixture/source-revision-unattested", fmt.Sprintf("%x", sha256.Sum256(executableBytes)), fmt.Sprintf("%x", sha256.Sum256(data)), "configured-mask=00000000", "explicit", "wago_profile", "established/direct-backend"}}
	path := filepath.Join(t.TempDir(), "fib.bin")
	if err = os.WriteFile(path, cm.Code, 0600); err != nil {
		t.Fatal(err)
	}
	dump, err := exec.Command("objdump", "-D", "-b", "binary", "-m", "i386:x86-64", "-Mintel", "--insn-width=16", path).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	var instructions []Instruction
	for _, line := range strings.Split(string(dump), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		offset, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 16, 64)
		if err != nil {
			continue
		}
		var code string
		for _, field := range strings.Fields(parts[1]) {
			if len(field) != 2 {
				break
			}
			if _, err := hex.DecodeString(field); err != nil {
				break
			}
			code += field
		}
		if code != "" {
			instructions = append(instructions, Instruction{offset, code, ""})
		}
	}
	foundAdd := false
	for i, source := range stats.SourceRanges {
		pc := source.WasmOffset
		r := Region{fmt.Sprintf("f%d.pc%d.region%d", source.Function, pc, i), source.Function, &pc, nil}
		if int(source.Function) >= len(m.Code) || int(pc) < int(m.Code[source.Function].LocalDeclBytes) || int(pc) >= int(m.Code[source.Function].LocalDeclBytes)+len(m.Code[source.Function].BodyBytes) {
			t.Fatal("source offset outside function body")
		}
		for _, in := range instructions {
			if in.Offset >= source.Offset && in.Offset+uint64(len(in.Hex)/2) <= source.Offset+source.Size {
				r.Instructions = append(r.Instructions, in)
				decoded, _ := decode("amd64", in)
				if decoded.Opcode == "add-register" {
					if m.Code[source.Function].BodyBytes[int(pc)-int(m.Code[source.Function].LocalDeclBytes)] != 0x6a {
						t.Fatal("ADD source attribution disagrees with Wasm bytes")
					}
					foundAdd = true
				}
			}
		}
		if len(r.Instructions) > 0 {
			s.Regions = append(s.Regions, r)
		}
	}
	if !foundAdd {
		t.Fatal("missing independently mapped ADD")
	}
	r, err := Compare(s, s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Compared == 0 || r.Known == 0 || r.Unknown != 0 || !r.Complete || len(r.Changes) != 0 {
		t.Fatalf("expected complete mapped scalar coverage %+v", r)
	}
	t.Logf("current fib fixture: wasm=%d native=%d mapped regions=%d compared=%d known=%d unknown=%d complete=%v", len(data), len(cm.Code), len(s.Regions), r.Compared, r.Known, r.Unknown, r.Complete)
	if dir := os.Getenv("WAGO_815_EVIDENCE_PATH"); dir != "" {
		for name, item := range map[string]any{"fib-snapshot.json": s, "fib-report.json": r, "fib-source-ranges.json": stats.SourceRanges} {
			b, err := json.MarshalIndent(item, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err = os.WriteFile(filepath.Join(dir, "fib.objdump.txt"), dump, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
