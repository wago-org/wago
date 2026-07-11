package wago

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ResetPolicy selects how a pooled instance is returned to a reusable initial
// state between leases.
type ResetPolicy int

const (
	// ResetReinstantiate discards the used instance and instantiates a fresh one.
	// This is the always-supported fallback policy.
	ResetReinstantiate ResetPolicy = iota
	// ResetMemorySnapshot restores linear memory and globals from an initial
	// snapshot when the instance is eligible and its current memory is at most one
	// Wasm page. Larger and unsupported shapes fall back to reinstantiation because
	// the bounded JobMemory reuse cache is faster than copying larger images.
	ResetMemorySnapshot
	// ResetCopyOnWrite resets via copy-on-write memory pages. Accepted, currently
	// implemented as reinstantiation until a measured page-level implementation
	// exists.
	ResetCopyOnWrite
)

func (p ResetPolicy) String() string {
	switch p {
	case ResetMemorySnapshot:
		return "memory-snapshot"
	case ResetCopyOnWrite:
		return "copy-on-write"
	default:
		return "reinstantiate"
	}
}

// PoolOptions configures a Class's instance pool.
type PoolOptions struct {
	// MinInstances is the number of instances pre-instantiated at Class creation
	// (warm start). Must be <= MaxInstances.
	MinInstances int
	// MaxInstances is the hard cap on live instances (outstanding + idle). It must
	// be > 0.
	MaxInstances int
	// Reset selects the between-lease reset strategy.
	Reset ResetPolicy
	// MaxIdleTime is reserved for idle-instance reaping; not yet enforced.
	MaxIdleTime time.Duration
	// WarmStart forces pre-instantiation even when MinInstances is 0 (currently a
	// no-op beyond MinInstances).
	WarmStart bool
}

// classSnapshotResetMaxBytes is the measured crossover for in-place Class reset.
// On the pinned reset benchmark, zero/one-page instances are about 18-22% faster
// and drop 9 allocations; at two pages, copying already loses to the bounded
// JobMemory reclaim/reuse path. Re-benchmark before raising this limit.
const classSnapshotResetMaxBytes = 65536

// ClassOptions configures a Class.
type ClassOptions struct {
	Name    string
	Pool    PoolOptions
	Policy  Policy  // capability/resource policy applied to every instance
	Imports Imports // per-class imports merged on top of the runtime's extensions
}

// Class is a deployable unit: a compiled Module plus an instance pool. It is the
// foundation for large instance fleets (and, later, actor processes) — compile
// once, spawn many, share the native code.
type Class struct {
	rt      *Runtime
	mod     *Module
	name    string
	reset   ResetPolicy
	max     int
	imports Imports

	idle    chan *Instance
	mu      sync.Mutex
	created int // live instances: outstanding + idle
	closed  bool

	// snapshot is captured once from the first fresh instance admitted by the
	// memory-snapshot policy. Its byte/global images are immutable and shared by
	// every reset; resetToSnapshot receives a shallow copy rebound to the actual
	// linked *Compiled used by each instance. The atomic pointer keeps both the
	// enabled and measured-fallback release paths free of another mutex.
	snapshotOnce sync.Once
	snapshot     atomic.Pointer[Snapshot]
}

// Class builds a Class over mod with the given pool and options.
func (rt *Runtime) Class(mod *Module, opts ClassOptions) (*Class, error) {
	if mod == nil {
		return nil, fmt.Errorf("wago: Class: nil module")
	}
	max := opts.Pool.MaxInstances
	if max <= 0 {
		return nil, fmt.Errorf("wago: Class requires Pool.MaxInstances > 0")
	}
	min := opts.Pool.MinInstances
	if min < 0 || min > max {
		return nil, fmt.Errorf("wago: Class Pool.MinInstances (%d) must be in [0, MaxInstances=%d]", min, max)
	}
	// The policy applies to every instance of this module; validate it once here.
	if err := applyPolicy(mod, opts.Policy); err != nil {
		return nil, err
	}
	if err := validateClassResetModule(mod.c); err != nil {
		return nil, err
	}
	c := &Class{
		rt: rt, mod: mod, name: opts.Name, reset: opts.Pool.Reset,
		max: max, imports: opts.Imports, idle: make(chan *Instance, max),
	}
	for i := 0; i < min; i++ {
		in, err := c.newInstance(context.Background())
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("wago: Class warm start: %w", err)
		}
		c.created++
		c.idle <- in
	}
	return c, nil
}

func validateClassResetModule(c *Compiled) error {
	if c == nil {
		return fmt.Errorf("wago: Class has no compiled module")
	}
	for i, global := range c.GlobalImports {
		if isReferenceValType(global.Type) {
			return fmt.Errorf("wago: Class pooled reset rejects reference global import %d (%s.%s): imported state cannot be reset between tenants", i, global.Module, global.Name)
		}
	}
	if imports := c.TableImports(); len(imports) != 0 {
		return fmt.Errorf("wago: Class pooled reset rejects reference table imports: imported table state cannot be reset between tenants")
	}
	return nil
}

// Name returns the class name.
func (c *Class) Name() string { return c.name }

// Module returns the class's compiled module.
func (c *Class) Module() *Module { return c.mod }

// ResetPolicy returns the reset policy currently safe for this class. A runtime
// extension may require reinstantiation, in which case an in-place policy is
// transparently downgraded for existing and future classes.
func (c *Class) ResetPolicy() ResetPolicy {
	if c == nil {
		return ResetReinstantiate
	}
	if c.reset != ResetReinstantiate && c.rt.requiresFreshInstanceReset() {
		return ResetReinstantiate
	}
	return c.reset
}

// newInstance instantiates one fresh instance in the class's initial state.
func (c *Class) newInstance(ctx context.Context) (*Instance, error) {
	var (
		in  *Instance
		err error
	)
	if len(c.imports) > 0 {
		in, err = c.rt.Instantiate(ctx, c.mod, WithImports(c.imports))
	} else {
		in, err = c.rt.Instantiate(ctx, c.mod)
	}
	if err != nil {
		return nil, err
	}
	c.captureResetSnapshot(in)
	return in, nil
}

// captureResetSnapshot records one canonical initial state for the
// ResetMemorySnapshot policy. Unsupported snapshot shapes retain the historical
// reinstantiation fallback instead of turning an accepted Class into an error.
func (c *Class) captureResetSnapshot(in *Instance) {
	if c.ResetPolicy() != ResetMemorySnapshot {
		return
	}
	c.snapshotOnce.Do(func() {
		if in == nil || !in.ownsMem || in.c.boundsMode == BoundsChecksSignalsBased {
			return
		}
		if len(in.memory.Bytes()) > classSnapshotResetMaxBytes {
			return
		}
		if err := validateSnapshotModule(in.c); err != nil {
			return
		}
		snapshot := captureInstanceSnapshot(in, SnapshotOptions{Kind: SnapshotInit})
		// The reset path always rebinds a shallow copy to the current instance's
		// linked module. Avoid retaining the first instance-specific link here.
		snapshot.c = c.mod.c
		c.snapshot.Store(snapshot)
	})
}

func (c *Class) resetFromSnapshot(in *Instance) bool {
	if c.ResetPolicy() != ResetMemorySnapshot || in == nil {
		return false
	}
	template := c.snapshot.Load()
	if template == nil || len(in.memory.Bytes()) > classSnapshotResetMaxBytes {
		return false
	}
	// Cross-instance function imports can give each instance a distinct linked
	// Compiled pointer even though the captured Wasm state is identical. Rebind a
	// stack-local shallow copy; the immutable state slices remain shared.
	snapshot := *template
	snapshot.c = in.c
	return in.resetToSnapshot(&snapshot) == nil
}

// Instantiate returns a standalone instance from the class (not pool-managed);
// the caller owns it and must Close it.
func (c *Class) Instantiate(ctx context.Context) (*Instance, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("wago: Instantiate on a closed class")
	}
	return c.newInstance(ctx)
}

// Acquire leases a pooled instance, reusing a warm idle one when available,
// creating a new one while under MaxInstances, or blocking until one is released
// (or ctx is done) at the cap.
func (c *Class) Acquire(ctx context.Context) (*Lease, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Fast path: a warm instance is ready.
	select {
	case in := <-c.idle:
		return &Lease{inst: in, class: c}, nil
	default:
	}
	// Create a new instance while under the cap.
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("wago: Acquire on a closed class")
	}
	if c.created < c.max {
		c.created++
		c.mu.Unlock()
		in, err := c.newInstance(ctx)
		if err != nil {
			c.mu.Lock()
			c.created--
			c.mu.Unlock()
			return nil, err
		}
		return &Lease{inst: in, class: c}, nil
	}
	c.mu.Unlock()
	// At capacity: wait for a release or cancellation.
	select {
	case in := <-c.idle:
		return &Lease{inst: in, class: c}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close releases all idle instances and marks the class unusable. Outstanding
// leases are closed as they are released.
func (c *Class) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	for {
		select {
		case in := <-c.idle:
			in.Close()
			c.mu.Lock()
			c.created--
			c.mu.Unlock()
		default:
			return nil
		}
	}
}

// Lease is a borrowed pooled instance. Use Instance to access it; Release returns
// it to the pool (resetting it to the class's initial state) and invalidates the
// lease.
type Lease struct {
	inst     *Instance
	class    *Class
	released bool
}

// Instance returns the leased instance. It is valid until Release.
func (l *Lease) Instance() *Instance { return l.inst }

// Release resets the instance to the class's initial state and returns capacity
// to the pool. ResetMemorySnapshot reuses eligible explicit-bounds instances in
// place only while every registered extension permits it; otherwise Release
// closes the used instance and installs a fresh replacement.
func (l *Lease) Release() error {
	if l.released {
		return nil
	}
	l.released = true
	c := l.class

	c.mu.Lock()
	if c.closed {
		c.created--
		c.mu.Unlock()
		return l.inst.Close()
	}
	c.mu.Unlock()

	candidate := l.inst
	if !c.resetFromSnapshot(candidate) {
		_ = candidate.Close()
		fresh, err := c.newInstance(context.Background())
		if err != nil {
			c.mu.Lock()
			c.created--
			c.mu.Unlock()
			return fmt.Errorf("wago: Release reset: %w", err)
		}
		candidate = fresh
	}

	// Recheck while holding mu before publishing. Once Close marks the Class
	// closed under the same lock, no release can park a new idle instance after
	// Close's drain has completed.
	c.mu.Lock()
	if c.closed {
		c.created--
		c.mu.Unlock()
		return candidate.Close()
	}
	select {
	case c.idle <- candidate:
		c.mu.Unlock()
	default:
		// Pool unexpectedly full; drop the replacement rather than leak it.
		c.created--
		c.mu.Unlock()
		_ = candidate.Close()
	}
	return nil
}
