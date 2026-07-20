package embedded32

import (
	"slices"
	"testing"
)

func firmwareArtifactFixture() FirmwareArtifact {
	const base = uint32(0x20020000)
	return FirmwareArtifact{
		Target:         TransportTargetArm32,
		ImageAddress:   base,
		ContextAddress: base + 32,
		StartAddress:   base + 1,
		Image:          make([]byte, 160),
		Contexts:       []uint32{base + 32, base + 64},
		Functions: []FirmwareTransportFunction{
			{Address: base + 1, Context: base + 32, ParamSlots: 1, ResultSlots: 2},
			{Address: base + 17, Context: base + 64},
		},
	}
}

func TestFirmwareArtifactRoundTripUsesCallerStorage(t *testing.T) {
	want := firmwareArtifactFixture()
	for index := range want.Image {
		want.Image[index] = byte(index)
	}
	size, ok := FirmwareArtifactSize(uint32(len(want.Image)), uint32(len(want.Contexts)), uint32(len(want.Functions)))
	if !ok {
		t.Fatal("artifact size")
	}
	encoded := make([]byte, size)
	n, err := EncodeFirmwareArtifact(encoded, want)
	if err != nil || n != size {
		t.Fatalf("encode bytes=%d err=%v", n, err)
	}
	contexts := make([]uint32, 2)
	functions := make([]FirmwareTransportFunction, 2)
	got, err := DecodeFirmwareArtifact(encoded, contexts, functions)
	if err != nil {
		t.Fatal(err)
	}
	if got.Target != want.Target || got.ImageAddress != want.ImageAddress || got.ContextAddress != want.ContextAddress || got.StartAddress != want.StartAddress ||
		!slices.Equal(got.Image, want.Image) || !slices.Equal(got.Contexts, want.Contexts) || !slices.Equal(got.Functions, want.Functions) {
		t.Fatalf("artifact=%+v", got)
	}
	got.Image[0] = 0xee
	if encoded[len(encoded)-len(got.Image)] != 0xee {
		t.Fatal("decoded image does not alias encoded artifact")
	}
	allocations := testing.AllocsPerRun(100, func() {
		if _, err := DecodeFirmwareArtifact(encoded, contexts, functions); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("decode allocations=%v", allocations)
	}
}

func TestFirmwareArtifactRejectsMalformedMetadata(t *testing.T) {
	want := firmwareArtifactFixture()
	size, _ := FirmwareArtifactSize(uint32(len(want.Image)), uint32(len(want.Contexts)), uint32(len(want.Functions)))
	encoded := make([]byte, size)
	if _, err := EncodeFirmwareArtifact(encoded, want); err != nil {
		t.Fatal(err)
	}
	contexts := make([]uint32, 2)
	functions := make([]FirmwareTransportFunction, 2)
	tests := []struct {
		name string
		edit func([]byte)
	}{
		{"magic", func(value []byte) { value[0] = 0 }},
		{"version", func(value []byte) { value[4]++ }},
		{"image offset", func(value []byte) { value[36]++ }},
		{"context count", func(value []byte) { value[28]++ }},
		{"padding", func(value []byte) { value[75] = 1 }},
		{"callable", func(value []byte) { value[48] &^= 1 }},
		{"root context omitted", func(value []byte) { value[40] += 4 }},
		{"function context unlisted", func(value []byte) { value[52] += 4 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := slices.Clone(encoded)
			test.edit(value)
			if _, err := DecodeFirmwareArtifact(value, contexts, functions); err == nil {
				t.Fatal("malformed artifact accepted")
			}
		})
	}
	if _, err := DecodeFirmwareArtifact(encoded, contexts[:1], functions); err == nil {
		t.Fatal("undersized context storage accepted")
	}
	if _, err := DecodeFirmwareArtifact(encoded[:len(encoded)-1], contexts, functions); err == nil {
		t.Fatal("truncated image accepted")
	}
}
