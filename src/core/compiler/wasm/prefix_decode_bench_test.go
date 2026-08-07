package wasm

import "testing"

var prefixDecodeKindSink InstrKind

func BenchmarkDecodePrefixLookup(b *testing.B) {
	cases := []struct {
		name string
		data []byte
		fn   func(*reader) (Instruction, error)
	}{
		{name: "fc-no-immediate", data: []byte{0x07}, fn: decodeFC},
		{name: "fb-no-immediate", data: []byte{0x1e}, fn: decodeFB},
		{name: "fd-core-no-immediate", data: []byte{0x6e}, fn: func(r *reader) (Instruction, error) { return decodeFDWithMemarg64(r, false) }},
		{name: "fd-relaxed-no-immediate", data: []byte{0x93, 0x02}, fn: func(r *reader) (Instruction, error) { return decodeFDWithMemarg64(r, false) }},
		{name: "fe-memory", data: []byte{0x00, 0x00, 0x00}, fn: func(r *reader) (Instruction, error) { return decodeFEWithMemarg64(r, false) }},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var kind InstrKind
			for i := 0; i < b.N; i++ {
				r := reader{data: tc.data}
				in, err := tc.fn(&r)
				if err != nil {
					b.Fatal(err)
				}
				kind = in.Kind
			}
			prefixDecodeKindSink = kind
		})
	}
}
