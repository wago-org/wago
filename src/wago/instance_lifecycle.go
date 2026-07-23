package wago

import (
	"errors"
	"fmt"

	"github.com/wago-org/wago/src/core/runtime"
)

// Close releases the instance's mapped code, engine, and (if instance-owned) its
// memory. An imported memory is left for the host to Close. Close is idempotent.
// Concurrent callers wait for the active close operation and receive its same
// completed, possibly aggregated, error result.
func (in *Instance) Close() (err error) {
	if in == nil {
		return nil
	}
	state, owner := in.beginClose()
	if !owner {
		<-state.done
		return state.result
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if panicErr, ok := recovered.(error); ok {
				err = joinPrimary(err, fmt.Errorf("wago: instance close panicked: %w", panicErr))
			} else {
				err = joinPrimary(err, fmt.Errorf("wago: instance close panicked: %v", recovered))
			}
		}
		state.result = err
		close(state.done)
	}()
	return in.closeOnce()
}

func (in *Instance) beginClose() (*instanceCloseState, bool) {
	state := in.ensurePluginState()
	if active := state.close.Load(); active != nil {
		return active, false
	}
	candidate := &instanceCloseState{done: make(chan struct{})}
	if state.close.CompareAndSwap(nil, candidate) {
		return candidate, true
	}
	return state.close.Load(), false
}

func (in *Instance) closeOnce() error {
	var errs []error
	appendStep := func(phase string, fn func()) {
		if err := callHookSafely(phase, fn); err != nil {
			errs = append(errs, err)
		}
	}

	in.lifeMu.Lock()
	alreadyClosed := in.closed
	in.lifeMu.Unlock()
	if alreadyClosed {
		return nil
	}

	if in.rt != nil {
		for i := len(in.rt.hooks.internalBeforeClose) - 1; i >= 0; i-- {
			fn := in.rt.hooks.internalBeforeClose[i]
			appendStep("internal BeforeClose", func() { fn(in) })
		}
	}

	var hctx *InstanceContext
	if in.rt != nil && (len(in.rt.hooks.beforeClose) != 0 || len(in.rt.hooks.afterClose) != 0) {
		hctx = &InstanceContext{
			Runtime: in.rt, Compiled: in.c, Instance: in, Origin: in.instantiateOrigin(), Metadata: map[string]any{},
		}
		for i := len(in.rt.hooks.beforeClose) - 1; i >= 0; i-- {
			fn := in.rt.hooks.beforeClose[i]
			appendStep("BeforeClose", func() { fn(hctx) })
		}
	}

	// Before marking the instance closed, transfer producer roots to imported
	// funcref tables/globals that still hold any descriptor reachable through this
	// instance: local functions, canonical InstanceExport identities, bare-producer
	// proxies, or HostFuncRef proxies. Retaining the writer preserves its existing
	// transitive attachments. retainResourceRoot refuses a closed instance, so this
	// runs first; the container prunes the root after overwrite or on close.
	appendStep("retain imported table roots", func() { retainProducerRootsInImportedTables(in) })
	appendStep("retain imported global roots", func() { retainProducerRootsInImportedGlobals(in) })

	in.lifeMu.Lock()
	in.closed = true
	shouldRelease := in.resourceRefs == 0
	store := in.refStore
	in.lifeMu.Unlock()

	if store != nil {
		appendStep("close reference store instance", func() { store.instanceClosed(in) })
	}
	if shouldRelease {
		appendStep("release instance resources", in.releaseResources)
	}
	if hctx != nil {
		for i := len(in.rt.hooks.afterClose) - 1; i >= 0; i-- {
			fn := in.rt.hooks.afterClose[i]
			appendStep("AfterClose", func() { fn(hctx) })
		}
	}
	return errors.Join(errs...)
}

func (in *Instance) releaseResources() {
	in.lifeMu.Lock()
	if in.resourcesClosed {
		in.lifeMu.Unlock()
		return
	}
	in.resourcesClosed = true
	in.lifeMu.Unlock()

	// Every imported raw-pointer dependency remains attached until physical
	// release. A table/global/token or downstream function importer may keep this
	// instance's native code and context callable after logical Close.
	detachImportedFunctions(in)
	detachImportedHostFuncRefs(in)
	detachImportedGlobals(in)
	detachImportedTables(in)
	detachImportedTags(in)
	transferredImportAttachments.Delete(in)
	if in.gc != nil {
		closeCollector := func() {
			if table := in.existingGCRefTestTableState(); table != nil {
				table.drop(in.gc)
			}
			in.gc.Close()
		}
		if state := in.existingPublicGCState(); state != nil {
			state.mu.Lock()
			closeCollector()
			state.mu.Unlock()
		} else {
			closeCollector()
		}
	}
	for table := in.table; table != nil; table = table.next {
		table.releaseRetainedInstances()
	}
	unregisterHostControl(in)
	if in.thunkMem != nil {
		runtime.Unmap(in.thunkMem)
		in.thunkMem = nil
	}
	in.c.releaseCode()
	runtime.ReleaseArena(in.ar)
	var detachedMemories importDedup[*Memory]
	if in.memoryDir != nil {
		for i := len(in.memoryDir.memories) - 1; i >= 1; i-- {
			memory := in.memoryDir.memories[i]
			if memory == nil {
				continue
			}
			if i < len(in.memoryDir.owns) && in.memoryDir.owns[i] {
				memoryJM := memory.jobMemory()
				memory.ownerClosed()
				runtime.ReleaseJobMemory(memoryJM)
			} else if detachedMemories.add(memory) {
				memory.detachImporter()
			}
		}
	}
	if in.ownsMem {
		if in.memory != nil {
			in.memory.ownerClosed()
		}
		runtime.ReleaseJobMemory(in.jm)
	} else if in.memory != nil && detachedMemories.add(in.memory) {
		in.memory.detachImporter()
	}
	runtime.ReleaseEngine(in.eng)
}

// Memory returns the instance's linear-memory object (instance-owned or the
// host-imported one). Use Memory().Bytes() for the zero-copy byte view.
func (in *Instance) Memory() *Memory { return in.memory }
