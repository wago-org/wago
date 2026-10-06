//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const rv64NativeImportTransportBytes = 256 * 8

func railshotNativeContextTrapABI() bool { return true }

// Each retained edge has full-width transport even when the public signature is
// narrow: the producer may make wider internal calls on the same transport.
// Admission bounds the edge count before this footprint is computed.
func railshotNativeImportArenaBytes(c *Compiled, imports resolvedImports) int {
	bytes := 0
	for i := range c.Imports {
		if _, native := imports[c.functionImportBindingKey(i)].(*InstanceExport); native {
			bytes += 2 * rv64NativeImportTransportBytes
		}
	}
	return bytes
}

func railshotNativeContextIdentity(jm *coreruntime.JobMemory) uintptr { return jm.RV64NativeContext() }
func railshotSetNativeContextIdentity(jm *coreruntime.JobMemory, identity uintptr) {
	jm.SetRV64NativeContext(identity)
}

func railshotAlignNativeImportBlob(blob []byte) []byte {
	// Every RV64 thunk consists of fixed four-byte instructions.
	for len(blob)&15 != 0 {
		blob = append(blob, 0x13, 0, 0, 0)
	}
	return blob
}

// This pre-publication check borrows the producer's public invocation lease so a
// concurrent Close cannot clear its native pointers while validation reads them.
// The later attachment repeats lifetime admission under the physical ownership
// lock. Signature and ABI validation rely on the producer's frozen metadata.
func railshotValidateNativeExport(c *Compiled, index int, export *InstanceExport) error {
	if export == nil || export.inst == nil {
		return fmt.Errorf("native instance export is nil")
	}
	producer := export.inst
	if err := producer.beginInvocation(); err != nil {
		return err
	}
	defer producer.endInvocation()
	if err := railshotValidateCompiled(producer.c); err != nil {
		return err
	}
	if producer.hostEvents != nil {
		return deferredHostEventCalleeError()
	}
	if producer.jm == nil || producer.base == 0 || producer.nativeContext == 0 || export.localIdx < 0 || export.localIdx >= len(producer.c.Entry) || export.localIdx >= len(producer.c.Funcs) {
		return fmt.Errorf("native instance export has no retained framed entry")
	}
	if index < 0 || index >= len(c.importFuncSigs) || !sigMatches(c.importFuncSigs[index], c.Types, export, false) {
		return fmt.Errorf("native instance export signature mismatch")
	}
	return nil
}

// Called only after function attachment, which retains the producer through
// failure cleanup and through the importing instance's final physical release.
// A producer logically closed after attachment remains callable by that owner.
func railshotNativeImportCode(c *Compiled, export *InstanceExport, arena *coreruntime.Arena, callerHome, callerContext uintptr, index int) ([]byte, bool, error) {
	if export == nil || export.inst == nil || index < 0 || index >= len(c.importFuncSigs) {
		return nil, true, fmt.Errorf("invalid retained native import")
	}
	producer := export.inst
	if producer.jm == nil || producer.nativeContext == 0 || export.localIdx < 0 || export.localIdx >= len(producer.c.Entry) {
		return nil, true, fmt.Errorf("native import lost its attached producer")
	}
	sig := c.importFuncSigs[index]
	args, err := valTypesSlots(sig.Params)
	if err != nil {
		return nil, true, err
	}
	results, err := valTypesSlots(sig.Results)
	if err != nil {
		return nil, true, err
	}
	code, err := railshot.NativeImportThunk(railshot.NativeImportTarget{
		Entry: producer.base + uintptr(producer.c.Entry[export.localIdx]),
		Home:  producer.jm.LinMemBase(), Context: producer.nativeContext,
		Arguments: arena.Alloc(rv64NativeImportTransportBytes), Results: arena.Alloc(rv64NativeImportTransportBytes),
		ArgumentSlots: args, ResultSlots: results,
	}, callerHome, callerContext, uint32(index))
	return code, true, err
}

// Resolve only within the frozen physically attached function graph. A trap
// carries an opaque context identity; never dereference it or infer an owner
// from a shared memory home. Work and storage are bounded even for cyclic graphs.
// Consume once so a retained TrapError cannot acquire a new owner's name after
// context addresses are recycled. Root admission keeps the graph alive here.
func railshotTrapOwner(root *Instance, trap *coreruntime.TrapError) (*Instance, bool) {
	identity, present, pending := trap.TakeNativeOrigin()
	if !pending {
		return nil, false
	}
	if !present {
		return root, true
	}
	if identity == 0 {
		return nil, true
	}
	const maxOwners, maxEdges = 256, 4096
	var owners [maxOwners + 1]*Instance
	owners[0] = root
	count, edges := 1, 0
	for cursor := 0; cursor < count; cursor++ {
		owner := owners[cursor]
		if owner == nil || owner.c == nil {
			continue
		}
		if owner.nativeContext == identity {
			return owner, true
		}
		for index := range owner.c.Imports {
			if edges == maxEdges {
				return nil, true
			}
			edges++
			export, native := owner.imports[owner.c.functionImportBindingKey(index)].(*InstanceExport)
			if !native || export == nil || export.inst == nil {
				continue
			}
			target := export.inst
			if target.nativeContext == identity {
				return target, true
			}
			seen := false
			for i := 0; i < count; i++ {
				if owners[i] == target {
					seen = true
					break
				}
			}
			if !seen && count < len(owners) {
				owners[count] = target
				count++
			}
		}
	}
	return nil, true
}
