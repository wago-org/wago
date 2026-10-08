//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

// This borrowed adapter is used only while the invocation's retained owner
// graph holds the particular instance's physical resources. It acquires no
// roots, changes no mapping protections and owns no cleanup. The graph must
// refresh/revalidate it after each park before executing a target again.
type rv64TailCodeOwner struct{ instance *Instance }

func (o rv64TailCodeOwner) NativeTailCodeView() ([]byte, error) {
	in := o.instance
	if in == nil {
		return nil, fmt.Errorf("%w: RV64 published image has no instance", railshot.ErrUnsupported)
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.resourcesClosed || in.c == nil || in.jm == nil || in.ar == nil || in.base == 0 || in.nativeContext == 0 || in.resourceRefs == 0 && in.invocationState.Load()&instanceInvocationCount == 0 {
		return nil, fmt.Errorf("%w: RV64 published image has no physical instance lease", railshot.ErrUnsupported)
	}
	cc := in.c.loadCodeCache()
	if cc == nil {
		return nil, fmt.Errorf("%w: RV64 published image has no code cache", railshot.ErrUnsupported)
	}
	cc.mu.Lock()
	defer cc.mu.Unlock()
	b := in.c.code
	indexes := in.c.loadCompileIndexes()
	// Closing the execution snapshot itself clears its readable code slice,
	// while the particular instance's cache reference keeps the RX mapping live.
	// Recover only that mapping's recorded logical span, never page padding.
	if cc.closed && b == nil && indexes != nil {
		n := indexes.railshotTail.mappedCodeBytes
		if n > 0 && n <= len(cc.mem) {
			b = cc.mem[:n:n]
		}
	}
	// Logical Close is allowed: this particular instance still owns its job,
	// arena/context and code reference. A shared cache's refs alone cannot prove
	// that those resources survive for this instance.
	if !cc.sealed || cc.refs <= 0 || cc.base != in.base || indexes == nil || len(b) == 0 || len(b) != indexes.railshotTail.mappedCodeBytes || len(b) > len(cc.mem) || offHeapSlicePtr(cc.mem) != cc.base || offHeapSlicePtr(b) != cc.base {
		return nil, fmt.Errorf("%w: RV64 published image differs from sealed code cache", railshot.ErrUnsupported)
	}
	return b[:len(b):len(b)], nil
}
