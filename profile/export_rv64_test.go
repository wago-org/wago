package profile

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

func TestRV64JITDumpCodeAndGenerations(t *testing.T) {
	var output bytes.Buffer
	dump, err := NewJITDump(&output, 123, "riscv64", 1)
	if err != nil {
		t.Fatal(err)
	}
	// Two fixed-width instructions, with a non-executable alignment gap. Reuse
	// the same address after retirement to exercise temporal code identities.
	code := []byte{0x13, 0, 0, 0, 0, 0, 0, 0, 0x67, 0x80, 0, 0}
	for generation := uint64(1); generation <= 2; generation++ {
		im := jitprofile.Image{ID: generation, Target: "linux/riscv64", Base: 0x1000, Size: uint64(len(code)), Code: code,
			Regions: []jitprofile.Region{{Size: 4, Kind: "guest-body", Function: 0}, {Offset: 4, Size: 4, Kind: "padding", Function: -1}, {Offset: 8, Size: 4, Kind: "runtime-helper", Function: 0}}}
		if err := dump.Write([]jitprofile.Event{{Timestamp: generation * 10, ThreadID: 456, Kind: "load", ImageID: generation, Image: &im}, {Timestamp: generation*10 + 1, Kind: "retire", ImageID: generation}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := dump.Close(30); err != nil {
		t.Fatal(err)
	}
	data := output.Bytes()
	u32 := func(offset int) uint32 { return binary.LittleEndian.Uint32(data[offset:]) }
	u64 := func(offset int) uint64 { return binary.LittleEndian.Uint64(data[offset:]) }
	if u32(0) != 0x4a695444 || u32(4) != 1 || u32(8) != 40 || u32(12) != 243 || u32(20) != 123 {
		t.Fatal("invalid RV64 jitdump header")
	}
	at := 40
	for record := uint64(1); record <= 4; record++ {
		if len(data)-at < 56 {
			t.Fatal("truncated code record")
		}
		end := at + int(u32(at+4))
		instruction := int((record - 1) % 2 * 8)
		if end > len(data) || end < at+61 || u32(at) != 0 || u64(at+8) != ((record-1)/2+1)*10 || u32(at+16) != 123 || u32(at+20) != 456 || u64(at+24) != 0x1000+uint64(instruction) || u64(at+32) != u64(at+24) || u64(at+40) != 4 || u64(at+48) != record {
			t.Fatal("invalid code record", record)
		}
		if data[end-5] != 0 || !bytes.Equal(data[end-4:end], code[instruction:instruction+4]) {
			t.Fatal("code bytes or terminated symbol changed", record)
		}
		at = end
	}
	if len(data)-at != 16 || u32(at) != 3 || u32(at+4) != 16 || u64(at+8) != 30 {
		t.Fatal("missing close or unexpected padding/unload records")
	}
}

func TestRV64JITDumpRejectsCompilerUnwind(t *testing.T) {
	var output bytes.Buffer
	dump, err := NewJITDump(&output, 123, "riscv64", 1)
	if err != nil {
		t.Fatal(err)
	}
	events := fixture()
	events[0].Image.Target = "linux/riscv64"
	events[0].Image.Unwind = []jitprofile.UnwindRange{{Size: 2, CFARegister: 2, CFAOffset: 16, ReturnOffset: -16}}
	if err := dump.Write(events); err == nil || !strings.Contains(err.Error(), "unsupported compiler unwind target") {
		t.Fatal("unqualified compiler unwind accepted", err)
	}
	if output.Len() != 40 || dump.Close(30) == nil {
		t.Fatal("unwind rejection wrote records or lost its error")
	}
}
