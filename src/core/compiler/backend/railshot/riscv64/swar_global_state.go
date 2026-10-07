//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type swarGlobalInitializer struct {
	typ   wasm.GlobalType
	value [2]uint64
}

// Decode exact literals into owned bit patterns before any image or cell arena
// exists. No extended expression, global.get, reference or imported state is
// admitted, including in globals that no function ever reads.
func swarLiteralGlobals(globals []wasm.Global) ([]swarGlobalInitializer, error) {
	if len(globals) > 256 {
		return nil, unsupported("SWAR literal global bound")
	}
	initializers := make([]swarGlobalInitializer, len(globals))
	for i, global := range globals {
		value, err := swarLiteralGlobal(global)
		if err != nil {
			return nil, err
		}
		initializers[i] = value
	}
	return initializers, nil
}

// The public runtime retains initializer metadata itself; validate one literal
// without allocating a duplicate global directory.
func swarLiteralGlobal(global wasm.Global) (value swarGlobalInitializer, err error) {
	if len(global.Init.Instrs) != 0 {
		return value, unsupported("SWAR global initializer must be byte-backed")
	}

	class := swarClassOf(global.Type.Type)
	if class == swarInvalid {
		return value, unsupported("SWAR literal global carrier")
	}
	value.typ = global.Type
	if class != swarV128 {
		bits, err := plainScalarConst(global.Init, global.Type.Type, true)
		if err != nil {
			return value, err
		}
		value.value[0] = bits
		return value, nil
	}
	r := wasm.ReaderFrom(global.Init.BodyBytes)
	op, err := r.Byte()
	if err != nil || op != 0xfd {
		return value, unsupported("SWAR vector global literal")
	}
	sub, err := r.U32()
	if err != nil || sub != 12 {
		return value, unsupported("SWAR vector global literal")
	}
	for half := range 2 {
		value.value[half], err = r.LEU64()
		if err != nil {
			return value, err
		}
	}
	end, err := r.Byte()
	if err != nil || end != 0x0b || r.HasNext() {
		return value, unsupported("SWAR extended vector initializer")
	}
	return value, nil
}

// This private owner requires exclusive, idle access to its JobMemory, with no
// saved or queued instance contexts. It is not a public instantiation path.
// JobMemory must remain live through Close. Its lifetime encloses every native
// invocation. Close must follow completion
// of all invocations; it removes the binding before freeing the stable arena.
type swarGlobalsOwner struct {
	arena     *coreruntime.Arena
	job       *coreruntime.JobMemory
	directory uintptr
}

func (image *swarModuleImage) instantiateGlobals(job *coreruntime.JobMemory) (*swarGlobalsOwner, error) {
	if image == nil || image.publicInitializers || job == nil || len(image.globals) > 256 {
		return nil, unsupported("SWAR global owner image/job/bound")
	}
	if job.CaptureInstanceContext().GlobalsPtr != 0 {
		return nil, unsupported("SWAR global owner requires unbound job")
	}
	bytes := len(image.globals) * 8
	for _, global := range image.globals {
		class := swarClassOf(global.typ.Type)
		if class == swarInvalid || class.narrow() && global.value[0]>>32 != 0 || class != swarV128 && global.value[1] != 0 {
			return nil, unsupported("SWAR global owner initializer shape")
		}
		bytes += 8
		if class == swarV128 {
			bytes += 8
		}
	}
	owner := &swarGlobalsOwner{}
	if bytes == 0 {
		return owner, nil
	}
	arena, err := coreruntime.NewArena(bytes)
	if err != nil {
		return nil, err
	}
	directory := arena.Alloc(len(image.globals) * 8)
	for i, global := range image.globals {
		width := 8
		if swarClassOf(global.typ.Type) == swarV128 {
			width = 16
		}
		cell := arena.Alloc(width)
		binary.LittleEndian.PutUint64(cell, global.value[0])
		if width == 16 {
			binary.LittleEndian.PutUint64(cell[8:], global.value[1])
		}
		binary.LittleEndian.PutUint64(directory[i*8:], uint64(uintptr(unsafe.Pointer(&cell[0]))))
	}
	owner.arena, owner.job = arena, job
	owner.directory = uintptr(unsafe.Pointer(&directory[0]))
	// All declared cells and directory entries are initialized before binding.
	job.SetGlobalsPtr(owner.directory)
	return owner, nil
}

func (owner *swarGlobalsOwner) Close() error {
	return owner.closeWith((*coreruntime.Arena).Close)
}

// A local release operation keeps failure ordering testable without replacing
// process-wide runtime hooks. Production Close always uses Arena.Close.
func (owner *swarGlobalsOwner) closeWith(release func(*coreruntime.Arena) error) error {
	if owner == nil || owner.arena == nil {
		return nil
	}
	current := owner.job.CaptureInstanceContext().GlobalsPtr
	if current != 0 && current != owner.directory {
		return unsupported("SWAR global owner binding replaced")
	}
	owner.job.SetGlobalsPtr(0)
	if err := release(owner.arena); err != nil {
		return err
	}
	owner.arena, owner.job, owner.directory = nil, nil, 0
	return nil
}
