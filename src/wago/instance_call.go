package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// Call is the high-level, context-aware, typed invocation: arguments and results
// are typed Values checked against the export's signature. It wraps the low-level
// Invoke (untyped uint64 slots). ctx is honored for cancellation before the call
// begins. When the instance was created through a Runtime, its BeforeInvoke and
// AfterInvoke hooks fire around the call. Reference Values carry one opaque
// uint64 token slot; non-null funcrefs are valid only in the Runtime store (or
// standalone private store) that issued them. The exact staged basic-struct
// result may also return one bounded GCRef token that must be released through
// its producer Instance. Accepting a reference-typed module remains controlled
// by compiler feature support. v128
// parameters/results are not expressible as a Value; use Invoke for those.
func (in *Instance) Call(ctx context.Context, export string, args ...Value) ([]Value, error) {
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("call %q: %w", export, err)
	}
	defer in.endInvocation()
	state := in.ensurePluginState()
	state.invokeMu.Lock()
	state.invocationID = newInvocationID()
	defer func() {
		state.invocationID = 0
		state.invokeMu.Unlock()
	}()
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	params, results, err := in.c.Signature(export)
	if err != nil {
		return nil, err
	}
	if len(args) != len(params) {
		return nil, fmt.Errorf("%s expects %d arg(s), got %d", export, len(params), len(args))
	}
	slots := make([]uint64, len(args))
	for i, a := range args {
		if params[i] == ValV128 {
			return nil, fmt.Errorf("%s param %d is v128; use Invoke for v128 values", export, i)
		}
		if a.typ != params[i] {
			return nil, fmt.Errorf("%s param %d is %s, got %s", export, i, params[i], a.typ)
		}
		slots[i] = a.bits
	}
	for i, r := range results {
		if r == ValV128 {
			return nil, fmt.Errorf("%s result %d is v128; use Invoke for v128 values", export, i)
		}
	}
	contexts := invocationContextSetFor(ctx)

	// Fast path: no runtime or no invoke hooks — invoke directly under the one
	// already-admitted invocation lease, with no plugin reservation allocation.
	if in.rt == nil {
		out, err := in.callInnerAdmitted(export, slots, results, contexts, nil)
		return out, contextInterruptError(ctx, err)
	}
	hooks := in.rt.loadHooks()
	if len(hooks.beforeInvoke) == 0 && len(hooks.afterInvoke) == 0 {
		out, err := in.callInnerAdmitted(export, slots, results, contexts, nil)
		return out, contextInterruptError(ctx, err)
	}
	reservation, err := reservePluginOperation(hooks.operationGates)
	if err != nil {
		return nil, err
	}
	defer reservation.release()

	request := InvocationRequest{Operation: OperationIdentity{value: &operationIdentityToken{}}, Instance: InstanceIdentity{value: in}, Export: export, Args: append([]Value(nil), args...), Start: time.Now(), reservation: reservation}
	emitAfter := func(event InvocationEvent) error {
		var hookErrs []error
		for _, fn := range hooks.afterInvoke {
			observer := fn
			copyEvent := event
			copyEvent.Results = append([]Value(nil), event.Results...)
			if panicErr := callHookSafely("InstanceInvokeObserver", func() { observer(copyEvent) }); panicErr != nil {
				hookErrs = append(hookErrs, panicErr)
			}
		}
		return errors.Join(hookErrs...)
	}
	for _, fn := range hooks.beforeInvoke {
		interceptor := fn
		copyRequest := request
		copyRequest.Args = append([]Value(nil), request.Args...)
		var interceptErr error
		panicErr := callHookSafely("InstanceInvokeInterceptor", func() { interceptErr = interceptor(copyRequest) })
		if err := joinPrimary(interceptErr, panicErr); err != nil {
			// A BeforeInvoke veto aborts the call; report it to AfterInvoke too so
			// paired hooks can unwind.
			return nil, joinPrimary(err, emitAfter(InvocationEvent{Operation: request.Operation, Instance: request.Instance, Export: export, Err: err, Start: request.Start, reservation: reservation}))
		}
	}
	out, err := in.callInnerAdmitted(export, slots, results, contexts, reservation)
	err = contextInterruptError(ctx, err)
	err = joinPrimary(err, emitAfter(InvocationEvent{Operation: request.Operation, Instance: request.Instance, Export: export, Results: out, Err: err, Start: request.Start, reservation: reservation}))
	return out, err
}

func contextInterruptError(ctx context.Context, err error) error {
	if err == nil || ctx == nil {
		return err
	}
	var trap *wruntime.TrapError
	if errors.As(err, &trap) && trap.Code == wruntime.TrapInterrupted {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
	}
	return err
}

// callInnerAdmitted performs the actual invocation and result decoding under
// the invocation lease already held by Call.
func (in *Instance) callInnerAdmitted(export string, slots []uint64, results []ValType, contexts invocationContextSet, reservation *pluginOperationReservation) ([]Value, error) {
	raw, err := in.invokeAdmitted(export, slots, contexts, reservation)
	if err != nil {
		return nil, err
	}
	out := make([]Value, len(results))
	for i, r := range results {
		out[i] = Value{typ: r, bits: raw[i]}
	}
	return out, nil
}

// GlobalValue returns an exported global's current value, typed. Non-null
// funcrefs are translated from internal descriptors to opaque store-owned tokens;
// non-null externrefs are returned only after exact store validation.
func (in *Instance) GlobalValue(name string) (Value, error) {
	if err := in.beginInvocation(); err != nil {
		return Value{}, fmt.Errorf("global %q: %w", name, err)
	}
	defer in.endInvocation()
	unlockNative := in.lockInstanceNativeStateForHostAccess()
	defer unlockNative()
	idx, err := in.exportedGlobalIndex(name)
	if err != nil {
		return Value{}, err
	}
	g := in.c.Globals[idx]
	if g.Type == ValV128 {
		return Value{}, fmt.Errorf("exported global %q is v128; use GlobalV128", name)
	}
	cell := in.globalCells[idx]
	if isReferenceValType(g.Type) && cell.owner != nil {
		value, err := cell.getValueNoLease()
		if err != nil {
			return Value{}, fmt.Errorf("global %q: %w", name, err)
		}
		return value, nil
	}
	bits := readGlobalObject(cell, g.Type)
	if (g.Type == ValAnyRef || g.Type == ValExnRef) && bits != 0 {
		return Value{}, fmt.Errorf("global %q contains a non-null %s value; public GC/exception reference egress is unsupported", name, g.Type)
	}
	if g.Type == ValFuncRef {
		exact, err := in.c.globalExactType(idx)
		if err != nil {
			return Value{}, fmt.Errorf("global %q exact type: %w", name, err)
		}
		if bits == 0 {
			if exact.Kind == ValueTypeReference && !exact.Ref.Nullable {
				return Value{}, fmt.Errorf("global %q contains null for a non-null reference type", name)
			}
		} else {
			store, err := in.funcrefStoreForEgress()
			if err != nil {
				return Value{}, fmt.Errorf("global %q: invalid funcref value: %w", name, err)
			}
			actual, actualTypes, ok := store.descriptorFuncrefExactType(in, bits)
			if !ok || !valueTypeSubtype(actual, actualTypes, exact, in.c.Types) {
				return Value{}, fmt.Errorf("global %q contains a funcref with an incompatible exact structural type", name)
			}
			token, err := store.issue(in, bits)
			if err != nil {
				return Value{}, fmt.Errorf("global %q: invalid funcref value: %w", name, err)
			}
			bits = token
		}
	}
	if g.Type == ValExternRef && bits != 0 && !in.validExternrefToken(bits) {
		return Value{}, fmt.Errorf("global %q: invalid externref value", name)
	}
	return Value{typ: g.Type, bits: bits}, nil
}

// TableFunctionIndex reports the Wasm function index stored in one entry of an
// exported funcref table. A null entry returns nonNull=false. The method compares
// canonical descriptor identities and never exposes or allocates a reference
// token. It fails if a non-null entry is not a function in this instance's Wasm
// function index space.
func (in *Instance) TableFunctionIndex(name string, entry uint64) (index uint32, nonNull bool, err error) {
	if err := in.beginInvocation(); err != nil {
		return 0, false, fmt.Errorf("table %q: %w", name, err)
	}
	defer in.endInvocation()
	unlockNative := in.lockInstanceNativeStateForHostAccess()
	defer unlockNative()
	if in.c == nil {
		return 0, false, fmt.Errorf("instance has no compiled module")
	}
	tableIndex, ok := in.c.tableExports[name]
	if !ok {
		return 0, false, fmt.Errorf("no exported table %q", name)
	}
	if in.c.tableElementType(tableIndex) != ValFuncRef {
		return 0, false, fmt.Errorf("exported table %q is not a funcref table", name)
	}
	desc := in.tableDescriptor(tableIndex)
	if len(desc) < 8 {
		return 0, false, fmt.Errorf("exported table %q descriptor is invalid", name)
	}
	size := uint64(binary.LittleEndian.Uint32(desc))
	if entry >= size {
		return 0, false, fmt.Errorf("table %q index %d out of bounds (size %d)", name, entry, size)
	}
	stride := in.c.tableEntryBytes(tableIndex)
	if entry > uint64((maxInt()-8-wruntime.TableEntryRefSlotOffset)/stride) {
		return 0, false, fmt.Errorf("table %q index %d overflows host addressing", name, entry)
	}
	offset := 8 + int(entry)*stride + wruntime.TableEntryRefSlotOffset
	if offset < 8 || offset+8 > len(desc) {
		return 0, false, fmt.Errorf("exported table %q descriptor is truncated", name)
	}
	identity := binary.LittleEndian.Uint64(desc[offset:])
	if identity == 0 {
		return 0, false, nil
	}
	functionCount := in.c.NumImports + len(in.c.Funcs)
	for functionIndex := 0; functionIndex < functionCount; functionIndex++ {
		expectedOffset := (functionIndex+1)*wruntime.FuncRefDescBytes + wruntime.TableEntryRefSlotOffset
		if expectedOffset < wruntime.FuncRefDescBytes || expectedOffset+8 > len(in.funcRefDescs) {
			break
		}
		if binary.LittleEndian.Uint64(in.funcRefDescs[expectedOffset:]) == identity {
			return uint32(functionIndex), true, nil
		}
	}
	return 0, false, fmt.Errorf("table %q index %d contains a function outside this instance", name, entry)
}

// SetGlobalValue writes a mutable exported global, checking the value's type
// against the global's declared type. Non-null funcref tokens are resolved and
// non-null externref tokens are validated only through the instance's exact
// reference store before native-visible storage.
func (in *Instance) SetGlobalValue(name string, v Value) error {
	if err := in.beginInvocation(); err != nil {
		return fmt.Errorf("set global %q: %w", name, err)
	}
	defer in.endInvocation()
	unlockNative := in.lockInstanceNativeStateForHostAccess()
	defer unlockNative()
	idx, err := in.exportedGlobalIndex(name)
	if err != nil {
		return err
	}
	g := in.c.Globals[idx]
	if v.typ != g.Type {
		return fmt.Errorf("global %q is %s, got %s", name, g.Type, v.typ)
	}
	if !g.Mutable {
		return fmt.Errorf("exported global %q is immutable", name)
	}
	cell := in.globalCells[idx]
	if isReferenceValType(g.Type) && cell.owner != nil {
		if err := cell.setValueNoLease(v); err != nil {
			return fmt.Errorf("global %q: %w", name, err)
		}
		return nil
	}
	bits := v.bits
	if (g.Type == ValAnyRef || g.Type == ValExnRef) && bits != 0 {
		return fmt.Errorf("global %q: non-null %s ingress is unsupported", name, g.Type)
	}
	if g.Type == ValFuncRef {
		exact, err := in.c.globalExactType(idx)
		if err != nil {
			return fmt.Errorf("global %q exact type: %w", name, err)
		}
		if bits == 0 {
			if exact.Kind == ValueTypeReference && !exact.Ref.Nullable {
				return fmt.Errorf("global %q requires a non-null reference value", name)
			}
		} else {
			if in.refStore == nil {
				return fmt.Errorf("global %q: invalid funcref token", name)
			}
			actual, actualTypes, ok := in.refStore.tokenFuncrefExactType(bits)
			if !ok {
				return fmt.Errorf("global %q: invalid funcref token", name)
			}
			if !valueTypeSubtype(actual, actualTypes, exact, in.c.Types) {
				return fmt.Errorf("global %q: funcref token does not match its exact structural type", name)
			}
			descriptor, ok := in.refStore.resolve(bits)
			if !ok {
				return fmt.Errorf("global %q: invalid funcref token", name)
			}
			bits = descriptor
		}
	}
	if g.Type == ValExternRef && bits != 0 && !in.validExternrefToken(bits) {
		return fmt.Errorf("global %q: invalid externref token", name)
	}
	if g.Type == ValV128 {
		return fmt.Errorf("global %q is v128; use SetGlobalV128", name)
	}
	writeGlobalObject(cell, g.Type, bits)
	return nil
}
