package wago

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ResetPolicy selects how a pooled instance is returned to a reusable initial
// state between leases.
type ResetPolicy int

const (
	// ResetReinstantiate discards the used instance and instantiates a fresh one.
	// This is the fully-supported policy today.
	ResetReinstantiate ResetPolicy = iota
	// ResetMemorySnapshot restores linear memory and globals from an initial
	// snapshot. Accepted, but currently implemented as reinstantiation until the
	// engine grows an in-place reset; behavior (fresh initial state) is identical,
	// only the performance differs.
	ResetMemorySnapshot
	// ResetCopyOnWrite resets via copy-on-write memory pages. Accepted, currently
	// implemented as reinstantiation (see ResetMemorySnapshot).
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

// Name returns the class name.
func (c *Class) Name() string { return c.name }

// Module returns the class's compiled module.
func (c *Class) Module() *Module { return c.mod }

// newInstance instantiates one fresh instance in the class's initial state.
func (c *Class) newInstance(ctx context.Context) (*Instance, error) {
	if len(c.imports) > 0 {
		return c.rt.Instantiate(ctx, c.mod, WithImports(c.imports))
	}
	return c.rt.Instantiate(ctx, c.mod)
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
// to the pool. The reset is currently a reinstantiation: the used instance is
// closed and a fresh one is placed in the pool so the next Acquire stays warm.
func (l *Lease) Release() error {
	if l.released {
		return nil
	}
	l.released = true
	c := l.class

	l.inst.Close()

	c.mu.Lock()
	if c.closed {
		c.created--
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	fresh, err := c.newInstance(context.Background())
	if err != nil {
		c.mu.Lock()
		c.created--
		c.mu.Unlock()
		return fmt.Errorf("wago: Release reset: %w", err)
	}
	select {
	case c.idle <- fresh:
	default:
		// Pool unexpectedly full; drop the replacement rather than leak it.
		fresh.Close()
		c.mu.Lock()
		c.created--
		c.mu.Unlock()
	}
	return nil
}
