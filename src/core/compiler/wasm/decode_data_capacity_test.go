package wasm

import (
	"encoding/binary"
	"strings"
	"testing"
	"unsafe"
)

func passiveDataVector(count int) []byte {
	data := binary.AppendUvarint(nil, uint64(count))
	for range count {
		data = append(data, 1, 0)
	}
	return data
}

func TestDecodeDataVectorsHaveExactCapacity(t *testing.T) {
	const count = 10000
	r := reader{data: passiveDataVector(count)}
	var dm directModule
	if err := decodeDirectDataSection(&dm, &r); err != nil {
		t.Fatal(err)
	}
	if len(dm.m.Data) != count || cap(dm.m.Data) != count {
		t.Fatalf("data vector has len/cap %d/%d", len(dm.m.Data), cap(dm.m.Data))
	}
	for i := range dm.m.Data {
		if dm.m.Data[i].Mode.Kind != DataPassive || len(dm.m.Data[i].Init) != 0 {
			t.Fatalf("segment%d changed", i)
		}
	}
	if r.has() {
		t.Fatal("did not consume vector")
	}
}

func TestDecodeDataMinimumEncodingBeforeAllocation(t *testing.T) {
	for _, data := range [][]byte{{2, 1, 0}, {1, 1}, {2}, {0x80}} {
		r := reader{data: data}
		var dm directModule
		if err := decodeDirectDataSection(&dm, &r); err == nil {
			t.Fatalf("accepted truncated vector %x", data)
		}
		if dm.m.Data != nil {
			t.Fatalf("allocated vectors for impossible count %x", data)
		}
	}
}

func TestDecodeDataConservativeBudgetIsUnchanged(t *testing.T) {
	const count = 2048
	reserved := uint64(count) * (uint64(unsafe.Sizeof(Data{}))*8 + 16)
	for _, limit := range []uint64{reserved - 1, reserved} {
		r := reader{data: passiveDataVector(count), budget: newDecodeBudget(DecodeLimits{MaxMetadataBytes: limit})}
		var dm directModule
		err := decodeDirectDataSection(&dm, &r)
		if limit < reserved {
			if err == nil || !strings.Contains(err.Error(), "allocation limit") {
				t.Fatalf("budget%d: %v", limit, err)
			}
			if dm.m.Data != nil {
				t.Fatal("allocated before budget rejection")
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestDecodeDataStillRejectsMalformedSegments(t *testing.T) {
	for _, data := range [][]byte{
		{1, 3, 0},                   // invalid flags, despite enough bytes for a segment
		{1, 1, 2, 0},                // truncated passive payload
		{1, 0, 0x41, 0, 0},          // active expression without end
		{1, 2, 0, 0x41, 0, 0x0b, 1}, // indexed active segment with missing payload
	} {
		r := reader{data: data}
		var dm directModule
		if err := decodeDirectDataSection(&dm, &r); err == nil {
			t.Fatalf("accepted malformed data %x", data)
		}
	}
}
