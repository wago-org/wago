package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

// PID identifies a spawned process within a Runtime.
type PID uint64

// Guest-visible mailbox status codes (returned by the injected wago_mailbox.*
// imports).
const (
	statusOK          int32 = 0
	statusBufTooSmall int32 = 4
	statusWouldBlock  int32 = 5
	statusTimeout     int32 = 6
	statusClosed      int32 = 7
)

// Process-layer sentinel errors (const so the root facade can re-export them;
// match with errors.Is).
const (
	ErrNoProcess     = extErr("wago: no such process")
	ErrMailboxFull   = extErr("wago: mailbox full")
	ErrMailboxClosed = extErr("wago: mailbox closed")
)

// DefaultMailboxCapacity is the mailbox size used when SpawnOptions.MailboxCapacity
// is zero.
const DefaultMailboxCapacity = 1024

// ExitReason describes why a process ended.
type ExitReason struct {
	Normal  bool    // guest returned without error and was not killed
	Killed  bool    // terminated via Kill or link propagation
	Err     error   // non-nil if the guest trapped or instantiation failed
	Results []Value // the entry function's return values on a normal exit
}

func (r ExitReason) String() string {
	switch {
	case r.Killed:
		return "killed"
	case r.Err != nil:
		return "error: " + r.Err.Error()
	default:
		return "normal"
	}
}

// ExitEvent is delivered to monitors when a process exits.
type ExitEvent struct {
	PID    PID
	Reason ExitReason
}

// SpawnOptions configures a spawned process.
type SpawnOptions struct {
	// Entry is the exported function to run as the process body; defaults to "main".
	Entry string
	// Args are passed to Entry.
	Args []Value
	// Name is an optional human label.
	Name string
	// Policy is the capability/resource policy applied to the process instance.
	Policy Policy
	// Links are existing processes to bidirectionally link the new process to;
	// abnormal exit of either kills the other.
	Links []PID
	// MailboxCapacity bounds the process mailbox (default DefaultMailboxCapacity).
	MailboxCapacity int
}

// Process is a running instance with a mailbox and lifecycle. Processes are not
// green threads: each is a dedicated wago instance driven by its own goroutine.
type Process struct {
	PID   PID
	Name  string
	Class *Class

	mailbox *Mailbox
	inst    *Instance

	mu       sync.Mutex
	exited   bool
	killed   bool
	reason   ExitReason
	monitors []chan ExitEvent
	links    map[PID]struct{}
}

// Spawn starts a new process from a class: it instantiates a dedicated instance
// with per-process wago_process/wago_mailbox imports bound to the process, then
// runs the entry function on its own goroutine. It returns the new PID.
func (rt *Runtime) Spawn(ctx context.Context, class *Class, opts SpawnOptions) (PID, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if class == nil {
		return 0, fmt.Errorf("wago: Spawn: nil class")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	entry := opts.Entry
	if entry == "" {
		entry = "main"
	}
	capN := opts.MailboxCapacity
	if capN <= 0 {
		capN = DefaultMailboxCapacity
	}
	if err := applyPolicy(class.mod, opts.Policy); err != nil {
		return 0, err
	}

	rt.procMu.Lock()
	if rt.closed {
		rt.procMu.Unlock()
		return 0, fmt.Errorf("wago: Spawn on a closed runtime")
	}
	pid := rt.nextPID
	rt.nextPID++
	proc := &Process{PID: pid, Name: opts.Name, Class: class, mailbox: newMailbox(capN), links: map[PID]struct{}{}}
	rt.procs[pid] = proc
	rt.procMu.Unlock()

	inst, err := rt.instantiateProcess(class, proc)
	if err != nil {
		rt.procMu.Lock()
		delete(rt.procs, pid)
		rt.procMu.Unlock()
		return 0, err
	}
	proc.inst = inst

	// Wire requested links now that the process is registered.
	for _, other := range opts.Links {
		_ = rt.Link(ctx, pid, other)
	}

	go func() {
		res, err := inst.Call(ctx, entry, opts.Args...)
		rt.finishProcess(proc, res, err)
	}()
	return pid, nil
}

// instantiateProcess instantiates the class's module with the runtime's extension
// imports plus per-process process/mailbox imports overlaid on top. It bypasses
// the reserved-override and missing-import guards of rt.Instantiate because Spawn
// is trusted infrastructure providing those reserved modules itself.
func (rt *Runtime) instantiateProcess(class *Class, proc *Process) (*Instance, error) {
	rt.mu.Lock()
	merged := make(Imports, len(rt.imports)+len(class.imports)+8)
	for k, v := range rt.imports {
		merged[k] = v
	}
	rt.mu.Unlock()
	for k, v := range class.imports {
		merged[k] = v
	}
	for k, v := range rt.processImports(proc) {
		merged[k] = v // per-process imports take precedence
	}
	inst, err := instantiateCore(class.mod.c, InstantiateOptions{Imports: merged})
	if err != nil {
		return nil, err
	}
	inst.rt = rt // enable Instance.Call invoke hooks for the process body
	return inst, nil
}

// processImports builds the per-process wago_process/wago_mailbox host bindings.
func (rt *Runtime) processImports(proc *Process) Imports {
	mb := proc.mailbox
	return Imports{
		"wago_process.self": HostFunc(func(_ HostModule, _, res []uint64) {
			res[0] = uint64(proc.PID)
		}),
		"wago_mailbox.send": HostFunc(func(m HostModule, p, res []uint64) {
			pid := PID(p[0])
			ptr, n := uint32(p[1]), uint32(p[2])
			mem := m.Memory()
			if int64(ptr)+int64(n) > int64(len(mem)) {
				res[0] = I32(statusBufTooSmall)
				return
			}
			if err := rt.Send(context.Background(), pid, mem[ptr:ptr+n]); err != nil {
				res[0] = I32(statusClosed)
				return
			}
			res[0] = I32(statusOK)
		}),
		"wago_mailbox.send_tagged": HostFunc(func(m HostModule, p, res []uint64) {
			pid, tag := PID(p[0]), p[1]
			ptr, n := uint32(p[2]), uint32(p[3])
			mem := m.Memory()
			if int64(ptr)+int64(n) > int64(len(mem)) {
				res[0] = I32(statusBufTooSmall)
				return
			}
			if err := rt.SendTagged(context.Background(), pid, tag, mem[ptr:ptr+n]); err != nil {
				res[0] = I32(statusClosed)
				return
			}
			res[0] = I32(statusOK)
		}),
		"wago_mailbox.recv": HostFunc(func(m HostModule, p, res []uint64) {
			res[0] = I32(mb.receiveIntoTag(m.Memory(), 0, uint32(p[0]), uint32(p[1]), uint32(p[2]), AsI64(p[3])))
		}),
		"wago_mailbox.recv_tagged": HostFunc(func(m HostModule, p, res []uint64) {
			res[0] = I32(mb.receiveIntoTag(m.Memory(), p[3], uint32(p[0]), uint32(p[1]), uint32(p[2]), AsI64(p[4])))
		}),
		"wago_mailbox.try_recv": HostFunc(func(m HostModule, p, res []uint64) {
			res[0] = I32(mb.receiveIntoTag(m.Memory(), 0, uint32(p[0]), uint32(p[1]), uint32(p[2]), 0))
		}),
		"wago_mailbox.try_recv_tagged": HostFunc(func(m HostModule, p, res []uint64) {
			res[0] = I32(mb.receiveIntoTag(m.Memory(), p[3], uint32(p[0]), uint32(p[1]), uint32(p[2]), 0))
		}),
		"wago_mailbox.len": HostFunc(func(_ HostModule, _, res []uint64) {
			res[0] = I32(int32(mb.length()))
		}),
	}
}

// finishProcess records a process's exit, closes its instance, notifies monitors,
// and propagates abnormal exits to linked processes.
func (rt *Runtime) finishProcess(proc *Process, res []Value, err error) {
	proc.mu.Lock()
	reason := ExitReason{Results: res, Err: err, Killed: proc.killed}
	reason.Normal = err == nil && !proc.killed
	proc.exited = true
	proc.reason = reason
	monitors := proc.monitors
	proc.monitors = nil
	links := make([]PID, 0, len(proc.links))
	for lp := range proc.links {
		links = append(links, lp)
	}
	proc.mu.Unlock()

	// Close the mailbox so later sends fail, and retain the exited Process record
	// (marked exited) so a Monitor that races the exit still finds the real
	// reason rather than a reaped, unknown pid.
	proc.mailbox.close()
	if proc.inst != nil {
		proc.inst.Close()
	}

	ev := ExitEvent{PID: proc.PID, Reason: reason}
	for _, ch := range monitors {
		select {
		case ch <- ev:
		default:
		}
	}
	if !reason.Normal {
		for _, lp := range links {
			_ = rt.Kill(context.Background(), lp, ExitReason{Killed: true})
		}
	}
}

// Send delivers an untagged message to a process's mailbox. The bytes are copied.
func (rt *Runtime) Send(ctx context.Context, pid PID, msg []byte) error {
	return rt.SendTagged(ctx, pid, 0, msg)
}

// SendTagged delivers a tagged message to a process's mailbox. The bytes are
// copied. A tag of zero is the untagged mailbox lane.
func (rt *Runtime) SendTagged(ctx context.Context, pid PID, tag uint64, msg []byte) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	rt.procMu.Lock()
	proc := rt.procs[pid]
	rt.procMu.Unlock()
	if proc == nil {
		return ErrNoProcess
	}
	return proc.mailbox.send(tag, msg)
}

// Kill requests termination of a process. It is cooperative: the mailbox is
// closed so the guest's next receive returns "closed" and the process unwinds;
// a compute-bound guest that never receives cannot be preempted. The process's
// exit is reported to monitors as Killed.
func (rt *Runtime) Kill(ctx context.Context, pid PID, _ ExitReason) error {
	rt.procMu.Lock()
	proc := rt.procs[pid]
	rt.procMu.Unlock()
	if proc == nil {
		return ErrNoProcess
	}
	proc.mu.Lock()
	if proc.exited {
		proc.mu.Unlock()
		return nil
	}
	proc.killed = true
	proc.mu.Unlock()
	proc.mailbox.close()
	return nil
}

// Monitor returns a channel that receives one ExitEvent when the process exits.
// If the process has already exited, the event is delivered immediately.
func (rt *Runtime) Monitor(ctx context.Context, pid PID) (<-chan ExitEvent, error) {
	rt.procMu.Lock()
	proc := rt.procs[pid]
	rt.procMu.Unlock()
	ch := make(chan ExitEvent, 1)
	if proc == nil {
		// Unknown pid: it may have already exited and been reaped. Report a
		// generic normal exit so callers do not block forever.
		ch <- ExitEvent{PID: pid, Reason: ExitReason{Normal: true}}
		return ch, nil
	}
	proc.mu.Lock()
	if proc.exited {
		ev := ExitEvent{PID: pid, Reason: proc.reason}
		proc.mu.Unlock()
		ch <- ev
		return ch, nil
	}
	proc.monitors = append(proc.monitors, ch)
	proc.mu.Unlock()
	return ch, nil
}

// Link bidirectionally links two processes: an abnormal exit of either kills the
// other. Linking is a no-op if either process has already exited.
func (rt *Runtime) Link(ctx context.Context, a, b PID) error {
	rt.procMu.Lock()
	pa, pb := rt.procs[a], rt.procs[b]
	rt.procMu.Unlock()
	if pa == nil || pb == nil {
		return ErrNoProcess
	}
	pa.mu.Lock()
	pa.links[b] = struct{}{}
	pa.mu.Unlock()
	pb.mu.Lock()
	pb.links[a] = struct{}{}
	pb.mu.Unlock()
	return nil
}

// mailboxMessage is one process mailbox item. Tag zero is the untagged lane.
type mailboxMessage struct {
	Tag  uint64
	Data []byte
}

// Mailbox is a bounded FIFO of tagged byte messages delivered to a process.
type Mailbox struct {
	mu     sync.Mutex
	q      []mailboxMessage
	cap    int
	closed bool
	notify chan struct{}
}

func newMailbox(capN int) *Mailbox {
	return &Mailbox{cap: capN, notify: make(chan struct{}, 1)}
}

func (m *Mailbox) send(tag uint64, msg []byte) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrMailboxClosed
	}
	if len(m.q) >= m.cap {
		m.mu.Unlock()
		return ErrMailboxFull
	}
	m.q = append(m.q, mailboxMessage{Tag: tag, Data: append([]byte(nil), msg...)})
	m.mu.Unlock()
	m.signal()
	return nil
}

func (m *Mailbox) close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.signal()
}

func (m *Mailbox) signal() {
	select {
	case m.notify <- struct{}{}:
	default:
	}
}

func (m *Mailbox) length() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.q)
}

// copyTaggedInto copies the first message matching tag into guest memory and pops it
// only after the destination has been validated. It leaves nonmatching messages in
// FIFO order and reports closedNoMatch when no future matching message can arrive.
func (m *Mailbox) copyTaggedInto(mem []byte, tag uint64, bufPtr, bufCap, outLenPtr uint32) (status int32, matched, closedNoMatch bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for idx, msg := range m.q {
		if msg.Tag != tag {
			continue
		}
		n := uint32(len(msg.Data))
		if !writeU32(mem, outLenPtr, n) {
			return statusBufTooSmall, true, false
		}
		if n > bufCap {
			return statusBufTooSmall, true, false // leave the message queued for a larger buffer
		}
		if int64(bufPtr)+int64(n) > int64(len(mem)) {
			return statusBufTooSmall, true, false
		}
		copy(mem[bufPtr:bufPtr+n], msg.Data)
		copy(m.q[idx:], m.q[idx+1:])
		m.q[len(m.q)-1] = mailboxMessage{}
		m.q = m.q[:len(m.q)-1]
		return statusOK, true, false
	}
	return 0, false, m.closed
}

// receiveInto blocks (per timeoutMs: <0 forever, 0 non-blocking, >0 bounded) for an
// untagged message, then writes it into guest memory at bufPtr (up to bufCap bytes)
// and the message length at outLenPtr. It returns a guest status code.
func (m *Mailbox) receiveInto(mem []byte, bufPtr, bufCap, outLenPtr uint32, timeoutMs int64) int32 {
	return m.receiveIntoTag(mem, 0, bufPtr, bufCap, outLenPtr, timeoutMs)
}

// receiveIntoTag blocks (per timeoutMs: <0 forever, 0 non-blocking, >0 bounded) for
// a message with tag, then writes it into guest memory at bufPtr (up to bufCap
// bytes) and the message length at outLenPtr. It returns a guest status code.
func (m *Mailbox) receiveIntoTag(mem []byte, tag uint64, bufPtr, bufCap, outLenPtr uint32, timeoutMs int64) int32 {
	var deadline <-chan time.Time
	if timeoutMs > 0 {
		t := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
		defer t.Stop()
		deadline = t.C
	}
	for {
		status, matched, closedNoMatch := m.copyTaggedInto(mem, tag, bufPtr, bufCap, outLenPtr)
		if matched {
			return status
		}
		if closedNoMatch {
			return statusClosed
		}
		if timeoutMs == 0 {
			return statusWouldBlock
		}
		select {
		case <-m.notify:
		case <-deadline:
			return statusTimeout
		}
	}
}

// writeU32 writes v little-endian at off, returning false if out of range.
func writeU32(mem []byte, off, v uint32) bool {
	if int64(off)+4 > int64(len(mem)) {
		return false
	}
	binary.LittleEndian.PutUint32(mem[off:], v)
	return true
}
