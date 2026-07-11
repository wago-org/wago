package wago

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"
)

// PreparedFunction is a resolved local Wasm export ready for repeated calls.
// It caches export lookup, signature layout, and the native entry address. Like
// Instance, it is not safe for concurrent calls: calls reuse the instance's
// argument and result buffers, and returned results remain valid only until the
// next call on that instance.
type PreparedFunction struct {
	in          *Instance
	export      string
	entry       uintptr
	paramSlots  int
	resultSlots int
	resultWide  []bool
}

// PrepareFunction resolves a locally-defined function export once. The returned
// handle is the like-for-like counterpart of runtimes whose exported-function
// lookup occurs outside the timed invocation loop. Re-exported imports continue
// to use Invoke because their target instance may differ.
func (in *Instance) PrepareFunction(export string) (*PreparedFunction, error) {
	if in == nil || in.closed {
		return nil, fmt.Errorf("wago: prepare function on closed instance")
	}
	ic := in.findInvokeCache(export)
	if ic == nil {
		var err error
		ic, err = in.fillInvokeCache(export)
		if err != nil {
			return nil, err
		}
	}
	wide := append([]bool(nil), ic.resultWide...)
	return &PreparedFunction{
		in:          in,
		export:      export,
		entry:       in.base + uintptr(in.c.Entry[ic.li]),
		paramSlots:  ic.paramSlots,
		resultSlots: ic.resultSlots,
		resultWide:  wide,
	}, nil
}

// Invoke calls the prepared export. Arguments and results use the same raw slot
// representation and lifetime rules as Instance.Invoke.
func (fn *PreparedFunction) Invoke(args ...uint64) ([]uint64, error) {
	if fn == nil || fn.in == nil || fn.in.closed {
		return nil, fmt.Errorf("wago: invoke closed prepared function")
	}
	in := fn.in
	if len(args) != fn.paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", fn.export, fn.paramSlots, len(args))
	}
	switch len(args) {
	case 0:
	case 1:
		binary.LittleEndian.PutUint64(in.serArgs, args[0])
	case 2:
		binary.LittleEndian.PutUint64(in.serArgs, args[0])
		binary.LittleEndian.PutUint64(in.serArgs[8:], args[1])
	default:
		for i, a := range args {
			binary.LittleEndian.PutUint64(in.serArgs[i*8:], a)
		}
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	if in.syncMode {
		if err := in.callNativeSync(fn.entry); err != nil {
			return nil, err
		}
	} else {
		var err error
		if directPreparedCallEnabled && preparedCallEnabled && in.ownsMem {
			if err = refreshNativeControl(in.nativeControlShared, in.eng, in.jm, in.trap); err == nil {
				err = in.eng.CallPrepared(fn.entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results)
			}
		} else {
			err = callNative(in.c, in.eng, in.jm, in.nativeControlShared, fn.entry, in.serArgs, in.trap, in.results)
		}
		if err != nil {
			return nil, err
		}
		if len(in.hostLog) != 0 {
			if err := in.replayHostLog(); err != nil {
				return nil, err
			}
		}
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:fn.resultSlots]
	if fn.resultSlots == 1 {
		if fn.resultWide[0] {
			out[0] = binary.LittleEndian.Uint64(in.results)
		} else {
			out[0] = uint64(binary.LittleEndian.Uint32(in.results))
		}
		return out, nil
	}
	for i, wide := range fn.resultWide {
		off := i * 8
		if wide {
			out[i] = binary.LittleEndian.Uint64(in.results[off:])
		} else {
			out[i] = uint64(binary.LittleEndian.Uint32(in.results[off:]))
		}
	}
	return out, nil
}
