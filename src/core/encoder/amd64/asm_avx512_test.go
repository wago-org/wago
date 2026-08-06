package amd64

import (
	"bytes"
	"testing"
)

func TestAVX512Encoding(t *testing.T) {
	var a Asm
	a.ZMovdqu64LoadIdx(RCX, RBX, R10, 64)
	a.ZMovdqu64StoreIdx(RBX, R10, RCX, 64)
	a.ZSIMDRRR(vexMap0F, 1, 0xfc, false, RCX, RDX, RBX)
	a.ZSIMDRRR(vexMap0F, 1, 0xd4, true, RCX, RDX, RBX)
	a.ZSIMDRRR(vexMap0F, 1, 0xdb, false, RCX, RDX, RBX)
	a.ZSIMDRRR(vexMap0F, 0, 0x58, false, RCX, RDX, RBX)
	a.ZSIMDRRR(vexMap0F, 1, 0x58, true, RCX, RDX, RBX)
	a.ZSIMDRR(vexMap0F, 0, 0x51, false, RCX, RDX)
	a.ZPternlogd(RCX, RDX, RBX, 0xe4)

	want := []byte{
		0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x4c, 0x13, 0x01,
		0x62, 0xb1, 0xfe, 0x48, 0x7f, 0x4c, 0x13, 0x01,
		0x62, 0xf1, 0x6d, 0x48, 0xfc, 0xcb,
		0x62, 0xf1, 0xed, 0x48, 0xd4, 0xcb,
		0x62, 0xf1, 0x6d, 0x48, 0xdb, 0xcb,
		0x62, 0xf1, 0x6c, 0x48, 0x58, 0xcb,
		0x62, 0xf1, 0xed, 0x48, 0x58, 0xcb,
		0x62, 0xf1, 0x7c, 0x48, 0x51, 0xca,
		0x62, 0xf3, 0x6d, 0x48, 0x25, 0xcb, 0xe4,
	}
	if !bytes.Equal(a.B, want) {
		t.Fatalf("encoding\n got % x\nwant % x", a.B, want)
	}
}

func TestEVEXIndexedMemoryDisplacements(t *testing.T) {
	tests := []struct {
		name   string
		encode func(*Asm)
		want   []byte
	}{
		{
			name:   "load disp32 below compressed range",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RBX, R10, -8256) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x8c, 0x13, 0xc0, 0xdf, 0xff, 0xff},
		},
		{
			name:   "load compressed negative limit",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RBX, R10, -8192) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x4c, 0x13, 0x80},
		},
		{
			name:   "load compressed negative one",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RBX, R10, -64) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x4c, 0x13, 0xff},
		},
		{
			name:   "load no displacement",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RBX, R10, 0) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x0c, 0x13},
		},
		{
			name:   "load compressed positive one",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RBX, R10, 64) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x4c, 0x13, 0x01},
		},
		{
			name:   "load compressed positive limit",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RBX, R10, 8128) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x4c, 0x13, 0x7f},
		},
		{
			name:   "load disp32 above compressed range",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RBX, R10, 8192) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x8c, 0x13, 0x00, 0x20, 0x00, 0x00},
		},
		{
			name:   "load unaligned one uses disp32",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RBX, R10, 1) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x8c, 0x13, 0x01, 0x00, 0x00, 0x00},
		},
		{
			name:   "load unaligned sixty-three uses disp32",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RBX, R10, 63) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x8c, 0x13, 0x3f, 0x00, 0x00, 0x00},
		},
		{
			name:   "load unaligned sixty-five uses disp32",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RBX, R10, 65) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x8c, 0x13, 0x41, 0x00, 0x00, 0x00},
		},
		{
			name:   "load unaligned one-twenty-seven uses disp32",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RBX, R10, 127) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x8c, 0x13, 0x7f, 0x00, 0x00, 0x00},
		},
		{
			name:   "load rbp zero uses compressed zero",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RBP, R10, 0) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x4c, 0x15, 0x00},
		},
		{
			name:   "load r13 zero uses compressed zero",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, R13, R10, 0) },
			want:   []byte{0x62, 0x91, 0xfe, 0x48, 0x6f, 0x4c, 0x15, 0x00},
		},
		{
			name:   "load rsp zero retains sib",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, RSP, R10, 0) },
			want:   []byte{0x62, 0xb1, 0xfe, 0x48, 0x6f, 0x0c, 0x14},
		},
		{
			name:   "load r12 zero retains sib and base extension",
			encode: func(a *Asm) { a.ZMovdqu64LoadIdx(RCX, R12, R10, 0) },
			want:   []byte{0x62, 0x91, 0xfe, 0x48, 0x6f, 0x0c, 0x14},
		},
		{
			name:   "store compressed displacement with extended source",
			encode: func(a *Asm) { a.ZMovdqu64StoreIdx(RBX, R10, R9, 64) },
			want:   []byte{0x62, 0xa1, 0xfe, 0x48, 0x7f, 0x4c, 0x13, 0x01},
		},
		{
			name:   "store unaligned displacement uses disp32",
			encode: func(a *Asm) { a.ZMovdqu64StoreIdx(RBX, R10, R9, 65) },
			want:   []byte{0x62, 0xa1, 0xfe, 0x48, 0x7f, 0x8c, 0x13, 0x41, 0x00, 0x00, 0x00},
		},
		{
			name:   "store rbp zero uses compressed zero",
			encode: func(a *Asm) { a.ZMovdqu64StoreIdx(RBP, R10, R9, 0) },
			want:   []byte{0x62, 0xa1, 0xfe, 0x48, 0x7f, 0x4c, 0x15, 0x00},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.encode(&a)
			if !bytes.Equal(a.B, tc.want) {
				t.Fatalf("encoding\n got % x\nwant % x", a.B, tc.want)
			}
		})
	}
}
