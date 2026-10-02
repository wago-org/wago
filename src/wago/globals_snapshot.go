package wago

import (
	"errors"
	"fmt"
)

// GlobalsSnapshot captures every numeric module-local global of an initialized
// instance. For AssemblyScript's stub runtime this includes the hidden bump
// allocator offset, so Restore rewinds the allocation watermark without
// touching linear-memory pages.
type GlobalsSnapshot struct {
	c       *Compiled
	globals []globalSnap
	cursor  uint64
}

// CaptureGlobals captures all numeric globals after module initialization.
func CaptureGlobals(in *Instance) (*GlobalsSnapshot, error) {
	if in == nil || in.c == nil {
		return nil, errors.New("wago: global snapshot requires a live instance")
	}
	if err := in.c.validateSnapshotReferenceGlobals(); err != nil {
		return nil, err
	}
	return &GlobalsSnapshot{
		c:       in.c,
		globals: capturePageSnapshotGlobals(in),
	}, nil
}

// CaptureStubGlobals captures globals and discovers AssemblyScript stub's
// hidden bump cursor. Newer instrumented modules expose __host_reset_cursor;
// ordinary --exportRuntime binaries expose __new, which can be used as an exact
// one-allocation probe. The probe path restores both globals and linear memory
// before returning, so capture remains byte-identical to post-initialization
// state.
func CaptureStubGlobals(in *Instance) (*GlobalsSnapshot, error) {
	snapshot, err := CaptureGlobals(in)
	if err != nil {
		return nil, err
	}
	memory := in.memory.Bytes()
	memoryBefore := append([]byte(nil), memory...)
	pagesBefore := in.jm.CurrentPages()
	restore := func() {
		_ = snapshot.Restore(in)
		copy(memory, memoryBefore)
	}
	defer restore()

	if _, ok := in.c.Exports["__host_reset_cursor"]; ok {
		if _, err = in.Invoke("__host_reset_cursor"); err != nil {
			return nil, fmt.Errorf("wago: invoke AssemblyScript cursor reset: %w", err)
		}
	} else {
		if _, ok = in.c.Exports["__new"]; !ok {
			return nil, errors.New("wago: AssemblyScript stub cursor discovery requires __host_reset_cursor or __new")
		}
		if _, err = in.Invoke("__new", 0, 0); err != nil {
			return nil, fmt.Errorf("wago: invoke AssemblyScript allocation probe: %w", err)
		}
		if in.jm.CurrentPages() != pagesBefore {
			return nil, errors.New("wago: AssemblyScript allocation probe grew linear memory")
		}
	}

	changed := 0
	for i, snap := range snapshot.globals {
		if i >= len(in.globalCells) || in.globalCells[i] == nil || snap.typ == ValV128 {
			continue
		}
		if readGlobalObject(in.globalCells[i], snap.typ) != snap.bits {
			snapshot.cursor = snap.bits
			changed++
		}
	}
	if changed != 1 {
		return nil, fmt.Errorf("wago: AssemblyScript __reset changed %d numeric globals, want exactly 1", changed)
	}
	if err = snapshot.Restore(in); err != nil {
		return nil, err
	}
	copy(memory, memoryBefore)
	return snapshot, nil
}

// Cursor returns the captured post-initialization bump allocation watermark.
func (s *GlobalsSnapshot) Cursor() uint64 {
	if s == nil {
		return 0
	}
	return s.cursor
}

// Restore writes every captured global back to an instance of the same compiled
// module. Linear memory is deliberately left untouched.
func (s *GlobalsSnapshot) Restore(in *Instance) error {
	if s == nil || s.c == nil {
		return errors.New("wago: nil global snapshot")
	}
	if in == nil || in.c != s.c {
		return errors.New("wago: global snapshot belongs to a different compiled module")
	}
	if len(in.globalCells) != len(s.globals) {
		return fmt.Errorf("wago: global count changed: got %d, want %d", len(in.globalCells), len(s.globals))
	}
	for i, snap := range s.globals {
		if g := in.globalCells[i]; g != nil {
			if snap.typ == ValV128 {
				writeGlobalObjectV128(g, snap.vec)
			} else {
				writeGlobalObject(g, snap.typ, snap.bits)
			}
		}
	}
	return nil
}
