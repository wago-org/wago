package wago

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// SnapshotPoolOptions configures an InstancePool. (Named to avoid colliding with
// the Runtime Class system's PoolOptions, which pools instances differently.)
type SnapshotPoolOptions struct {
	// MinIdle instances are created up front (warm start). Must be <= MaxInstances.
	MinIdle int
	// MaxIdle caps how many idle instances are retained; instances released beyond
	// it are discarded. Zero means retain up to MaxInstances.
	MaxIdle int
	// MaxInstances is the hard cap on live instances (idle + leased). Required > 0.
	MaxInstances int
}

// InstancePool is a concurrency-safe pool of instances restored from a single
// Snapshot. Acquiring leases an instance; releasing resets it back to the
// snapshot state and returns it for reuse, which is far cheaper than a fresh
// instantiation. Discarding drops an instance whose state can't be trusted
// (typically after a trap).
type InstancePool struct {
	snap    *Snapshot
	idle    chan *Instance
	sem     chan struct{} // one token per live instance; cap == MaxInstances
	maxIdle int

	closed atomic.Bool

	created   atomic.Uint64
	reused    atomic.Uint64
	discarded atomic.Uint64
	traps     atomic.Uint64

	closeOnce sync.Once
}

// PoolStats is a point-in-time view of an InstancePool.
type PoolStats struct {
	Idle  int // instances ready for reuse
	InUse int // instances currently leased out
	Live  int // Idle + InUse

	Created   uint64 // instances instantiated from the snapshot
	Reused    uint64 // Acquire calls served from an idle instance
	Discarded uint64 // instances torn down (over MaxIdle, or Discard/trap)
	Traps     uint64 // Invoke calls that returned an error
}

// Pool creates an InstancePool over snapshot. MinIdle instances are instantiated
// immediately; the rest are created lazily up to MaxInstances.
func Pool(snapshot *Snapshot, opts SnapshotPoolOptions) (*InstancePool, error) {
	if snapshot == nil || snapshot.c == nil {
		return nil, errors.New("wago: Pool: nil or unbound snapshot")
	}
	if err := validateSnapshotModule(snapshot.c); err != nil {
		return nil, fmt.Errorf("wago: Pool: %w", err)
	}
	if opts.MaxInstances <= 0 {
		return nil, errors.New("wago: Pool requires MaxInstances > 0")
	}
	if opts.MinIdle < 0 || opts.MinIdle > opts.MaxInstances {
		return nil, fmt.Errorf("wago: Pool MinIdle (%d) must be in [0, MaxInstances=%d]", opts.MinIdle, opts.MaxInstances)
	}
	maxIdle := opts.MaxIdle
	if maxIdle <= 0 || maxIdle > opts.MaxInstances {
		maxIdle = opts.MaxInstances
	}
	if maxIdle < opts.MinIdle {
		maxIdle = opts.MinIdle // never retain fewer than the warm-start set
	}
	p := &InstancePool{
		snap: snapshot,
		// idle is sized to maxIdle so a return past the retain cap can't be parked
		// (the default branch in returnClean fires and discards it instead); sem is
		// sized to MaxInstances to bound total live (idle + leased) instances.
		idle:    make(chan *Instance, maxIdle),
		sem:     make(chan struct{}, opts.MaxInstances),
		maxIdle: maxIdle,
	}
	// Warm start: pre-instantiate MinIdle instances, each holding a live-slot token.
	for i := 0; i < opts.MinIdle; i++ {
		p.sem <- struct{}{}
		in, err := p.newInstance()
		if err != nil {
			<-p.sem
			p.Close()
			return nil, fmt.Errorf("wago: Pool warm start: %w", err)
		}
		p.idle <- in
	}
	return p, nil
}

// newInstance restores a fresh instance from the snapshot. The caller must hold a
// live-slot token before calling.
func (p *InstancePool) newInstance() (*Instance, error) {
	in, err := Instantiate(p.snap, InstantiateOptions{})
	if err != nil {
		return nil, err
	}
	p.created.Add(1)
	return in, nil
}

// Acquire leases an instance, blocking until one is available or ctx is done. It
// reuses an idle instance when one exists, otherwise instantiates a new one up to
// MaxInstances.
func (p *InstancePool) Acquire(ctx context.Context) (*SnapshotLease, error) {
	if p.closed.Load() {
		return nil, errors.New("wago: Acquire on closed pool")
	}
	// Prefer an already-warm idle instance.
	select {
	case in := <-p.idle:
		p.reused.Add(1)
		return &SnapshotLease{Instance: in, pool: p}, nil
	default:
	}
	// Otherwise wait for whichever comes first: an idle instance is returned, a
	// live slot frees up (so we can create one), or ctx is cancelled.
	select {
	case in := <-p.idle:
		p.reused.Add(1)
		return &SnapshotLease{Instance: in, pool: p}, nil
	case p.sem <- struct{}{}:
		in, err := p.newInstance()
		if err != nil {
			<-p.sem
			return nil, err
		}
		return &SnapshotLease{Instance: in, pool: p}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Invoke leases an instance, calls export, and releases it — resetting the
// instance to the snapshot state afterwards so successive calls don't share
// state. On error the instance is discarded (its state is untrusted) and the
// trap counter is bumped.
func (p *InstancePool) Invoke(ctx context.Context, export string, args ...uint64) ([]uint64, error) {
	lease, err := p.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	out, err := lease.Instance.Invoke(export, args...)
	if err != nil {
		p.traps.Add(1)
		lease.Discard()
		return nil, err
	}
	// Copy results before Release resets the instance's buffers.
	cp := append([]uint64(nil), out...)
	lease.Release()
	return cp, nil
}

// Close discards every idle instance and marks the pool closed. Leased instances
// are the holder's responsibility (Release/Discard); doing so after Close tears
// them down rather than returning them.
func (p *InstancePool) Close() error {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		for {
			select {
			case in := <-p.idle:
				in.Close()
				<-p.sem
			default:
				return
			}
		}
	})
	return nil
}

// Stats returns a snapshot of pool counters and occupancy.
func (p *InstancePool) Stats() PoolStats {
	idle := len(p.idle)
	live := len(p.sem)
	return PoolStats{
		Idle:      idle,
		InUse:     live - idle,
		Live:      live,
		Created:   p.created.Load(),
		Reused:    p.reused.Load(),
		Discarded: p.discarded.Load(),
		Traps:     p.traps.Load(),
	}
}

// returnClean resets in to the snapshot state and parks it as idle, or discards
// it if the idle cache is full or the reset fails.
func (p *InstancePool) returnClean(in *Instance) {
	if p.closed.Load() {
		p.discard(in)
		return
	}
	if err := in.resetToSnapshot(p.snap); err != nil {
		p.discard(in)
		return
	}
	select {
	case p.idle <- in:
	default:
		p.discard(in) // idle cache full (over MaxIdle): tear this one down
	}
}

// discard tears down in and frees its live slot.
func (p *InstancePool) discard(in *Instance) {
	in.Close()
	p.discarded.Add(1)
	select {
	case <-p.sem:
	default:
	}
}

// SnapshotLease is a borrowed instance from an InstancePool. Access it via
// Instance; return it with Release (reset + reuse) or Discard (tear down).
type SnapshotLease struct {
	Instance *Instance
	pool     *InstancePool
	done     atomic.Bool
}

// Release returns the instance to its pool, resetting it to the snapshot state
// for reuse. It is a no-op if the lease was already released or discarded.
func (l *SnapshotLease) Release() {
	if l == nil || l.done.Swap(true) {
		return
	}
	l.pool.returnClean(l.Instance)
}

// Discard tears the leased instance down instead of returning it — use it after
// a trap or any state the pool shouldn't reuse. No-op if already returned.
func (l *SnapshotLease) Discard() {
	if l == nil || l.done.Swap(true) {
		return
	}
	l.pool.discard(l.Instance)
}
