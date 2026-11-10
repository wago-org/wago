package profile

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

// This fixture is a frameless recursive AMD64 function. Each activation subtracts
// eight bytes from RSP, calls itself, then restores RSP. The leaf spins before
// returning. Its CFA changes at native offsets 4, 19, 20 and 33. It was recorded
// with perf's DWARF stack capture; frame-pointer fallback alone sees one frame.
func unwindFixture(t *testing.T) (UnwindInfo, []jitprofile.Event) {
	t.Helper()
	raw, err := hex.DecodeString("1400000000000000017a5200017810011b0c0708900100001c0000001c000000b8ffffff2200000000440e104f0e08410e104d0e080000000000000000000000011b033bbcffffff0100000098ffffffd8ffffff")
	if err != nil {
		t.Fatal(err)
	}
	code, err := hex.DecodeString("4883ec0885ff740cffcfe8f1ffffff4883c408c3b9a0860100ffc975fc4883c408c3")
	if err != nil {
		t.Fatal(err)
	}
	image := &jitprofile.Image{ID: 1, ModuleID: "probe", ArtifactID: "recursive-cfi", Base: 0x1000, Size: uint64(len(code)), Code: code, Regions: []jitprofile.Region{{Size: uint64(len(code)), Kind: "guest-body", Function: 0, Name: "recursive_probe"}}}
	return UnwindInfo{EHFrame: raw[:64], EHFrameHeader: raw[64:]}, []jitprofile.Event{{Kind: "load", Timestamp: 10, ImageID: 1, Image: image}}
}

func TestJITDumpUnwindLayoutAndBinding(t *testing.T) {
	info, events := unwindFixture(t)
	var b bytes.Buffer
	j, err := NewJITDump(&b, 1, "amd64", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.WriteUnwind(1, 0x1000, 34, info, 10); err != nil {
		t.Fatal(err)
	}
	if err = j.Write(events); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(20); err != nil {
		t.Fatal(err)
	}
	raw := b.Bytes()[40:]
	u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(raw[off:]) }
	u64 := func(off int) uint64 { return binary.LittleEndian.Uint64(raw[off:]) }
	if u32(0) != 4 || u32(4) != 124 || u64(8) != 10 || u64(16) != 84 || u64(24) != 20 || u64(32) != 0 {
		t.Fatal("invalid unwind record header")
	}
	if !bytes.Equal(raw[40:104], info.EHFrame) || !bytes.Equal(raw[104:124], info.EHFrameHeader) || u32(124) != 0 {
		t.Fatal("unwind payload or code-load order changed")
	}
}

func TestJITDumpUnwindRejectsUnboundOrMalformedData(t *testing.T) {
	for _, mode := range []string{"address", "generation", "size", "timestamp", "duplicate", "close", "truncated", "bad-cie", "mapped-size", "mapped-range", "short-write"} {
		t.Run(mode, func(t *testing.T) {
			info, events := unwindFixture(t)
			j, err := NewJITDump(io.Discard, 1, "amd64", 1)
			if err != nil {
				t.Fatal(err)
			}
			imageID := uint64(1)
			address, size, ts := uint64(0x1000), uint64(34), uint64(10)
			switch mode {
			case "address":
				address++
			case "generation":
				imageID++
			case "size":
				size++
			case "timestamp":
				ts++
			case "truncated":
				info.EHFrame = info.EHFrame[:30]
			case "bad-cie":
				info.EHFrame[28] = 0xff
			case "mapped-size":
				info.MappedSize = 1
			case "mapped-range":
				address = ^uint64(0) - size
				info.MappedSize = uint64(len(info.EHFrame) + len(info.EHFrameHeader))
			case "short-write":
				j.w = shortWriter{}
			}
			err = j.WriteUnwind(imageID, address, size, info, ts)
			if mode == "mapped-range" && err == nil {
				t.Fatal("accepted mapped address overflow")
			}
			if mode == "duplicate" && err == nil {
				err = j.WriteUnwind(imageID, address, size, info, ts)
			}
			if mode == "close" && err == nil {
				err = j.Close(20)
			}
			if err == nil {
				err = j.Write(events)
			}
			if err == nil {
				t.Fatal("invalid unwind export succeeded")
			}
			if mode == "short-write" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatal(err)
			}
			if j.Close(20) == nil {
				t.Fatal("export failure was not sticky")
			}
		})
	}
}
