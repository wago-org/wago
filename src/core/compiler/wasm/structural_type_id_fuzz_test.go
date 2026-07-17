package wasm

import "testing"

func FuzzStructuralTypeKeyDAG(f *testing.F) {
	f.Add([]byte{1, 1, 2, 3})
	f.Add([]byte{16, 4, 8, 15, 16, 23, 42})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		depth := int(data[0]%31) + 1
		m := &Module{Types: make([]RecType, depth)}
		m.Types[0].SubTypes = []SubType{{Final: true, Comp: CompType{Kind: CompFunc}}}
		for i := 1; i < depth; i++ {
			width := 1
			if i < len(data) {
				width += int(data[i] % 8)
			}
			params := make([]ValType, width)
			for j := range params {
				parent := uint32(i - 1)
				if len(data) != 0 {
					parent = uint32(data[(i+j)%len(data)]) % uint32(i)
				}
				params[j] = indexedRef(parent, j&1 == 0)
			}
			m.Types[i].SubTypes = []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: params}}}
		}
		_, _ = m.StructuralTypeKeyChecked(uint32(depth - 1))
	})
}
