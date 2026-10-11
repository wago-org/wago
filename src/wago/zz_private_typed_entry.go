package wago

import (
	"os"
	goruntime "runtime"
)

var smallTypedEntryEnabled = os.Getenv("WAGO_SMALL_TYPED_ENTRY") != "0"

// The caller selects the immutable single-import integer certificate. This
// driver revalidates mutable ownership under the ordinary invocation gates.
func (in *Instance) tryInvokePrivateTypedI32(export string, args []uint64) (out []uint64, err error, admitted bool) {
	state := in.pluginState.Load()
	if !in.tryBeginOrdinaryInvocation(state) {
		return nil, nil, false
	}
	ic := in.findInvokeCache(export)
	privateRefStore := in.refStore == nil || in.refStore.private
	if ic == nil || !ic.boundedNumericHost || len(args) != int(ic.paramSlots) ||
		!privateRefStore || in.guestStorageBorrowed() || !in.preparedFastStateValid() || !in.usesIndependentExecution() ||
		in.gc != nil || in.threadedMemoryZero || in.table != nil || in.importsFuncrefStorage() || len(in.hostLog) != 0 {
		in.endOrdinaryInvocation(state)
		return nil, nil, false
	}
	p := in.eng.PreparedScalarHost()
	if p == nil || !p.DetachedNumericContext() || !p.IntegerGuestContext() {
		in.endOrdinaryInvocation(state)
		return nil, nil, false
	}
	// No Caller or collector authority is exposed by this scalar-only private
	// owner. Inherited host contexts still need a distinct identity.
	state.invocationID = newInvocationID()
	admitted = true
	nativeOwned := false
	defer func() {
		defer func() { state.invocationID = 0; in.endOrdinaryInvocation(state) }()
		if r := recover(); r != nil {
			if !nativeOwned {
				panic(r)
			}
			setNativeSyncPanicError(r, &err)
		}
		if nativeOwned && err != nil {
			err = in.decorateTrap(err)
		}
	}()
	copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, ic.slotWide[:ic.paramSlots], ic.paramWidthClass)
	nativeOwned = true
	if activeHostInvocationBindings.Load() != 0 {
		restore := bindHostInvocationParent(in, nil)
		defer restore()
	}
	entry := in.base + uintptr(in.c.Entry[ic.li])
	err = p.CallIntegerI32(entry, 1, (func(int32) int32)(in.syncHosts[0].typedI32))
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	if err != nil {
		return nil, err, true
	}
	out = in.resultVals[:ic.resultSlots]
	copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), ic.slotWide[ic.paramSlots:], ic.resultWidthClass)
	return out, nil, true
}
