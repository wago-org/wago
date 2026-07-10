package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/runtime"
)

// HostModule gives a synchronous host import access to the instance that called
// it. It is passed as the optional leading parameter of a host function.
type HostModule interface {
	// Memory returns the calling instance's linear memory as a mutable slice
	// (empty if the module declares no memory). Writes are visible to wasm; the
	// slice is valid only for the duration of the host call.
	Memory() []byte
}

// ExternRefHostModule is the optional reference-store surface implemented by the
// HostModule value wago passes to callbacks. Keeping it separate preserves the
// minimal HostModule interface for existing mocks and wrappers.
type ExternRefHostModule interface {
	HostModule
	// NewExternRef registers an embedder object in the calling instance's
	// compatible reference store and returns a non-null opaque token.
	NewExternRef(any) (ExternRef, error)
	// ExternRefValue resolves a token from the calling instance's compatible
	// store. Forged, stale, and incompatible-store tokens return false.
	ExternRefValue(ExternRef) (any, bool)
}

// HostFunc is a host import in reflection-free slot (stack) form: it reads its
// wasm params from params (i32/f32 in the low 32 bits) and writes its results
// into results, with the calling instance's linear memory and externref store
// available through HostModule. A reference occupies one opaque uint64 slot; a
// v128 occupies two adjacent little-endian uint64 slots, matching Invoke's public
// ABI. It is the single host-import type — it binds identically
// under standard Go and TinyGo — with no reflection anywhere on the path.
type HostFunc func(m HostModule, params, results []uint64)

// instanceHostModule is the HostModule handed to host functions during a call.
type instanceHostModule struct{ in *Instance }

func (h instanceHostModule) Memory() []byte { return h.in.mem() }
func (h instanceHostModule) NewExternRef(value any) (ExternRef, error) {
	return h.in.NewExternRef(value)
}
func (h instanceHostModule) ExternRefValue(ref ExternRef) (any, bool) {
	return h.in.ExternRefValue(ref)
}

// bindHostImport normalizes an Imports value into a HostFunc for the synchronous
// host-call path. The only accepted host-function form is a HostFunc (the stack
// form); any other value is an error. There is no reflection: host imports bind
// identically under standard Go and TinyGo.
func bindHostImport(v any, _ FuncSig) (HostFunc, error) {
	switch f := v.(type) {
	case HostFunc:
		if f == nil {
			return nil, fmt.Errorf("host function is nil")
		}
		return f, nil
	case nil:
		return nil, fmt.Errorf("no host function provided")
	default:
		return nil, fmt.Errorf("host import must be a wago.HostFunc (stack form); got %T", v)
	}
}

// buildSyncHosts resolves every function import of a sync-mode module to a
// HostFunc, indexed by import function index. c.Imports lists the function
// imports in order; c.importFuncSigs (set by linkModule) holds their signatures.
func (c *Compiled) buildSyncHosts(imports Imports) ([]HostFunc, error) {
	hosts := make([]HostFunc, len(c.Imports))
	for i, key := range c.Imports {
		if i >= len(c.importFuncSigs) {
			return nil, fmt.Errorf("import %q: missing signature", key)
		}
		// A cross-instance binding is a native call, not a host function; skip it.
		if _, cross := imports[key].(*InstanceExport); cross {
			continue
		}
		sig := c.importFuncSigs[i]
		paramSlots, err := valTypesSlots(sig.Params)
		if err != nil {
			return nil, fmt.Errorf("import %q params: %w", key, err)
		}
		resultSlots, err := valTypesSlots(sig.Results)
		if err != nil {
			return nil, fmt.Errorf("import %q results: %w", key, err)
		}
		if paramSlots > runtime.MaxHostArity || resultSlots > runtime.MaxHostArity {
			return nil, fmt.Errorf("import %q uses %d param slot(s), %d result slot(s); synchronous host imports support at most %d slots in each direction", key, paramSlots, resultSlots, runtime.MaxHostArity)
		}
		fn, err := bindHostImport(imports[key], sig)
		if err != nil {
			return nil, fmt.Errorf("import %q: %w", key, err)
		}
		hosts[i] = fn
	}
	return hosts, nil
}

type missingHostFunc struct{ importIdx uint32 }
type invalidHostReference struct{ err error }

// newHostDispatch builds the runtime callback the CallWithHost loop invokes: it
// maps the wasm import index to the bound HostFunc and runs it with a HostModule
// bound to this instance. It is constructed once at instantiation so hot Invoke
// paths do not allocate a fresh closure per call.
func (in *Instance) newHostDispatch() runtime.HostCall {
	mod := instanceHostModule{in: in}
	return func(importIdx uint32, args, results []uint64) {
		if int(importIdx) >= len(in.syncHosts) || in.syncHosts[importIdx] == nil {
			panic(missingHostFunc{importIdx: importIdx})
		}
		if int(importIdx) >= len(in.c.importFuncSigs) {
			panic(invalidHostReference{err: fmt.Errorf("host import %d has no signature", importIdx)})
		}
		sig := in.c.importFuncSigs[importIdx]
		if err := in.validateHostExternrefs(args, sig.Params, "argument"); err != nil {
			panic(invalidHostReference{err: fmt.Errorf("host import %d: %w", importIdx, err)})
		}
		in.syncHosts[importIdx](mod, args, results)
		if err := in.validateHostExternrefs(results, sig.Results, "result"); err != nil {
			panic(invalidHostReference{err: fmt.Errorf("host import %d: %w", importIdx, err)})
		}
	}
}

func (in *Instance) validateHostExternrefs(values []uint64, types []ValType, direction string) error {
	slot := 0
	for i, typ := range types {
		if typ == ValV128 {
			slot += 2
			continue
		}
		if slot >= len(values) {
			return fmt.Errorf("missing %s slot %d", direction, slot)
		}
		if typ == ValExternRef && values[slot] != 0 && !in.validExternrefToken(values[slot]) {
			return fmt.Errorf("invalid externref token for %s %d", direction, i)
		}
		slot++
	}
	return nil
}

// HostExit, panicked by a host function, terminates the current Invoke and
// surfaces as an *ExitError. It lets a host import (e.g. WASI proc_exit) end
// execution without returning to wasm; the abandoned foreign-stack frames are
// reset on the engine's next entry.
type HostExit struct{ Code int32 }

// ExitError is returned by Invoke when a host function requested termination via
// panic(HostExit{...}). A zero code is a normal exit.
type ExitError struct{ Code int32 }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// callNativeSync runs a native entry that may make synchronous host calls,
// driving the re-entry loop with this instance's host dispatch. A host function
// may panic(HostExit{...}) to terminate; it is recovered here as an *ExitError.
func (in *Instance) callNativeSync(entry uintptr) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if ex, ok := r.(HostExit); ok {
				err = &ExitError{Code: ex.Code}
				return
			}
			if missing, ok := r.(missingHostFunc); ok {
				err = fmt.Errorf("missing host function for import index %d", missing.importIdx)
				return
			}
			if invalid, ok := r.(invalidHostReference); ok {
				err = invalid.err
				return
			}
			panic(r)
		}
	}()
	in.jm.SetStackFence(in.eng.StackLimit())
	if in.hostCall == nil {
		in.hostCall = in.newHostDispatch()
	}
	return in.eng.CallWithHost(entry, in.serArgs, in.jm.LinearMemory(), in.trap, in.results, in.ctrl, in.hostCall)
}
