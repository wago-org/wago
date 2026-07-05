package wago

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RestartStrategy selects how a supervisor reacts to a child exit.
type RestartStrategy int

const (
	// OneForOne restarts only the child that exited.
	OneForOne RestartStrategy = iota
	// OneForAll restarts all children when any one exits.
	OneForAll
)

// RestartPolicy selects when an individual child is restarted.
type RestartPolicy int

const (
	// RestartPermanent always restarts the child (default).
	RestartPermanent RestartPolicy = iota
	// RestartTransient restarts only on abnormal exit (error or killed).
	RestartTransient
	// RestartTemporary never restarts the child.
	RestartTemporary
)

// ChildSpec describes one supervised child.
type ChildSpec struct {
	Name    string
	Class   *Class
	Spawn   SpawnOptions
	Restart RestartPolicy
}

// SupervisorOptions configures a supervisor's strategy and restart intensity.
type SupervisorOptions struct {
	Strategy RestartStrategy
	// MaxRestarts within Window before the supervisor gives up and shuts down.
	// Zero MaxRestarts means a single restart trips the limit; set it with Window.
	MaxRestarts int
	Window      time.Duration
}

// Supervisor spawns and monitors a set of child processes, restarting them per
// its strategy until the restart intensity is exceeded.
type Supervisor struct {
	rt       *Runtime
	strategy RestartStrategy
	maxR     int
	window   time.Duration

	events chan supEvent
	done   chan struct{}
	stopMu sync.Once

	mu       sync.Mutex
	children []*supChild
	restarts []time.Time
	stopped  bool
}

type supChild struct {
	spec ChildSpec
	pid  PID
	gen  uint64
	up   bool
}

type supEvent struct {
	idx int
	gen uint64
	ev  ExitEvent
}

// Supervise starts a supervisor over the given children.
func (rt *Runtime) Supervise(ctx context.Context, opts SupervisorOptions, children ...ChildSpec) (*Supervisor, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(children) == 0 {
		return nil, fmt.Errorf("wago: Supervise requires at least one child")
	}
	s := &Supervisor{
		rt: rt, strategy: opts.Strategy, maxR: opts.MaxRestarts, window: opts.Window,
		events: make(chan supEvent, len(children)*2), done: make(chan struct{}),
	}
	for i := range children {
		if children[i].Class == nil {
			return nil, fmt.Errorf("wago: Supervise: child %d (%q) has no class", i, children[i].Name)
		}
		s.children = append(s.children, &supChild{spec: children[i]})
	}
	for i := range s.children {
		if err := s.startChild(ctx, i); err != nil {
			s.Stop()
			return nil, fmt.Errorf("wago: Supervise: starting child %q: %w", s.children[i].spec.Name, err)
		}
	}
	go s.loop()
	return s, nil
}

// startChild spawns child i and wires a forwarder that tags its exit with the
// child's current generation. Caller must not hold s.mu across the spawn only if
// it is the initial start; here we take the lock to update state.
func (s *Supervisor) startChild(ctx context.Context, i int) error {
	s.mu.Lock()
	c := s.children[i]
	gen := c.gen
	spec := c.spec
	s.mu.Unlock()

	pid, err := s.rt.Spawn(ctx, spec.Class, spec.Spawn)
	if err != nil {
		return err
	}
	mon, err := s.rt.Monitor(ctx, pid)
	if err != nil {
		_ = s.rt.Kill(ctx, pid, ExitReason{})
		return err
	}

	s.mu.Lock()
	c.pid = pid
	c.up = true
	s.mu.Unlock()

	go func() {
		select {
		case ev := <-mon:
			select {
			case s.events <- supEvent{idx: i, gen: gen, ev: ev}:
			case <-s.done:
			}
		case <-s.done:
		}
	}()
	return nil
}

// loop processes child exits sequentially.
func (s *Supervisor) loop() {
	for {
		select {
		case <-s.done:
			return
		case e := <-s.events:
			s.handleExit(e)
		}
	}
}

func (s *Supervisor) handleExit(e supEvent) {
	s.mu.Lock()
	if s.stopped || e.idx >= len(s.children) {
		s.mu.Unlock()
		return
	}
	c := s.children[e.idx]
	if e.gen != c.gen {
		s.mu.Unlock() // stale event from a superseded instance
		return
	}
	c.up = false
	restart := shouldRestart(c.spec.Restart, e.ev.Reason)
	if !restart {
		s.mu.Unlock()
		return
	}
	// Record the restart against the intensity window.
	now := time.Now()
	if s.window > 0 {
		cutoff := now.Add(-s.window)
		kept := s.restarts[:0]
		for _, t := range s.restarts {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		s.restarts = kept
	}
	s.restarts = append(s.restarts, now)
	if len(s.restarts) > s.maxR {
		s.mu.Unlock()
		s.Stop() // exceeded restart intensity
		return
	}
	strategy := s.strategy
	// Bump generations for every child we are about to (re)start so their old
	// forwarders' events are ignored.
	var toStart []int
	if strategy == OneForAll {
		for i, ch := range s.children {
			ch.gen++
			if ch.up && i != e.idx {
				pid := ch.pid
				ch.up = false
				go s.rt.Kill(context.Background(), pid, ExitReason{})
			}
			toStart = append(toStart, i)
		}
	} else {
		c.gen++
		toStart = []int{e.idx}
	}
	s.mu.Unlock()

	for _, i := range toStart {
		if err := s.startChild(context.Background(), i); err != nil {
			// A child that fails to restart stays down; surface nothing but keep the
			// supervisor alive for the others.
			continue
		}
	}
}

// shouldRestart applies a child's restart policy to an exit reason.
func shouldRestart(p RestartPolicy, r ExitReason) bool {
	switch p {
	case RestartTemporary:
		return false
	case RestartTransient:
		return !r.Normal
	default: // RestartPermanent
		return true
	}
}

// Children returns the current PIDs of the supervised children, in spec order.
// A child that is currently down reports PID 0.
func (s *Supervisor) Children() []PID {
	s.mu.Lock()
	defer s.mu.Unlock()
	pids := make([]PID, len(s.children))
	for i, c := range s.children {
		if c.up {
			pids[i] = c.pid
		}
	}
	return pids
}

// Stopped reports whether the supervisor has shut down (via Stop or by exceeding
// its restart intensity).
func (s *Supervisor) Stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// Stop shuts the supervisor down and kills all live children.
func (s *Supervisor) Stop() error {
	s.stopMu.Do(func() { close(s.done) })
	s.mu.Lock()
	s.stopped = true
	live := make([]PID, 0, len(s.children))
	for _, c := range s.children {
		if c.up {
			live = append(live, c.pid)
			c.up = false
		}
	}
	s.mu.Unlock()
	for _, pid := range live {
		_ = s.rt.Kill(context.Background(), pid, ExitReason{})
	}
	return nil
}
