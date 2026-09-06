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
// next call on that instance. Invoke must not race Instance.Close.
type PreparedFunction struct {
	in                  *Instance
	export              string
	entry               uintptr
	directEntry         uintptr
	paramSlots          int
	resultSlots         int
	paramTypes          []ValType
	resultTypes         []ValType
	paramExact          []ValueTypeDescriptor
	resultExact         []ValueTypeDescriptor
	hasReferenceParams  bool
	hasReferenceResults bool
	scalarWideMask      uint8
	scalarFast          bool
	scalarResultWide    bool
	resultWide          []bool
	privateFast         bool
	isolatedFast        bool
	directIntFast       bool
}

func (c *Compiled) directPreparedAt(local int) bool {
	return c != nil && local >= 0 && local < len(c.InternalEntry) && directPreparedEntry(c.InternalEntry[local])
}

func preparedDirectIntSignature(sig FuncSig) bool {
	if len(sig.Params) > 4 || len(sig.Results) > 1 {
		return false
	}
	for _, typ := range sig.Params {
		if typ != ValI32 && typ != ValI64 {
			return false
		}
	}
	for _, typ := range sig.Results {
		if typ != ValI32 && typ != ValI64 {
			return false
		}
	}
	return true
}

// PrepareFunction resolves a locally-defined function export once. The returned
// handle is the like-for-like counterpart of runtimes whose exported-function
// lookup occurs outside the timed invocation loop. Re-exported imports continue
// to use Invoke because their target instance may differ.
func (in *Instance) PrepareFunction(export string) (*PreparedFunction, error) {
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("wago: prepare function: %w", err)
	}
	defer in.endInvocation()
	ic := in.findInvokeCache(export)
	if ic == nil {
		var err error
		ic, err = in.fillInvokeCache(export)
		if err != nil {
			return nil, err
		}
	}
	if ic.li < 0 {
		return nil, fmt.Errorf("wago: prepare function %q: re-exported imports must use Invoke", export)
	}
	if in.c == nil || ic.li >= len(in.c.Entry) || ic.li >= len(in.c.Funcs) {
		return nil, fmt.Errorf("wago: prepare function %q: local function index %d is out of range", export, ic.li)
	}
	sig := in.c.Funcs[ic.li]
	params, results, err := exactFuncSignatureView(sig, in.c.Types)
	if err != nil {
		return nil, fmt.Errorf("wago: prepare function %q exact signature: %w", export, err)
	}
	wide := append([]bool(nil), ic.resultWide...)
	scalarFast := preparedScalarFastEnabled &&
		!hasReferenceValType(sig.Params) &&
		!hasReferenceValType(sig.Results) &&
		ic.paramSlots <= 4 &&
		ic.resultSlots <= 1
	var scalarWideMask uint8
	if scalarFast {
		slot := 0
		for _, typ := range sig.Params {
			if typ == ValV128 {
				scalarWideMask |= 3 << slot
				slot += 2
			} else {
				if isWideValType(typ) {
					scalarWideMask |= 1 << slot
				}
				slot++
			}
		}
	}
	fn := &PreparedFunction{
		in:                  in,
		export:              export,
		entry:               in.base + uintptr(in.c.Entry[ic.li]),
		paramSlots:          ic.paramSlots,
		resultSlots:         ic.resultSlots,
		scalarWideMask:      scalarWideMask,
		scalarFast:          scalarFast,
		scalarResultWide:    ic.resultSlots == 1 && wide[0],
		paramTypes:          append([]ValType(nil), sig.Params...),
		resultTypes:         append([]ValType(nil), sig.Results...),
		paramExact:          append([]ValueTypeDescriptor(nil), params...),
		resultExact:         append([]ValueTypeDescriptor(nil), results...),
		hasReferenceParams:  hasReferenceValType(sig.Params),
		hasReferenceResults: hasReferenceValType(sig.Results),
		resultWide:          wide,
	}
	if scalarFast && preparedCallEnabled && preparedPrivateEntryEnabled && in.preparedPrivateEligible() {
		fn.privateFast = true
		fn.isolatedFast = preparedIsolatedEntryEnabled && in.preparedIsolatedEligible()
		if (fn.isolatedFast || preparedDirectIntPrivateSupported) && preparedDirectIntSupported && preparedDirectIntEnabled && preparedDirectIntSignature(sig) && in.c.directPreparedAt(ic.li) {
			fn.directIntFast = true
			fn.directEntry = in.base + uintptr(internalEntryOffset(in.c.InternalEntry[ic.li]))
		}
	}
	return fn, nil
}

// Invoke calls the prepared export. Arguments and results use the same raw slot
// representation and lifetime rules as Instance.Invoke.
func (fn *PreparedFunction) Invoke(args ...uint64) ([]uint64, error) {
	if fn != nil && fn.in != nil && len(args) == fn.paramSlots {
		if fn.directIntFast {
			switch len(args) {
			case 0:
				return fn.invokeDirectIntFixed(0, 0, 0, 0)
			case 1:
				return fn.invokeDirectIntFixed(args[0], 0, 0, 0)
			case 2:
				return fn.invokeDirectIntFixed(args[0], args[1], 0, 0)
			case 3:
				return fn.invokeDirectIntFixed(args[0], args[1], args[2], 0)
			case 4:
				return fn.invokeDirectIntFixed(args[0], args[1], args[2], args[3])
			}
		}
		if fn.scalarFast {
			return fn.invokeScalar(args)
		}
	}
	return fn.invokeGeneral(args)
}

type preparedInvocationLease struct {
	state *instancePluginState
	gc    gcInvocationLease
}

func (in *Instance) lockPreparedInvocation() preparedInvocationLease {
	state := in.ensurePluginState()
	state.invokeMu.Lock()
	id := newInvocationID()
	state.invocationID = id
	return preparedInvocationLease{state: state, gc: in.lockGCInvocation(id)}
}

func (l preparedInvocationLease) unlock() {
	l.gc.unlock()
	l.state.invocationID = 0
	l.state.invokeMu.Unlock()
}

// Invoke0 calls a prepared export with no argument slots. Unlike the variadic
// Invoke, fixed-arity calls do not require TinyGo to allocate an argument slice.
func (fn *PreparedFunction) Invoke0() ([]uint64, error) {
	return fn.invokeFixed(0, 0, 0, 0, 0)
}

// Invoke1 calls a prepared export with one argument slot.
func (fn *PreparedFunction) Invoke1(a0 uint64) ([]uint64, error) {
	return fn.invokeFixed(1, a0, 0, 0, 0)
}

// Invoke2 calls a prepared export with two argument slots.
func (fn *PreparedFunction) Invoke2(a0, a1 uint64) ([]uint64, error) {
	return fn.invokeFixed(2, a0, a1, 0, 0)
}

// Invoke3 calls a prepared export with three argument slots.
func (fn *PreparedFunction) Invoke3(a0, a1, a2 uint64) ([]uint64, error) {
	return fn.invokeFixed(3, a0, a1, a2, 0)
}

// Invoke4 calls a prepared export with four argument slots.
func (fn *PreparedFunction) Invoke4(a0, a1, a2, a3 uint64) ([]uint64, error) {
	return fn.invokeFixed(4, a0, a1, a2, a3)
}

func (fn *PreparedFunction) invokeFixed(count int, a0, a1, a2, a3 uint64) ([]uint64, error) {
	if fn == nil || fn.in == nil {
		return nil, fmt.Errorf("wago: invoke closed prepared function")
	}
	if count != fn.paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", fn.export, fn.paramSlots, count)
	}
	if fn.directIntFast {
		return fn.invokeDirectIntFixed(a0, a1, a2, a3)
	}
	args := [4]uint64{a0, a1, a2, a3}
	if fn.scalarFast {
		return fn.invokeScalar(args[:count])
	}
	return fn.invokeGeneral(args[:count])
}

func (fn *PreparedFunction) invokeGeneral(args []uint64) ([]uint64, error) {
	if fn == nil || fn.in == nil {
		return nil, fmt.Errorf("wago: invoke closed prepared function")
	}
	in := fn.in
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("wago: invoke prepared function: %w", err)
	}
	defer in.endInvocation()
	// Prepared calls share the same instance buffers and Runtime GC domain as
	// Invoke. Publish an invocation identity under the instance gate so host
	// callbacks can suspend the domain lease, and retain that lease through public
	// reference-result tokenization.
	preparedLease := in.lockPreparedInvocation()
	defer preparedLease.unlock()
	if len(args) != fn.paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", fn.export, fn.paramSlots, len(args))
	}
	if fn.hasReferenceParams {
		defer in.clearGCRefArgumentRoots()
		if err := in.marshalPublicReferenceArgs(fn.export, args, fn.paramTypes, fn.paramExact); err != nil {
			return nil, err
		}
	} else {
		marshalPublicScalarArgs(in.serArgs, args, fn.paramTypes)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	if in.syncMode {
		if err := in.callNativeSync(fn.entry); err != nil {
			return nil, err
		}
	} else {
		prepared := directPreparedCallEnabled && preparedCallEnabled && in.ownsMem
		err := in.callNativeAsync(fn.entry, prepared)
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
		if fn.hasReferenceResults {
			if err := in.translatePublicReferenceResults(fn.export, out, fn.resultTypes, fn.resultExact); err != nil {
				return nil, err
			}
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
	if fn.hasReferenceResults {
		if err := in.translatePublicReferenceResults(fn.export, out, fn.resultTypes, fn.resultExact); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (fn *PreparedFunction) invokeScalar(args []uint64) ([]uint64, error) {
	in := fn.in
	if fn.privateFast {
		if in.isLogicallyClosed() {
			return nil, fmt.Errorf("wago: invoke prepared function: instance is closed")
		}
	} else {
		if err := in.beginInvocation(); err != nil {
			return nil, fmt.Errorf("wago: invoke prepared function: %w", err)
		}
		defer in.endInvocation()
		preparedLease := in.lockPreparedInvocation()
		defer preparedLease.unlock()
	}
	put := func(slot int) {
		bits := args[slot]
		if fn.scalarWideMask&(1<<slot) == 0 {
			bits = uint64(uint32(bits))
		}
		binary.LittleEndian.PutUint64(in.serArgs[slot*8:], bits)
	}
	switch len(args) {
	case 4:
		put(3)
		fallthrough
	case 3:
		put(2)
		fallthrough
	case 2:
		put(1)
		fallthrough
	case 1:
		put(0)
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
		if fn.isolatedFast {
			err = in.callPreparedIsolated(fn.entry, in.trap)
		} else if fn.privateFast {
			err = in.callPreparedPrivate(fn.entry, in.trap)
		} else {
			prepared := directPreparedCallEnabled && preparedCallEnabled && in.ownsMem
			err = in.callNativeAsync(fn.entry, prepared)
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
		if fn.scalarResultWide {
			out[0] = binary.LittleEndian.Uint64(in.results)
		} else {
			out[0] = uint64(binary.LittleEndian.Uint32(in.results))
		}
	}
	return out, nil
}
