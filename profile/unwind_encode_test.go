package profile

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

// Interpret the emitted CFI as a consumer: resolve header-relative and
// PC-relative addresses, then apply recovery-state changes at each advance.
// Starting other registers as preserved ensures the CIE explicitly clears them.
func decodeAMD64Unwind(t *testing.T, codeSize uint64, info UnwindInfo) []jitprofile.UnwindRange {
	t.Helper()
	frame, header := info.EHFrame, info.EHFrameHeader
	if err := info.validate(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(header[:4], []byte{1, 0x1b, 3, 0x3b}) {
		t.Fatal("unexpected EH header encodings")
	}
	s32 := func(b []byte) int64 { return int64(int32(binary.LittleEndian.Uint32(b))) }
	base := int64((codeSize + 7) &^ 7)
	headerBase := base + int64(len(frame))
	if headerBase+4+s32(header[4:]) != base {
		t.Fatal("header does not locate EH frame")
	}
	count := int(binary.LittleEndian.Uint32(header[8:]))
	if count == 0 || len(header) != 12+count*8 {
		t.Fatal("invalid search table")
	}
	cieEnd := int(binary.LittleEndian.Uint32(frame)) + 4
	if !bytes.Equal(frame[4:17], []byte{0, 0, 0, 0, 1, 'z', 'R', 0, 1, 0x78, 16, 1, 0x1b}) {
		t.Fatal("invalid CIE")
	}
	type state struct {
		cfa       uint64
		ra        int64
		sp        bool
		undefined [33]bool
	}
	readULEB := func(b []byte, at *int) uint64 {
		var n uint64
		for shift := uint(0); shift < 64; shift += 7 {
			if *at >= len(b) {
				t.Fatal("truncated ULEB")
			}
			c := b[*at]
			*at++
			n |= uint64(c&127) << shift
			if c < 128 {
				return n
			}
		}
		t.Fatal("overflowing ULEB")
		return 0
	}
	var rows []jitprofile.UnwindRange
	consume := func(b []byte, st *state, start, end uint64, record bool) {
		pc := start
		flush := func(next uint64) {
			if next > end || next < pc {
				t.Fatal("CFI PC outside FDE")
			}
			if record && next > pc {
				rows = append(rows, jitprofile.UnwindRange{Offset: pc, Size: next - pc, CFARegister: 7, CFAOffset: int64(st.cfa), ReturnOffset: st.ra})
			}
			pc = next
		}
		for at := 0; at < len(b); {
			op := b[at]
			at++
			switch {
			case op == 0:
			case op&0xc0 == 0x40:
				flush(pc + uint64(op&63))
			case op&0xc0 == 0x80:
				if op&63 != 16 {
					t.Fatal("unexpected saved register")
				}
				st.ra = -8 * int64(readULEB(b, &at))
			case op == 2 || op == 3 || op == 4:
				width := 1 << (op - 2)
				if at+int(width) > len(b) {
					t.Fatal("truncated advance")
				}
				var delta uint64
				for i := 0; i < int(width); i++ {
					delta |= uint64(b[at+i]) << uint(i*8)
				}
				at += int(width)
				flush(pc + delta)
			case op == 0x0c:
				if readULEB(b, &at) != 7 {
					t.Fatal("CFA is not RSP-based")
				}
				st.cfa = readULEB(b, &at)
			case op == 0x0e:
				st.cfa = readULEB(b, &at)
			case op == 0x14:
				if readULEB(b, &at) != 7 || readULEB(b, &at) != 0 {
					t.Fatal("caller SP is not CFA")
				}
				st.sp = true
			case op == 7:
				reg := readULEB(b, &at)
				if reg > 32 {
					t.Fatal("unexpected register")
				}
				st.undefined[reg] = true
			default:
				t.Fatalf("unexpected CFI opcode %x", op)
			}
		}
		flush(end)
	}
	var initial state
	consume(frame[17:cieEnd], &initial, 0, 0, false)
	if !initial.sp || initial.ra != -8 || initial.cfa != 8 {
		t.Fatal("invalid initial recovery state")
	}
	for reg, unknown := range initial.undefined {
		if reg != 7 && reg != 16 && !unknown {
			t.Fatalf("register %d incorrectly preserved", reg)
		}
	}
	var previousEnd uint64
	for i := 0; i < count; i++ {
		start := headerBase + s32(header[12+i*8:])
		fde := headerBase + s32(header[16+i*8:]) - base
		if fde < int64(cieEnd) || fde+17 > int64(len(frame)) {
			t.Fatal("invalid header FDE address")
		}
		at := int(fde)
		end := at + 4 + int(binary.LittleEndian.Uint32(frame[at:]))
		if at+4-int(binary.LittleEndian.Uint32(frame[at+4:])) != 0 {
			t.Fatal("FDE CIE link mismatch")
		}
		begin := base + fde + 8 + s32(frame[at+8:])
		size := s32(frame[at+12:])
		if begin != start || begin < 0 || uint64(begin) < previousEnd || size <= 0 || begin+size > int64(codeSize) || frame[at+16] != 0 {
			t.Fatal("invalid FDE range")
		}
		current := initial
		consume(frame[at+17:end], &current, uint64(begin), uint64(begin+size), true)
		previousEnd = uint64(begin + size)
	}
	return rows
}

func TestEncodeAMD64UnwindTransitionsAndSparseGaps(t *testing.T) {
	var ranges []jitprofile.UnwindRange
	pc := uint64(17)
	for i, size := range []uint64{1, 63, 64, 255, 256, 65535, 65536, 3} {
		if i == 4 {
			pc += 11
		}
		ranges = append(ranges, jitprofile.UnwindRange{Offset: pc, Size: size, CFARegister: 7, CFAOffset: int64(8 + 256*i), ReturnOffset: -8})
		pc += size
	}
	codeSize := pc + 19
	info, err := EncodeAMD64Unwind(codeSize, ranges)
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeAMD64Unwind(t, codeSize, info)
	for pc := uint64(0); pc < codeSize; pc++ {
		want, known := jitprofile.LookupUnwind(ranges, pc)
		got, found := jitprofile.LookupUnwind(decoded, pc)
		if found != known || found && (got.CFAOffset != want.CFAOffset || got.ReturnOffset != want.ReturnOffset) {
			t.Fatalf("PC %d: got %+v (%v), want %+v (%v)", pc, got, found, want, known)
		}
	}
	if info.MappedSize != 0 {
		t.Fatal("offline encoder claims mapped bytes")
	}
}

func TestEncodeAMD64UnwindRejectsUnsupportedAndOverflow(t *testing.T) {
	valid := jitprofile.UnwindRange{Size: 1, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8}
	for _, test := range []struct {
		size uint64
		rows []jitprofile.UnwindRange
	}{
		{0, []jitprofile.UnwindRange{valid}}, {1, nil}, {math.MaxUint64, []jitprofile.UnwindRange{valid}},
		{math.MaxInt32 - 7, []jitprofile.UnwindRange{valid}},
		{1, []jitprofile.UnwindRange{{Size: 1, CFARegister: 6, CFAOffset: 8, ReturnOffset: -8}}},
		{1, []jitprofile.UnwindRange{{Size: 1, CFARegister: 7, CFAOffset: 8, ReturnOffset: -1}}},
		{1, []jitprofile.UnwindRange{valid, valid}},
	} {
		if _, err := EncodeAMD64Unwind(test.size, test.rows); err == nil {
			t.Fatal("accepted invalid encoding", test)
		}
	}
	// The largest signed CFA is still a legal ULEB; the saved return offset must
	// remain correctly signed after the data-alignment factor is applied.
	valid.CFAOffset = math.MaxInt64
	valid.ReturnOffset = -(math.MaxInt64 &^ 7)
	info, err := EncodeAMD64Unwind(1, []jitprofile.UnwindRange{valid})
	if err != nil {
		t.Fatal(err)
	}
	got := decodeAMD64Unwind(t, 1, info)
	if len(got) != 1 || got[0].CFAOffset != valid.CFAOffset || got[0].ReturnOffset != valid.ReturnOffset {
		t.Fatal(got)
	}
}

func TestJITDumpCompilerUnwindRegions(t *testing.T) {
	im := jitprofile.Image{ID: 9, Target: "linux/amd64", Base: 0x1000, Size: 40, Code: make([]byte, 40), Regions: []jitprofile.Region{
		{Size: 8, Kind: "entry-adapter", Function: 1},
		{Offset: 8, Size: 20, Kind: "guest-body", Function: 1},
		{Offset: 28, Size: 4, Kind: "literal-data", Function: -1},
		{Offset: 32, Size: 8, Kind: "guest-body", Function: 2},
	}, Unwind: []jitprofile.UnwindRange{
		{Offset: 8, Size: 4, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8},
		{Offset: 12, Size: 12, CFARegister: 7, CFAOffset: 40, ReturnOffset: -8},
		{Offset: 24, Size: 1, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8},
		{Offset: 32, Size: 7, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8},
	}}
	var b bytes.Buffer
	j, err := NewJITDump(&b, 1, "amd64", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Write([]jitprofile.Event{{Kind: "load", ImageID: 9, Timestamp: 10, Image: &im}}); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(20); err != nil {
		t.Fatal(err)
	}
	raw := b.Bytes()[40:]
	var pending *UnwindInfo
	var ids []uint32
	codeIndex := 0
	for len(raw) > 0 {
		id, size := binary.LittleEndian.Uint32(raw), int(binary.LittleEndian.Uint32(raw[4:]))
		ids = append(ids, id)
		if size < 16 || size > len(raw) {
			t.Fatal("invalid record length")
		}
		record := raw[:size]
		raw = raw[size:]
		if id == 4 {
			if pending != nil {
				t.Fatal("two unwind records without intervening code")
			}
			payloadSize := int(binary.LittleEndian.Uint64(record[16:]))
			headerSize := int(binary.LittleEndian.Uint64(record[24:]))
			if payloadSize != len(record)-40 {
				t.Fatal("bad payload size")
			}
			pending = &UnwindInfo{EHFrame: record[40 : len(record)-headerSize], EHFrameHeader: record[len(record)-headerSize:]}
		} else if id == 0 {
			address := binary.LittleEndian.Uint64(record[32:])
			codeSize := binary.LittleEndian.Uint64(record[40:])
			if codeIndex == 0 {
				if address != 0x1000 || pending != nil {
					t.Fatal("adapter inherited unwind data")
				}
			} else {
				if pending == nil {
					t.Fatal("missing code-bound unwind data")
				}
				decoded := decodeAMD64Unwind(t, codeSize, *pending)
				for pc := uint64(0); pc < codeSize; pc++ {
					want, known := jitprofile.LookupUnwind(im.Unwind, address-im.Base+pc)
					got, found := jitprofile.LookupUnwind(decoded, pc)
					if known != found || found && want.CFAOffset != got.CFAOffset {
						t.Fatal("code-relative row relocation failed", pc, got, want)
					}
				}
			}
			pending = nil
			codeIndex++
		}
	}
	wantIDs := []uint32{0, 4, 0, 4, 0, 3}
	if len(ids) != len(wantIDs) {
		t.Fatal(ids)
	}
	for i := range ids {
		if ids[i] != wantIDs[i] {
			t.Fatal(ids)
		}
	}
	if im.Unwind[0].Offset != 8 || im.Unwind[3].Offset != 32 {
		t.Fatal("exporter mutated retained rows")
	}
}

func TestJITDumpRejectsInvalidCompilerUnwind(t *testing.T) {
	for _, mode := range []string{"wrong-machine", "wrong-target", "unsupported-register", "cross-region", "literal", "manual-conflict"} {
		t.Run(mode, func(t *testing.T) {
			info, events := unwindFixture(t)
			im := events[0].Image
			im.Target = "linux/amd64"
			im.Unwind = []jitprofile.UnwindRange{{Size: im.Size, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8}}
			arch := "amd64"
			switch mode {
			case "wrong-machine":
				arch = "arm64"
			case "wrong-target":
				im.Target = "linux/arm64"
			case "unsupported-register":
				im.Unwind[0].CFARegister = 6
			case "cross-region":
				im.Regions = []jitprofile.Region{{Size: 10, Kind: "guest-body"}, {Offset: 10, Size: im.Size - 10, Kind: "guest-body"}}
			case "literal":
				im.Regions[0].Kind = "literal-data"
			}
			var b bytes.Buffer
			j, err := NewJITDump(&b, 1, arch, 1)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "manual-conflict" {
				if err := j.WriteUnwind(im.ID, im.Base, im.Size, info, 10); err != nil {
					t.Fatal(err)
				}
			}
			if j.Write(events) == nil || j.Close(20) == nil {
				t.Fatal("invalid compiler unwind accepted")
			}
		})
	}
}
