package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime"
)

// Instance is ready for repeated Invoke calls.
type Instance struct {
	c                      *Compiled
	eng                    *runtime.Engine
	jm                     *runtime.JobMemory
	ar                     *runtime.Arena
	base                   uintptr
	mem                    []byte
	hosts                  map[string]HostFunc
	hostLog                []byte
	globals                []byte
	serArgs, results, trap []byte
}

// Instantiate maps code, initializes memory/table state, and allocates call buffers.
func Instantiate(c *Compiled, hosts map[string]HostFunc) (*Instance, error) {
	return InstantiateWithImports(c, Imports{Funcs: hosts})
}

// InstantiateWithImports maps code and supplies host functions and globals.
func InstantiateWithImports(c *Compiled, imports Imports) (*Instance, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	importGlobalBits, err := c.importedGlobalBits(imports)
	if err != nil {
		return nil, err
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		return nil, err
	}
	jm, err := runtime.NewJobMemory(1 << 16)
	if err != nil {
		eng.Close()
		return nil, err
	}
	ar, err := runtime.NewArena(1 << 20)
	if err != nil {
		jm.Close()
		eng.Close()
		return nil, err
	}
	mem, base, err := runtime.MapCode(c.Code)
	if err != nil {
		ar.Close()
		jm.Close()
		eng.Close()
		return nil, err
	}
	success := false
	defer func() {
		if success {
			return
		}
		runtime.Unmap(mem)
		ar.Close()
		jm.Close()
		eng.Close()
	}()
	const maxEntries = (1 << 16) / 8
	hostLog := ar.Alloc(8 + maxEntries*8)
	jm.SetCustomCtx(uintptr(unsafe.Pointer(&hostLog[0])))

	var globals []byte
	if len(c.Globals) > 0 {
		globals = ar.Alloc(8 * len(c.Globals))
		for i, g := range c.Globals {
			bits := g.Bits
			if i < len(importGlobalBits) {
				bits = importGlobalBits[i]
			} else if g.HasInitGlobal {
				if g.InitGlobal < 0 || g.InitGlobal >= i {
					return nil, fmt.Errorf("global %d initializer references unavailable global %d", i, g.InitGlobal)
				}
				bits = readGlobalSlot(globals, g.InitGlobal, g.Type)
			}
			writeGlobalSlot(globals, i, g.Type, bits)
		}
		jm.SetGlobalsPtr(uintptr(unsafe.Pointer(&globals[0])))
	}

	// Table descriptor: [len u32][pad][entry...], entry {codePtr u64, sigID u32, pad u32}.
	if c.TableSize > 0 || len(c.Elems) > 0 {
		size := c.TableSize
		desc := ar.Alloc(8 + size*16)
		binary.LittleEndian.PutUint32(desc, uint32(size))
		for seg, el := range c.Elems {
			elemBase := el.Offset.Base
			if el.Offset.HasGlobal {
				if el.Offset.Global < 0 || el.Offset.Global >= len(c.Globals) || el.Offset.Global*8+8 > len(globals) {
					return nil, fmt.Errorf("element offset global %d out of range", el.Offset.Global)
				}
				elemBase = uint32(readGlobalSlot(globals, el.Offset.Global, c.Globals[el.Offset.Global].Type))
			}
			end := uint64(elemBase) + uint64(len(el.Funcs))
			if end > uint64(size) {
				return nil, fmt.Errorf("active element segment %d out of bounds: offset %d + length %d > table size %d", seg, elemBase, len(el.Funcs), size)
			}
			for k, fidx := range el.Funcs {
				slot := int(elemBase) + k
				off := 8 + slot*16
				if li := int(fidx) - c.NumImports; li >= 0 && li < len(c.Entry) {
					binary.LittleEndian.PutUint64(desc[off:], uint64(base)+uint64(c.Entry[li]))
				}
				if int(fidx) < len(c.FuncTypeID) {
					binary.LittleEndian.PutUint32(desc[off+8:], c.FuncTypeID[fidx])
				}
			}
		}
		jm.SetTablePtr(uintptr(unsafe.Pointer(&desc[0])))
	}

	if len(c.Data) > 0 {
		lin := jm.LinearMemory()
		for seg, d := range c.Data {
			off := d.Offset.Base
			if d.Offset.HasGlobal {
				if d.Offset.Global < 0 || d.Offset.Global >= len(c.Globals) || d.Offset.Global*8+8 > len(globals) {
					return nil, fmt.Errorf("data offset global %d out of range", d.Offset.Global)
				}
				off = uint32(readGlobalSlot(globals, d.Offset.Global, c.Globals[d.Offset.Global].Type))
			}
			end := uint64(off) + uint64(len(d.Bytes))
			if end > uint64(len(lin)) {
				return nil, fmt.Errorf("active data segment %d out of bounds: offset %d + length %d > memory size %d", seg, off, len(d.Bytes), len(lin))
			}
			copy(lin[off:end], d.Bytes)
		}
	}

	success = true
	return &Instance{
		c: c, eng: eng, jm: jm, ar: ar, base: base, mem: mem, hosts: imports.Funcs, hostLog: hostLog, globals: globals,
		serArgs: ar.Alloc(512), results: ar.Alloc(512), trap: ar.Alloc(8),
	}, nil
}

// Close releases the instance's mapped code and memory.
func (in *Instance) Close() {
	runtime.Unmap(in.mem)
	in.ar.Close()
	in.jm.Close()
	in.eng.Close()
}

// LinearMemory exposes the instance's linear memory for zero-copy access.
func (in *Instance) LinearMemory() []byte { return in.jm.LinearMemory() }
