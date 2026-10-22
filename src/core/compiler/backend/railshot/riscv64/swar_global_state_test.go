//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarGlobalDeclaration(class byte, mutable bool, bits [2]uint64) []byte {
	decl := []byte{class, 0}
	if mutable {
		decl[1] = 1
	}
	switch class {
	case 0x7f:
		decl = sleb(append(decl, 0x41), int64(int32(bits[0])))
	case 0x7e:
		decl = sleb(append(decl, 0x42), int64(bits[0]))
	case 0x7d:
		decl = binary.LittleEndian.AppendUint32(append(decl, 0x43), uint32(bits[0]))
	case 0x7c:
		decl = binary.LittleEndian.AppendUint64(append(decl, 0x44), bits[0])
	case 0x7b:
		decl = append(decl, swarLiteral(bits[0], bits[1])...)
	}
	return append(decl, 0x0b)
}

func swarGlobalDeclarationsSource(fixtures []fixtureFunc, declarations ...[]byte) []byte {
	payload := uleb(nil, uint32(len(declarations)))
	for _, decl := range declarations {
		payload = append(payload, decl...)
	}
	return modifySection(binaryModule(fixtures...), 7, 6, payload, false)
}

// These bits come from an owner-created live, bounded offheap directory. No
// arbitrary guest pointer is converted, and the owner stays live during use.
func swarOwnedGlobalCell(owner *swarGlobalsOwner, index, width int) []byte {
	dirBits := owner.directory
	dir := unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&dirBits))), (index+1)*8)
	cellBits := binary.LittleEndian.Uint64(dir[index*8:])
	return unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&cellBits))), width)
}

func TestSWARLiteralGlobalsWholeModuleAndIndependentOwners(t *testing.T) {
	classes := []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b}
	bits := [][2]uint64{{0xffffffff}, {0x8000000000000001}, {0xffa12345}, {0x7ff0000000000001}, {0x7ff0000000000001, 0x80000000deadbeef}}
	declarations := make([][]byte, len(classes))
	for i, class := range classes {
		declarations[i] = swarGlobalDeclaration(class, i%2 == 0, bits[i])
	}
	f := fixtureFunc{results: classes, body: []byte{0x23, 0, 0x23, 1, 0x23, 2, 0x23, 3, 0x23, 4, 0x0b}}
	source := swarGlobalDeclarationsSource([]fixtureFunc{f}, declarations...)
	image, err := compileSWARModule(source, emissionMode{production: true})
	if err != nil || image == nil || len(image.globals) != len(classes) {
		t.Fatal("literal whole-module admission", err)
	}
	clear(source)
	for i, global := range image.globals {
		if global.value != bits[i] || global.typ.Mutable != (i%2 == 0) {
			t.Fatal("literal bits/mutability or source alias", i, global)
		}
	}
	for pass := range 2 {
		job, err := coreruntime.NewJobMemory(0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := job.Close(); err != nil {
				t.Error(err)
			}
		})
		owner, err := image.instantiateGlobals(job)
		if err != nil || job.CaptureInstanceContext().GlobalsPtr != owner.directory || owner.directory == 0 {
			t.Fatal("initialized owner binding", err)
		}
		t.Cleanup(func() {
			if err := owner.Close(); err != nil {
				t.Error(err)
			}
		})
		for i, class := range classes {
			width := 8
			if class == 0x7b {
				width = 16
			}
			cell := swarOwnedGlobalCell(owner, i, width)
			if binary.LittleEndian.Uint64(cell) != bits[i][0] || width == 16 && binary.LittleEndian.Uint64(cell[8:]) != bits[i][1] {
				t.Fatal("initialized exact-width raw cell", pass, i, cell)
			}
			clear(cell)
		}
		if other, err := image.instantiateGlobals(job); other != nil || err == nil {
			t.Fatal("already-bound job overwritten", err)
		}
		job.SetGlobalsPtr(1)
		if err := owner.Close(); err == nil || owner.arena == nil || job.CaptureInstanceContext().GlobalsPtr != 1 {
			t.Fatal("replaced binding released owner", err)
		}
		job.SetGlobalsPtr(owner.directory)
		if err := owner.Close(); err != nil || job.CaptureInstanceContext().GlobalsPtr != 0 || owner.arena != nil {
			t.Fatal("owner detach before release", err)
		}
		if err := owner.Close(); err != nil {
			t.Fatal("idempotent close", err)
		}
	}
}

func TestSWARLiteralGlobalAdmissionBoundsAndUnsupportedInitializers(t *testing.T) {
	f := fixtureFunc{body: []byte{0x0b}}
	for _, count := range []int{0, 256, 257} {
		decls := make([][]byte, count)
		for i := range decls {
			decls[i] = swarGlobalDeclaration(0x7b, true, [2]uint64{uint64(i), ^uint64(i)})
		}
		image, err := compileSWARModule(swarGlobalDeclarationsSource([]fixtureFunc{f}, decls...), emissionMode{production: true})
		if count <= 256 {
			if err != nil || len(image.globals) != count {
				t.Fatal("literal global bound", count, err)
			}
		} else if image != nil || err == nil {
			t.Fatal("excess literal globals", err)
		}
	}
	// Extended arithmetic remains outside this literal pilot. Previous local
	// global references also fail the module's disabled ExtendedConstGlobals
	// validation feature. Direct helper negatives below prove literal-only
	// admission independently of frontend feature rejection.
	for _, decls := range [][][]byte{
		{{0x7f, 0, 0x41, 1, 0x41, 2, 0x6a, 0x0b}},
		{swarGlobalDeclaration(0x7f, false, [2]uint64{1}), {0x7f, 0, 0x23, 0, 0x0b}},
		{{0x70, 0, 0xd0, 0x70, 0x0b}},
		{{0x7b, 0, 0xfd, 12, 0, 0x0b}},
	} {
		if image, err := compileSWARModule(swarGlobalDeclarationsSource([]fixtureFunc{f}, decls...), emissionMode{production: true}); image != nil || err == nil {
			t.Fatal("unsupported initializer admitted", decls, err)
		}
	}
	for _, expr := range [][]byte{nil, {0xfd, 13}, append(append(swarLiteral(1, 2), 0x0b), 0x0b), {0xfd, 12, 1, 2}} {
		if values, err := swarLiteralGlobals([]wasm.Global{{Type: wasm.GlobalType{Type: wasm.V128}, Init: wasm.Expr{BodyBytes: expr}}}); values != nil || err == nil {
			t.Fatal("nonliteral/extended/truncated V128", expr, err)
		}
	}
	for _, global := range []wasm.Global{
		{Type: wasm.GlobalType{Type: wasm.I32}, Init: wasm.Expr{BodyBytes: []byte{0x41, 1, 0x41, 2, 0x6a, 0x0b}}},
		{Type: wasm.GlobalType{Type: wasm.I64}, Init: wasm.Expr{BodyBytes: []byte{0x23, 0, 0x0b}}},
		{Type: wasm.GlobalType{Type: wasm.F32}, Init: wasm.Expr{BodyBytes: []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b}}},
		{Type: wasm.GlobalType{Type: wasm.F64}, Init: wasm.Expr{BodyBytes: []byte{0x44, 0, 0x0b}}},
	} {
		if values, err := swarLiteralGlobals([]wasm.Global{global}); values != nil || err == nil {
			t.Fatal("direct nonliteral/mismatched/truncated scalar initializer", global, err)
		}
	}
	job, err := coreruntime.NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := job.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, globals := range [][]swarGlobalInitializer{
		{{typ: wasm.GlobalType{Type: wasm.FuncRef}}},
		{{typ: wasm.GlobalType{Type: wasm.I32}, value: [2]uint64{1 << 32}}},
		{{typ: wasm.GlobalType{Type: wasm.F64}, value: [2]uint64{0, 1}}},
		make([]swarGlobalInitializer, 257),
	} {
		image := &swarModuleImage{globals: globals}
		if owner, err := image.instantiateGlobals(job); owner != nil || err == nil || job.CaptureInstanceContext().GlobalsPtr != 0 {
			t.Fatal("malformed owner metadata published", err)
		}
	}
	var nilImage *swarModuleImage
	if owner, err := nilImage.instantiateGlobals(job); owner != nil || err == nil {
		t.Fatal("nil image owner", err)
	}
	if owner, err := (&swarModuleImage{}).instantiateGlobals(nil); owner != nil || err == nil {
		t.Fatal("nil job owner", err)
	}
	owner, err := (&swarModuleImage{}).instantiateGlobals(job)
	if err != nil || owner.arena != nil || job.CaptureInstanceContext().GlobalsPtr != 0 {
		t.Fatal("empty global owner", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	// A source-validated immutable set is rejected; mutability is not inferred
	// from any literal or from the fact that the owner supplies writable cells.
	set := fixtureFunc{body: append(append(swarLiteral(1, 2), 0x24, 0), 0x0b)}
	if image, err := compileSWARModule(swarGlobalDeclarationsSource([]fixtureFunc{set}, swarGlobalDeclaration(0x7b, false, [2]uint64{})), emissionMode{production: true}); image != nil || err == nil {
		t.Fatal("immutable set", err)
	}
}

func TestSWARGlobalOwnerDetachedCleanupFailureAndRetry(t *testing.T) {
	job, err := coreruntime.NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := job.Close(); err != nil {
			t.Error(err)
		}
	})
	image := &swarModuleImage{globals: []swarGlobalInitializer{{typ: wasm.GlobalType{Type: wasm.V128}, value: [2]uint64{1, 2}}}}
	owner, err := image.instantiateGlobals(job)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	originalArena, originalDirectory := owner.arena, owner.directory
	failure := errors.New("injected private owner release failure")
	calls := 0
	if err := owner.closeWith(func(arena *coreruntime.Arena) error {
		calls++
		if arena != originalArena || job.CaptureInstanceContext().GlobalsPtr != 0 {
			t.Fatal("release before detach or wrong arena")
		}
		return failure
	}); !errors.Is(err, failure) || calls != 1 || owner.arena != originalArena || owner.directory != originalDirectory || owner.job != job {
		t.Fatal("failed cleanup lost retry ownership", err)
	}
	cell := swarOwnedGlobalCell(owner, 0, 16)
	if binary.LittleEndian.Uint64(cell) != 1 || binary.LittleEndian.Uint64(cell[8:]) != 2 {
		t.Fatal("failed release invalidated live cells")
	}
	if err := owner.Close(); err != nil || owner.arena != nil || job.CaptureInstanceContext().GlobalsPtr != 0 {
		t.Fatal("detached retry", err)
	}
	fresh, err := image.instantiateGlobals(job)
	if err != nil {
		t.Fatal(err)
	}
	job.SetGlobalsPtr(0)
	if err := fresh.Close(); err != nil || fresh.arena != nil {
		t.Fatal("already-detached idle owner close", err)
	}
	if err := fresh.Close(); err != nil {
		t.Fatal("already-detached idempotent close", err)
	}
}
