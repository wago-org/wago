package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(arch, code string) Snapshot {
	pc := uint32(5)
	return Snapshot{nil, arch, Provenance{"rev", strings.Repeat("a", 64), strings.Repeat("b", 64), "baseline", "explicit", "ordinary", "established"}, []Region{{"f0.pc5", 0, &pc, []Instruction{{0, code, ""}}, 0}}, nil, nil}
}
func TestPairedControls(t *testing.T) {
	for _, c := range controls {
		t.Run(c.name, func(t *testing.T) {
			a, b := fixture("amd64", c.a), fixture("amd64", c.b)
			if c.name == "relocation" {
				a.Regions[0].Instructions[0].Relocation = "f1"
				b.Regions[0].Instructions[0].Relocation = "f1"
			}
			r, err := Compare(a, b)
			if err != nil {
				t.Fatal(err)
			}
			if !r.Complete || r.Known != 1 {
				t.Fatalf("control unqualified: %+v", r)
			}
			want := 1
			if c.name == "relocation" {
				want = 0
			}
			if len(r.Changes) != want {
				t.Fatalf("changes=%d want %d: %+v", len(r.Changes), want, r)
			}
			if c.name == "zero-idiom-dependency" && (r.Changes[0].Before.ReadCount != 0 || r.Changes[0].After.ReadCount != 2) {
				t.Fatal("dependency facts lost")
			}
			if c.name == "spill-width" && (r.Changes[0].Before.Width != 32 || r.Changes[0].After.Width != 64) {
				t.Fatal("width lost")
			}
			if c.name == "relocation" {
				ar, _ := decode("amd64", a.Regions[0].Instructions[0])
				br, _ := decode("amd64", b.Regions[0].Instructions[0])
				if ar.Raw != c.a || br.Raw != c.b {
					t.Fatal("raw bytes lost")
				}
			}
			t.Logf("opcode-only equal; operand changes=%d, complete=%v", len(r.Changes), r.Complete)
		})
	}
}
func TestARM64Controls(t *testing.T) {
	cases := []struct {
		name, a, b string
		reloc      bool
	}{
		{"constant", "40058052", "60058052", false},    // mov w0,#42 / #43
		{"spill-width", "e00b00b9", "e00b00f9", false}, // str w0 / x0 with scaled displacement
		{"load-width", "e00b40b9", "e00b40f9", false},
		{"add-constant", "00040011", "00080011", false},
		{"register-read", "00040011", "20040011", false},
		{"branch-relocation", "01000014", "02000014", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b := fixture("arm64", c.a), fixture("arm64", c.b)
			if c.reloc {
				a.Regions[0].Instructions[0].Relocation = "f1"
				b.Regions[0].Instructions[0].Relocation = "f1"
			}
			r, err := Compare(a, b)
			if err != nil {
				t.Fatal(err)
			}
			if !r.Complete || r.Known != 1 {
				t.Fatalf("not known %+v", r)
			}
			want := 1
			if c.reloc {
				want = 0
			}
			if len(r.Changes) != want {
				t.Fatalf("%+v", r)
			}
		})
	}
	r, _ := decode("arm64", Instruction{0, "e00b00b9", ""})
	if !r.Address.SP || r.Address.Base != 31 {
		t.Fatalf("SP lost %+v", r)
	}
}
func TestRelocationRequiresMatchingIdentityAndBranch(t *testing.T) {
	a, b := fixture("amd64", "e901000000"), fixture("amd64", "e902000000")
	a.Regions[0].Instructions[0].Relocation = "f1"
	b.Regions[0].Instructions[0].Relocation = "f2"
	r, err := Compare(a, b)
	if err != nil || len(r.Changes) != 1 {
		t.Fatal(r, err)
	}
	b.Regions[0].Instructions[0].Relocation = ""
	r, err = Compare(a, b)
	if err != nil || len(r.Changes) != 1 {
		t.Fatal(r, err)
	}
	for _, s := range []Snapshot{fixture("amd64", "b82a000000"), fixture("amd64", "e801000000"), fixture("arm64", "01000094")} {
		s.Regions[0].Instructions[0].Relocation = "f1"
		if _, err := Compare(s, s); err == nil {
			t.Fatal("unadmitted relocation accepted")
		}
	}
}
func TestIncompleteCoverageAndConfiguration(t *testing.T) {
	for _, name := range []string{"unknown", "unmapped", "missing-provenance", "wrong-config", "missing-region", "count-mismatch", "wrong-anchor"} {
		t.Run(name, func(t *testing.T) {
			a, b := fixture("amd64", "31c0"), fixture("amd64", "31c0")
			switch name {
			case "unknown":
				a = fixture("amd64", "c3")
				b = fixture("amd64", "c3")
			case "unmapped":
				a.Regions[0].WasmOffset = nil
				b.Regions[0].WasmOffset = nil
			case "missing-provenance":
				a.Provenance.BinarySHA256 = ""
			case "wrong-config":
				b.Provenance.Path = "shared"
			case "missing-region":
				b.Regions = append(b.Regions, Region{"other", 1, b.Regions[0].WasmOffset, []Instruction{{2, "31c0", ""}}, 0})
			case "count-mismatch":
				b.Regions[0].Instructions = append(b.Regions[0].Instructions, Instruction{2, "31c0", ""})
			case "wrong-anchor":
				b.Regions[0].ID = "other"
			}
			r, err := Compare(a, b)
			if err != nil {
				t.Fatal(err)
			}
			if r.Complete {
				t.Fatal("false complete", r)
			}
		})
	}
}
func TestResourceAndInputLimits(t *testing.T) {
	s := fixture("amd64", "31c0")
	for i := 1; i <= maxInstructions; i++ {
		s.Regions[0].Instructions = append(s.Regions[0].Instructions, Instruction{uint64(i * 2), "31c0", ""})
	}
	if _, err := Compare(s, s); err == nil {
		t.Fatal("instruction budget")
	}
	a, b := fixture("amd64", "31c0"), fixture("amd64", "31c8")
	for i := 1; i <= maxChanges; i++ {
		a.Regions[0].Instructions = append(a.Regions[0].Instructions, Instruction{uint64(i * 2), "31c0", ""})
		b.Regions[0].Instructions = append(b.Regions[0].Instructions, Instruction{uint64(i * 2), "31c8", ""})
	}
	r, err := Compare(a, b)
	if err != nil || !r.Limit || r.Complete || len(r.Changes) != maxChanges {
		t.Fatal(r, err)
	}
	path := filepath.Join(t.TempDir(), "input.json")
	if err = os.WriteFile(path, bytes.Repeat([]byte{' '}, maxInputBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readSnapshot(path); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatal(err)
	}
	for _, payload := range []string{"{} {}", `{"oops":1}`} {
		if err = os.WriteFile(path, []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = readSnapshot(path); err == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
	s = fixture("amd64", "31c0")
	s.Regions = append(s.Regions, Region{"overlap", 1, s.Regions[0].WasmOffset, []Instruction{{0, "31c0", ""}}, 0})
	if _, err = Compare(s, s); err == nil {
		t.Fatal("overlap accepted")
	}
}
func TestSnapshotReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	original := fixture("amd64", "b82a000000")
	data, _ := json.Marshal(original)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := readSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Compare(original, loaded)
	if err != nil || !r.Complete || len(r.Changes) != 0 {
		t.Fatal(r, err)
	}
}
func BenchmarkCompareScalarRegions(b *testing.B) {
	for _, size := range []int{16, 256, 4096} {
		b.Run(stringSize(size), func(b *testing.B) {
			a, c := fixture("amd64", "31c0"), fixture("amd64", "31c0")
			for i := 1; i < size; i++ {
				a.Regions[0].Instructions = append(a.Regions[0].Instructions, Instruction{uint64(i * 2), "31c0", ""})
				c.Regions[0].Instructions = append(c.Regions[0].Instructions, Instruction{uint64(i * 2), "31c0", ""})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r, err := Compare(a, c)
				if err != nil || !r.Complete {
					b.Fatal(r, err)
				}
			}
		})
	}
}
func stringSize(n int) string { return fmt.Sprint(n) }

func BenchmarkCompareChangedMemory(b *testing.B) {
	a, c := fixture("amd64", "89442408"), fixture("amd64", "4889442408")
	for i := 1; i < 32; i++ {
		a.Regions[0].Instructions = append(a.Regions[0].Instructions, Instruction{uint64(i * 4), "89442408", ""})
		c.Regions[0].Instructions = append(c.Regions[0].Instructions, Instruction{uint64(i * 5), "4889442408", ""})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := Compare(a, c)
		if err != nil || !r.Complete || len(r.Changes) != 32 {
			b.Fatal(r, err)
		}
	}
}

func TestMalformedShapesAndUnknownPartialWrites(t *testing.T) {
	for _, name := range []string{"architecture", "long-instruction", "bad-hex", "gap", "region-budget", "unaligned-arm"} {
		t.Run(name, func(t *testing.T) {
			s := fixture("amd64", "31c0")
			switch name {
			case "architecture":
				s.Architecture = "riscv64"
			case "long-instruction":
				s.Regions[0].Instructions[0].Hex = strings.Repeat("90", 16)
			case "bad-hex":
				s.Regions[0].Instructions[0].Hex = "zz"
			case "gap":
				s.Regions[0].Instructions = append(s.Regions[0].Instructions, Instruction{3, "31c0", ""})
			case "region-budget":
				for i := 1; i <= maxRegions; i++ {
					pc := uint32(i)
					s.Regions = append(s.Regions, Region{fmt.Sprint(i), uint32(i), &pc, []Instruction{{uint64(i * 2), "31c0", ""}}, 0})
				}
			case "unaligned-arm":
				s = fixture("arm64", "01000014")
				s.Regions[0].Instructions[0].Offset = 1
			}
			if _, err := Compare(s, s); err == nil {
				t.Fatal("malformed shape admitted")
			}
		})
	}
	for _, hex := range []string{"6689c0", "88c0", "b82a", "e801000000"} {
		s := fixture("amd64", hex)
		r, err := Compare(s, s)
		if err != nil || r.Complete || r.Unknown != 1 {
			t.Fatal(r, err)
		}
	}
}

func TestExtendedScalarFacts(t *testing.T) {
	for _, c := range []struct {
		hex   string
		check func(Record) bool
	}{
		{"4439d7", func(r Record) bool {
			return r.Opcode == "cmp-register" && r.Reads[0].Number == 7 && r.Reads[1].Number == 10 && r.ReadCount == 2 && r.WriteCount == 0 && r.FlagsWrite && !r.ZeroExtends
		}},
		{"4183c201", func(r Record) bool {
			return r.Opcode == "add-immediate" && r.Immediate == 1 && r.ImmediateWidth == 8 && r.Writes[0].Number == 10 && r.ZeroExtends && r.FlagsWrite
		}},
		{"4883c0ff", func(r Record) bool { return r.Immediate == ^uint64(0) && r.Width == 64 && !r.ZeroExtends }},
		{"83c0ff", func(r Record) bool { return r.Immediate == 0xffffffff && r.Width == 32 }},
		{"4881f8ffffffff", func(r Record) bool {
			return r.Opcode == "cmp-immediate" && r.Immediate == ^uint64(0) && r.WriteCount == 0 && !r.ZeroExtends
		}},
		{"8b3c24", func(r Record) bool {
			return r.Address.Present && r.Address.Base == 4 && r.Address.Displacement == 0 && r.Writes[0].Number == 7 && r.Reads[0].Width == 64
		}},
		{"0f8e12000000", func(r Record) bool {
			return r.Opcode == "jcc-e" && r.FlagsRead && !r.FlagsWrite && r.WriteCount == 0 && r.ImmediateWidth == 32
		}},
		{"7eff", func(r Record) bool {
			return r.Opcode == "jcc-e" && r.FlagsRead && r.Immediate == 255 && r.ImmediateWidth == 8
		}},
		{"660f1f840000000000", func(r Record) bool {
			return r.Opcode == "nop" && r.ReadCount == 0 && r.WriteCount == 0 && !r.Address.Present
		}},
	} {
		r, err := decode("amd64", Instruction{0, c.hex, ""})
		if err != nil || !r.Known || !c.check(r) {
			t.Fatalf("%s: %+v %v", c.hex, r, err)
		}
	}
	for _, raw := range []string{"660f1f840000000001", "8b0425", "8b0510000000", "428b0424", "6683c001", "4483c001", "81c001", "0f8e120000", "f390"} {
		r, err := decode("amd64", Instruction{0, raw, ""})
		if err != nil || r.Known {
			t.Fatalf("must stay unknown: %s %+v %v", raw, r, err)
		}
	}
	a, b := fixture("amd64", "0f8e01000000"), fixture("amd64", "0f8e02000000")
	a.Regions[0].Instructions[0].Relocation = "f0.pc9.i0"
	b.Regions[0].Instructions[0].Relocation = "f0.pc9.i0"
	r, err := Compare(a, b)
	if err != nil || !r.Complete || len(r.Changes) != 0 {
		t.Fatal(r, err)
	}
	b.Regions[0].Instructions[0].Hex = "0f8f02000000"
	r, err = Compare(a, b)
	if err != nil || len(r.Changes) != 1 || r.Changes[0].Category != "branch" {
		t.Fatal(r, err)
	}
}
func TestCaptureMetadataValidation(t *testing.T) {
	valid := func() Snapshot {
		s := fixture("amd64", "31c0")
		s.Capture = &CaptureMetadata{NativeSHA256: strings.Repeat("a", 64), NativeBytes: 4, MappedBytes: 2, UnmappedBytes: 2}
		return s
	}
	if err := validate(valid()); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Snapshot){
		func(s *Snapshot) { s.Capture.NativeSHA256 = "bad" },
		func(s *Snapshot) { s.Capture.MappedBytes = 3 },
		func(s *Snapshot) { s.Capture.UnmappedBytes = 3 },
		func(s *Snapshot) { s.Capture.NativeBytes = 1 },
		func(s *Snapshot) { s.Regions[0].Instructions[0].Offset = 3 },
	} {
		s := valid()
		mutate(&s)
		if err := validate(s); err == nil {
			t.Fatal("inconsistent metadata accepted", s)
		}
	}
	s := fixture("amd64", "c3")
	r, err := Compare(s, s)
	if err != nil || r.Complete || len(r.UnknownSites) != 1 || r.UnknownSites[0].BeforeHex != "c3" {
		t.Fatal(r, err)
	}
}
