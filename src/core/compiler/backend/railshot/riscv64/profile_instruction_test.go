//go:build linux && !tinygo && wago_profile

package riscv64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/internal/jitprofile"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"testing"
)

func instructionProfileFixture(t *testing.T, code []byte) ([]jitprofile.CodeSite, []jitprofile.UnwindRange) {
	t.Helper()
	sites, ranges, err := profileInstructionMetadata(code, []FunctionInfo{{CodeBytes: len(code), FrameBytes: 32}}, []jitprofile.Region{{Offset: 0, Size: uint64(len(code)), Function: 0, Kind: "guest-body"}}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	return sites, ranges
}
func TestProfileInstructionUnwindCallerRAAndOverwrite(t *testing.T) {
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -32)
	a.Sd(enc.RA, enc.SP, 0)
	a.Addi(enc.X6, enc.Zero, 17)
	call := a.JumpLong(true)
	a.Sd(enc.RA, enc.SP, 8)
	afterCalleeRA := a.Len()
	a.Addi(enc.X6, enc.X6, 1)
	a.Sd(enc.Zero, enc.SP, 0)
	afterOverwrite := a.Len()
	a.Addi(enc.X6, enc.X6, 1)
	a.Ret()
	if err := a.PatchLong(call, 0); err != nil {
		t.Fatal(err)
	}
	sites, ranges := instructionProfileFixture(t, a.B)
	if len(sites) == 0 || len(ranges) == 0 {
		t.Fatal("empty measured instruction metadata")
	}
	rule, ok := jitprofile.LookupUnwind(ranges, uint64(afterCalleeRA))
	if !ok || rule.CFARegister != 2 || rule.CFAOffset != 32 || rule.ReturnOffset != -32 {
		t.Fatal("callee RA replaced caller proof", rule, ok)
	}
	if _, ok := jitprofile.LookupUnwind(ranges, uint64(afterOverwrite)); ok {
		t.Fatal("overwritten caller RA kept an unwind rule")
	}
	if _, ok := jitprofile.LookupUnwind(ranges, 0); ok {
		t.Fatal("register-held return address claimed stack recovery")
	}
}
func TestProfileInstructionUnwindMergeAndEpilogue(t *testing.T) {
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -32)
	a.Sd(enc.RA, enc.SP, 0)
	branch := a.BranchLong(enc.EQ, enc.X6, enc.Zero)
	a.Addi(enc.SP, enc.SP, -16)
	join := a.Len()
	a.Addi(enc.X7, enc.Zero, 1)
	a.Ret()
	if err := a.PatchLong(branch, int64(join)); err != nil {
		t.Fatal(err)
	}
	_, ranges := instructionProfileFixture(t, a.B)
	if _, ok := jitprofile.LookupUnwind(ranges, uint64(join)); ok {
		t.Fatal("mismatched incoming stack rules merged")
	}
	var b enc.Asm
	b.Addi(enc.SP, enc.SP, -32)
	b.Sd(enc.RA, enc.SP, 0)
	b.Ld(enc.RA, enc.SP, 0)
	restore := b.Len()
	b.Addi(enc.SP, enc.SP, 32)
	b.Ret()
	_, ranges = instructionProfileFixture(t, b.B)
	if _, ok := jitprofile.LookupUnwind(ranges, uint64(restore)); !ok {
		t.Fatal("epilogue SP restore lost preceding rule")
	}
	if _, ok := jitprofile.LookupUnwind(ranges, uint64(restore+4)); ok {
		t.Fatal("released frame claimed stack recovery")
	}
}
func TestProductionProfileUnwindAndSitesDoNotChangeCode(t *testing.T) {
	m := statsModule(t, binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}}))
	plain, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true})
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	profile, err := CompileModuleWith(m, CompileOptions{Profile: true, SourceMaps: true, UnwindMaps: true, Stats: &stats, DeferCodeMapping: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain.Code, profile.Code) {
		t.Fatal("metadata changed code")
	}
	if len(stats.UnwindRanges) == 0 || len(stats.CodeSites) == 0 {
		t.Fatal("missing emitted instruction proofs")
	}
	for _, r := range stats.UnwindRanges {
		if r.CFARegister != 2 || r.ReturnOffset >= 0 {
			t.Fatal("invalid RV64 DWARF rule", r)
		}
	}
}

func BenchmarkProfileInstructionMetadata(b *testing.B) {
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -64)
	a.Sd(enc.RA, enc.SP, 0)
	for i := 0; i < 4096; i++ {
		a.Lwu(enc.X6, enc.SP, 16)
		a.Addi(enc.X6, enc.X6, 1)
		a.Sw(enc.X6, enc.SP, 16)
	}
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 64)
	a.Ret()
	functions := []FunctionInfo{{CodeBytes: len(a.B), FrameBytes: 64}}
	regions := []jitprofile.Region{{Offset: 0, Size: uint64(len(a.B)), Function: 0, Kind: "guest-body"}}
	b.ReportAllocs()
	b.SetBytes(int64(len(a.B)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sites, ranges, err := profileInstructionMetadata(a.B, functions, regions, true, true)
		if err != nil || len(sites) == 0 || len(ranges) == 0 {
			b.Fatal("missing measured metadata", err)
		}
	}
}

func TestProfileInstructionFloatStoreInvalidatesCallerRA(t *testing.T) {
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -32)
	a.Sd(enc.RA, enc.SP, 0)
	// FSD f1,0(SP) overlaps the saved integer return address.
	var w [4]byte
	binary.LittleEndian.PutUint32(w[:], 1<<20|2<<15|3<<12|0x27)
	a.B = append(a.B, w[:]...)
	after := a.Len()
	a.Addi(enc.X6, enc.Zero, 1)
	a.Ret()
	sites, ranges := instructionProfileFixture(t, a.B)
	if _, ok := jitprofile.LookupUnwind(ranges, uint64(after)); ok {
		t.Fatal("float overwrite retained integer return proof")
	}
	if len(sites) != 2 || sites[1].Kind != "frame-store" {
		t.Fatal("float frame store missing", sites)
	}
}

func TestProfileInstructionUnrequestedMetadataDoesNotAllocate(t *testing.T) {
	code := []byte{0x67, 0x80, 0, 0}
	functions := []FunctionInfo{{CodeBytes: len(code)}}
	if alloc := testing.AllocsPerRun(32, func() {
		sites, ranges, err := profileInstructionMetadata(code, functions, nil, false, false)
		if err != nil || sites != nil || ranges != nil {
			panic("unrequested metadata")
		}
	}); alloc != 0 {
		t.Fatal("unrequested instruction proof storage", alloc)
	}
}

func TestProfileInstructionUnknownStoreCannotClaimSavedRA(t *testing.T) {
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -32)
	a.Sd(enc.RA, enc.SP, 0)
	a.Addi(enc.X6, enc.SP, 0)
	a.Sw(enc.Zero, enc.X6, 0)
	after := a.Len()
	a.Addi(enc.X7, enc.Zero, 1)
	a.Ret()
	_, ranges := instructionProfileFixture(t, a.B)
	if _, ok := jitprofile.LookupUnwind(ranges, uint64(after)); ok {
		t.Fatal("unknown aliasing write retained return proof")
	}
}
