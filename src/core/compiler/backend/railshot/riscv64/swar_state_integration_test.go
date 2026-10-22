//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarCombinedStateSource() ([]byte, []fixtureFunc, [2]uint64, []byte) {
	bits := [2]uint64{0x7ff0000000000001, 0x80000000deadbeef}
	payload := []byte{1, 0, 0, 0, 0, 0, 0xf0, 0x7f, 0xef, 0xbe, 0xad, 0xde, 0, 0, 0, 0x80}
	root := fixtureFunc{params: []byte{0x7b, 0x7f, 0x7f}, results: []byte{0x7b, 0x7b, 0x7b, 0x7f}, locals: []byte{1, 125, 0x7b}, body: []byte{
		0x23, 0, // Keep the old raw global value live across mutation and calls.
		0x23, 0, 0x20, 0, 0xfd, 81, 0x24, 0,
		0x20, 1, 0x41, 0, 0x41, 16, 0xfc, 8, 1, 0,
		0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 12, 1, 0,
		0x20, 0, 0x20, 1, 0x41, 0, 0x11, 1, 0,
		0x23, 0,
		0x20, 2, 4, 0x40, 0xfc, 9, 1, 0xfc, 13, 1, 0x0b,
		0xfc, 16, 0, 0x0b,
	}}
	leaf := fixtureFunc{params: []byte{0x7b, 0x7f}, result: 0x7b, locals: []byte{1, 126, 0x7b}, unexported: true, body: []byte{0x20, 0, 0x20, 1, 0xfd, 0, 0, 0, 0xfd, 81, 0x23, 0, 0xfd, 81, 0x0b}}
	forward := leaf
	forward.body = []byte{0x20, 0, 0x20, 1, 0x10, 1, 0x0b}
	fixtures := []fixtureFunc{root, leaf, forward}
	segments := []wasm.Data{swarActiveSegment(7, []byte{0xa5, 0x5a}), {Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: payload}}
	source := swarMixedDataSource(fixtures, segments)
	source = modifySection(source, 5, 5, []byte{1, 1, 1, 2}, true)
	source = modifySection(source, 5, 4, []byte{1, 0x70, 1, 2, 4}, false)
	source = modifySection(source, 7, 6, append([]byte{1}, swarGlobalDeclaration(0x7b, true, bits)...), false)
	// Active slot0 initially targets leaf1; passive original index1 installs
	// function2, whose checked signature equals leaf1 and direct call goes back.
	source = modifySection(source, 12, 9, []byte{3, 0, 0x41, 0, 0x0b, 1, 1, 1, 0, 1, 2, 3, 0, 2, 1, 2}, false)
	return source, fixtures, bits, payload
}

func TestSWARCombinedStateModulePrewalkAndOwnedSnapshots(t *testing.T) {
	source, _, bits, payload := swarCombinedStateSource()
	image, err := compileSWARTableModule(source, emissionMode{production: true, interruptible: true})
	if err != nil || len(image.functions) != 3 || len(image.globals) != 1 || image.globals[0].value != bits || len(image.data) != 2 || len(image.activeData) != 1 || image.activeData[0].index != 0 || image.table == nil || len(image.elements) != 3 || image.functionKeys[1] != image.functionKeys[2] || image.functions[2].DirectCalls != 1 {
		t.Fatal("combined private state module", err)
	}
	for _, info := range image.functions {
		if info.FrameHeaderBytes != callFrameHeader || info.FrameBytes <= 2048 {
			t.Fatal("combined state lost large framed homes", info)
		}
	}
	clear(source)
	if !bytes.Equal(image.data[1], payload) || !bytes.Equal(image.activeData[0].bytes, []byte{0xa5, 0x5a}) || image.elements[1].functions[0] != 2 {
		t.Fatal("combined source snapshot aliases")
	}
	source, _, _, _ = swarCombinedStateSource()
	if image, err := compileSWARModule(source, emissionMode{production: true, interruptible: true}); image != nil || err == nil {
		t.Fatal("combined default table gate widened", err)
	}
	if image, err := compileSWARTableModule(source, emissionMode{production: true}); image != nil || err == nil {
		t.Fatal("combined mutation omitted interruption bridge", err)
	}
	// The last defined function is admitted even when the root export is valid.
	broken := fixtureFunc{body: []byte{0x00, 0x12, 0, 0x0b}, unexported: true}
	unsupported := swarTableStateSource([]fixtureFunc{{body: []byte{0x0b}}, broken}, 0, 0, nil)
	m, err := wasm.DecodeModule(unsupported)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 128}); err != nil {
		t.Fatal("unsupported body must pass normal validation", err)
	}
	if image, err := compileSWARTableModule(unsupported, emissionMode{production: true, interruptible: true}); image != nil || err == nil {
		t.Fatal("unsupported unexported body reached state owner", err)
	}
}
