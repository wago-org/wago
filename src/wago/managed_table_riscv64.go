//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// The managed instance already holds its entry lease and invocation gate. A
// local table wrapper must enter with that instance's authenticated context;
// code compiled for a separate dispatcher cannot borrow the context.
func railshotManagedVoidTableEntry(in *Instance, index uint32) (uintptr, bool, error) {
	table := railshotManagedTableDescriptor(in)
	offset := 8 + uint64(index)*coreruntime.TableEntryBytes
	if offset+coreruntime.TableEntryBytes > uint64(len(table)) {
		return 0, true, fmt.Errorf("managed table entry exceeds retained descriptor extent")
	}
	row := table[offset : offset+coreruntime.TableEntryBytes]
	descriptor := binary.LittleEndian.Uint64(row[coreruntime.TableEntryRefSlotOffset:])
	function, own := in.funcrefDescriptorIndex(descriptor)
	if !own {
		return 0, false, nil
	}
	published, valid := rv64PublishedDescriptor(in, descriptor)
	if !valid || !bytes.Equal(row, published[:coreruntime.TableEntryBytes]) {
		return 0, true, fmt.Errorf("managed table entry differs from its retained published function")
	}
	var signature FuncSig
	if function < in.c.NumImports {
		signature = in.c.importFuncSigs[function]
	} else {
		signature = in.c.Funcs[function-in.c.NumImports]
	}
	if len(signature.Params) != 0 || len(signature.Results) != 0 {
		return 0, true, fmt.Errorf("managed table entry requires a void function")
	}
	return uintptr(binary.LittleEndian.Uint64(row[coreruntime.TableEntryCodePtrOffset:])), true, nil
}

func railshotManagedTableDescriptor(in *Instance) []byte {
	return rv64RetainedTableDescriptor(in, 0)
}
