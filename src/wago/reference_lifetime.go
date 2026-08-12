package wago

import "sync"

// referenceLifetimeEvent is the complete interface between instance lifecycle
// and reference-store accounting. Events are idempotent and may arrive in any
// order; the store releases tokens only after the required state converges.
type referenceLifetimeEvent uint8

const (
	referenceLifetimeClosed referenceLifetimeEvent = iota
	referenceLifetimeQuiesced
	referenceLifetimeResourcesReleased
)

// referenceLifetime concentrates the invariant that any reachable reference
// keeps its exact owner physically alive through logical close and invocation
// quiescence. It is a zero-allocation value over an Instance.
type referenceLifetime struct{ instance *Instance }

func (in *Instance) referenceLifetime() referenceLifetime { return referenceLifetime{instance: in} }

func (lifetime referenceLifetime) notifyStore(store *referenceStore, event referenceLifetimeEvent) {
	in := lifetime.instance
	if in == nil || store == nil {
		return
	}
	store.advanceInstanceLifetime(in, event)
}

// finalize owns the exactly-once transition from logically closed to physically
// released. Persistent roots transfer only after invocation quiescence; the
// final locked check resolves concurrent root releases without a cleanup stack.
func (lifetime referenceLifetime) finalize() {
	in := lifetime.instance
	if in == nil {
		return
	}
	in.lifeMu.Lock()
	if !in.closed || in.finalizing || in.resourcesClosed || in.invocationState.Load()&instanceInvocationCount != 0 {
		in.lifeMu.Unlock()
		return
	}
	in.finalizing = true
	store := in.refStore
	in.lifeMu.Unlock()

	retainProducerRootsInImportedTablesForFinalization(in)
	retainProducerRootsInImportedGlobalsForFinalization(in)
	lifetime.notifyStore(store, referenceLifetimeQuiesced)

	in.lifeMu.Lock()
	in.finalizing = false
	if in.resourcesClosed || !in.closed || in.resourceRefs != 0 || in.invocationState.Load()&instanceInvocationCount != 0 {
		in.lifeMu.Unlock()
		return
	}
	in.resourcesClosed = true
	finalizer := in.physicalFinalizer
	in.physicalFinalizer = nil
	in.lifeMu.Unlock()

	lifetime.notifyStore(store, referenceLifetimeResourcesReleased)
	in.releaseResources()
	if finalizer != nil {
		finalizer()
	}
}

func (lifetime referenceLifetime) afterPhysicalRelease(fn func()) {
	in := lifetime.instance
	if in == nil || fn == nil {
		return
	}
	in.lifeMu.Lock()
	if in.resourcesClosed {
		in.lifeMu.Unlock()
		fn()
		return
	}
	if in.physicalFinalizer == nil {
		in.physicalFinalizer = fn
	} else {
		previous := in.physicalFinalizer
		in.physicalFinalizer = func() {
			previous()
			fn()
		}
	}
	in.lifeMu.Unlock()
}

type referenceLifetimeSnapshot struct {
	ResourceRoots     int
	PhysicalResources bool
	LogicallyClosed   bool
}

func (lifetime referenceLifetime) snapshot() referenceLifetimeSnapshot {
	in := lifetime.instance
	if in == nil {
		return referenceLifetimeSnapshot{LogicallyClosed: true}
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	return referenceLifetimeSnapshot{
		ResourceRoots: in.resourceRefs, PhysicalResources: !in.resourcesClosed, LogicallyClosed: in.closed,
	}
}

// transferredImportAttachmentState records table/global attachments whose
// persistent roots took over an importing instance's lifetime obligation.
type transferredImportAttachmentState struct {
	mu      sync.Mutex
	tables  map[*Table]struct{}
	globals map[*Global]struct{}
}

var transferredImportAttachments sync.Map // map[*Instance]*transferredImportAttachmentState

func transferredImportState(in *Instance) *transferredImportAttachmentState {
	state := &transferredImportAttachmentState{}
	actual, _ := transferredImportAttachments.LoadOrStore(in, state)
	return actual.(*transferredImportAttachmentState)
}

func (in *Instance) transferImportedGlobalAttachment(global *Global) {
	if in == nil || global == nil {
		return
	}
	state := transferredImportState(in)
	state.mu.Lock()
	if state.globals == nil {
		state.globals = make(map[*Global]struct{})
	}
	_, exists := state.globals[global]
	if !exists {
		state.globals[global] = struct{}{}
	}
	state.mu.Unlock()
	if !exists {
		global.detachReferenceImporter()
	}
}

func (in *Instance) ownsTransferredGlobalAttachment(global *Global) bool {
	value, ok := transferredImportAttachments.Load(in)
	if !ok {
		return false
	}
	state := value.(*transferredImportAttachmentState)
	state.mu.Lock()
	_, ok = state.globals[global]
	state.mu.Unlock()
	return ok
}

func (in *Instance) transferImportedTableAttachment(table *Table) {
	if in == nil || table == nil {
		return
	}
	state := transferredImportState(in)
	state.mu.Lock()
	if state.tables == nil {
		state.tables = make(map[*Table]struct{})
	}
	_, exists := state.tables[table]
	if !exists {
		state.tables[table] = struct{}{}
	}
	state.mu.Unlock()
	if !exists {
		table.detachImporter()
	}
}

func (in *Instance) ownsTransferredTableAttachment(table *Table) bool {
	value, ok := transferredImportAttachments.Load(in)
	if !ok {
		return false
	}
	state := value.(*transferredImportAttachmentState)
	state.mu.Lock()
	_, ok = state.tables[table]
	state.mu.Unlock()
	return ok
}
