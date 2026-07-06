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

// HostFunc is a host import in reflection-free slot (stack) form: it reads its
// wasm params from params (i32/f32 in the low 32 bits) and writes its results
// into results, with the calling instance's linear memory available through the
// HostModule. It is the single host-import type — it binds identically under
// standard Go and TinyGo — and is the form the reflection convenience (a native
// Go function passed to the low-level Imports on standard Go) compiles down to.
type HostFunc func(m HostModule, params, results []uint64)

// instanceHostModule is the HostModule handed to host functions during a call.
type instanceHostModule struct{ in *Instance }

func (h instanceHostModule) Memory() []byte { return h.in.mem() }

// memHostModule is a standalone HostModule backed by a memory slice, for host
// calls made before the Instance struct exists (e.g. an imported start function).
type memHostModule struct{ mem []byte }

func (h memHostModule) Memory() []byte { return h.mem }

// bindHostImport normalizes an Imports value into a HostFunc for the synchronous
// host-call path, given the import's signature. It accepts a HostFunc or — on
// standard Go — any native Go function whose numeric signature matches sig,
// optionally with a leading HostModule parameter (reflectSyncHost). Under TinyGo,
// only the HostFunc form is available.
func bindHostImport(v any, sig FuncSig) (HostFunc, error) {
	switch f := v.(type) {
	case HostFunc:
		return f, nil
	case nil:
		return nil, fmt.Errorf("no host function provided")
	default:
		return reflectSyncHost(v, sig) // native Go function (standard Go only)
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
		fn, err := bindHostImport(imports[key], c.importFuncSigs[i])
		if err != nil {
			return nil, fmt.Errorf("import %q: %w", key, err)
		}
		hosts[i] = fn
	}
	return hosts, nil
}

// hostDispatch builds the runtime callback the CallWithHost loop invokes: it maps
// the wasm import index to the bound HostFunc and runs it with a HostModule
// bound to this instance.
func (in *Instance) hostDispatch() runtime.HostCall {
	mod := instanceHostModule{in: in}
	return func(importIdx uint32, args, results []uint64) {
		if int(importIdx) < len(in.syncHosts) {
			if fn := in.syncHosts[importIdx]; fn != nil {
				fn(mod, args, results)
			}
		}
	}
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
			panic(r)
		}
	}()
	in.jm.SetStackFence(in.eng.StackLimit())
	return in.eng.CallWithHost(entry, in.serArgs, in.jm.LinearMemory(), in.trap, in.results, in.ctrl, in.hostDispatch())
}
