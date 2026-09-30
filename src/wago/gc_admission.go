package wago

import (
	"context"
	"sync"
)

// gcTopologyGate keeps domain lists stable while admission waits are cancelable.
// Notification storage is needed only when readers or writers contend.
type gcTopologyGate struct {
	mu      sync.Mutex
	readers int
	writer  bool
	writers int
	changed chan struct{}
}

func (g *gcTopologyGate) Lock()  { _ = g.lockContext(context.Background(), true) }
func (g *gcTopologyGate) RLock() { _ = g.lockContext(context.Background(), false) }

func (g *gcTopologyGate) lockContext(ctx context.Context, write bool) error {
	if ctx != nil && ctx.Done() == nil {
		ctx = nil
	}
	g.mu.Lock()
	if write {
		g.writers++
	}
	for {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				if write {
					g.writers--
					g.notifyLocked()
				}
				g.mu.Unlock()
				return err
			}
		}
		if !g.writer && ((write && g.readers == 0) || (!write && g.writers == 0)) {
			if write {
				g.writers--
				g.writer = true
			} else {
				g.readers++
			}
			g.mu.Unlock()
			return nil
		}
		if g.changed == nil {
			g.changed = make(chan struct{})
		}
		changed := g.changed
		g.mu.Unlock()
		if ctx == nil {
			<-changed
		} else {
			select {
			case <-changed:
			case <-ctx.Done():
			}
		}
		g.mu.Lock()
	}
}

func (g *gcTopologyGate) notifyLocked() {
	if g.changed != nil {
		close(g.changed)
		g.changed = nil
	}
}

func (g *gcTopologyGate) Unlock() {
	g.mu.Lock()
	if !g.writer {
		g.mu.Unlock()
		panic("wago: unlock of unheld GC topology writer")
	}
	g.writer = false
	g.notifyLocked()
	g.mu.Unlock()
}

func (g *gcTopologyGate) RUnlock() {
	g.mu.Lock()
	if g.readers == 0 {
		g.mu.Unlock()
		panic("wago: unlock of unheld GC topology reader")
	}
	g.readers--
	if g.readers == 0 {
		g.notifyLocked()
	}
	g.mu.Unlock()
}

func (g *gcTopologyGate) TryLock() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.writer || g.readers != 0 {
		return false
	}
	g.writer = true
	return true
}

func (v gcInvocationDomainView) lockContext(ctx context.Context) error {
	if v.dynamic {
		it := v.iterator(false)
		acquired := 0
		for domain := it.next(); domain != nil; domain = it.next() {
			if err := domain.invocationMu.lockContext(ctx); err != nil {
				reverse := v.iterator(true)
				for i := v.len(); i > 0; i-- {
					previous := reverse.next()
					if i <= acquired {
						previous.invocationMu.Unlock()
					}
				}
				return err
			}
			acquired++
		}
		return nil
	}
	for i := 0; i < v.len(); i++ {
		if err := v.at(i).invocationMu.lockContext(ctx); err != nil {
			for j := i - 1; j >= 0; j-- {
				v.at(j).invocationMu.Unlock()
			}
			return err
		}
	}
	return nil
}
