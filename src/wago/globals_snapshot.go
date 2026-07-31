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
