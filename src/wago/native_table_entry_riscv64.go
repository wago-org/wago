//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"strconv"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

// A temporary string used only for a map lookup does not escape. Keep common
// names on the stack; cache declared keys once for arbitrarily long names.
// Neither path scans unrelated entries in the supplied imports map. Instance
// metadata is frozen, and Once publishes the cache to all attached call chains.
func rv64NativeFunctionAttachment(owner *Instance, index int) (*InstanceExport, bool) {
	c := owner.c
	ends, _, _, _, _ := c.importModuleEndSections()
	module, name := splitImportKeyAt(c.Imports[index], importModuleEndAt(ends, index))
	var storage [256]byte
	key := strconv.AppendInt(storage[:0], int64(len(module)), 10)
	if len(key)+1+len(module)+len(name) <= len(storage) {
		key = append(key, ':')
		key = append(key, module...)
		key = append(key, name...)
		export, ok := owner.imports[string(key)].(*InstanceExport)
		return export, ok
	}
	if len(c.Imports) > railshot.MaxHostFunctionImports {
		return nil, false
	}
	state := &owner.ensurePluginState().railshotInvocation
	state.longImportKeysOnce.Do(func() {
		state.longImportKeys = make([]string, len(c.Imports))
		for ordinal := range c.Imports {
			state.longImportKeys[ordinal] = c.functionImportBindingKey(ordinal)
		}
	})
	export, native := owner.imports[state.longImportKeys[index]].(*InstanceExport)
	return export, native
}

// Scope storage is reused after a complete entry. Separate bounded slots retain
// an outer activation's targets while a parked callback recursively reenters.
const rv64MaxTableEntryDepth = 32

type railshotTableEntryLease struct {
	root    *Instance
	targets *rv64NativeTableTargets
	slot    int
}

func rv64NeedsTableEntry(root *Instance) (bool, error) {
	var owners [257]*Instance
	owners[0] = root
	count, edges := 1, 0
	for cursor := 0; cursor < count; cursor++ {
		owner := owners[cursor]
		if owner == nil || owner.c == nil {
			return false, fmt.Errorf("RV64 native attachment has no owner")
		}
		if owner.c.tableCount() != 0 {
			return true, nil
		}
		for index := range owner.c.Imports {
			edges++
			if edges > 4096 {
				return false, fmt.Errorf("RV64 table entry attachment edge bound")
			}
			export, native := rv64NativeFunctionAttachment(owner, index)
			if !native {
				continue
			}
			if export == nil || export.inst == nil {
				return false, fmt.Errorf("RV64 native attachment is nil")
			}
			seen := false
			for _, existing := range owners[:count] {
				if existing == export.inst {
					seen = true
					break
				}
			}
			if seen {
				continue
			}
			if count == len(owners) {
				return false, fmt.Errorf("RV64 table entry owner bound")
			}
			owners[count] = export.inst
			count++
		}
	}
	return false, nil
}

// Called while the selected native lease and root invocation gate are held.
// Its returned lease also owns partial acquisitions on failure; release follows
// native unlocking, including failure before the first guest instruction.
func railshotAcquireNativeTableTargets(root *Instance) (railshotTableEntryLease, error) {
	needed, err := rv64NeedsTableEntry(root)
	if err != nil || !needed {
		return railshotTableEntryLease{}, err
	}
	state := root.ensurePluginState()
	pool := &state.railshotInvocation
	if pool.tableDepth == len(pool.tableTargets) {
		return railshotTableEntryLease{}, fmt.Errorf("RV64 table entry nesting bound")
	}
	slot := pool.tableDepth
	targets := pool.tableTargets[slot]
	if targets == nil {
		targets = new(rv64NativeTableTargets)
		pool.tableTargets[slot] = targets
	}
	if targets.count != 0 {
		return railshotTableEntryLease{}, fmt.Errorf("RV64 table entry slot is active")
	}
	pool.tableDepth++
	lease := railshotTableEntryLease{root: root, targets: targets, slot: slot}
	return lease, targets.refresh(root)
}

func railshotReleaseNativeTableTargets(lease railshotTableEntryLease) {
	if lease.targets == nil {
		return
	}
	pool := &lease.root.pluginState.Load().railshotInvocation
	if pool.tableDepth != lease.slot+1 || pool.tableTargets[lease.slot] != lease.targets {
		panic("RV64 table entry release order")
	}
	pool.tableDepth--
	lease.targets.release()
}

// Every parked-host portal invokes this after its native mutex is reacquired.
// Row replacement can remove a container root, but all previous targets remain
// physically retained until the enclosing entry finishes cold decoration.
func railshotRefreshNativeTableTargets(root *Instance) {
	if root == nil {
		return
	}
	state := root.pluginState.Load()
	if state == nil {
		return
	}
	pool := &state.railshotInvocation
	for index := 0; index < pool.tableDepth; index++ {
		if err := pool.tableTargets[index].refresh(root); err != nil {
			panic(invalidHostReference{err: err})
		}
	}
}

func rv64ActiveTableTrapOwner(root *Instance, identity uintptr) *Instance {
	if root == nil {
		return nil
	}
	state := root.pluginState.Load()
	if state == nil {
		return nil
	}
	pool := &state.railshotInvocation
	for index := pool.tableDepth - 1; index >= 0; index-- {
		if owner := pool.tableTargets[index].owner(identity); owner != nil {
			return owner
		}
	}
	return nil
}
