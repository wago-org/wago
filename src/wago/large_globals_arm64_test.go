//go:build arm64

package wago

import (
	"encoding/binary"
	"testing"
)

func TestLargeGlobalCellTableGetAndSet(t *testing.T) {
	const count = 11537
	module := []byte{0, 0x61, 0x73, 0x6d, 1, 0, 0, 0}
	section := func(id byte, payload []byte) {
		module = append(module, id)
		module = binary.AppendUvarint(module, uint64(len(payload)))
		module = append(module, payload...)
	}
	section(1, []byte{2, 0x60, 0, 1, 0x7f, 0x60, 1, 0x7f, 0})
	section(3, []byte{2, 0, 1})
	globals := binary.AppendUvarint(nil, count)
	for i := 0; i < count; i++ {
		globals = append(globals, 0x7f, 1, 0x41, 0, 0x0b)
	}
	section(6, globals)
	section(7, []byte{2, 3, 'g', 'e', 't', 0, 0, 3, 's', 'e', 't', 0, 1})
	get := []byte{0, 0x23}
	get = binary.AppendUvarint(get, count-1)
	get = append(get, 0x0b)
	set := []byte{0, 0x20, 0, 0x24}
	set = binary.AppendUvarint(set, count-1)
	set = append(set, 0x0b)
	code := []byte{2}
	code = binary.AppendUvarint(code, uint64(len(get)))
	code = append(code, get...)
	code = binary.AppendUvarint(code, uint64(len(set)))
	code = append(code, set...)
	section(10, code)
	compiled, err := Compile(nil, module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("set", 42); err != nil {
		t.Fatal(err)
	}
	got, err := in.Invoke("get")
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("get=%v, %v", got, err)
	}
}
