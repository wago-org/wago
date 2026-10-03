//go:build linux || darwin

package gocommand

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"sync"
	"syscall"
	"time"
)

type copiedStream struct {
	read  *os.File
	write *os.File
	to    io.Writer
}

type commandTree struct {
	killOnce   sync.Once
	killErr    error
	stopCancel func() bool
	cancelDone chan struct{}
	exitDone   chan struct{}
	exitErr    error
	copyDone   chan struct{}
	copyMu     sync.Mutex
	copyErr    error
	streams    []*copiedStream
}

func prepareCommandTree(command *exec.Cmd) (*commandTree, error) {
	tree := new(commandTree)
	// A direct Go process remains waitable until beforeWait finishes. This
	// private group contains ordinary compiler and VCS descendants. Signal
	// it on both cancellation and normal leader exit, before staged cleanup.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// CommandContext's own watcher can run after Process.Wait has reaped the
	// leader. It must never send a numeric process-group signal at that point.
	command.Cancel = func() error { return os.ErrProcessDone }
	if err := tree.prepareStreams(command); err != nil {
		tree.closeStreams()
		return nil, err
	}
	return tree, nil
}

func (tree *commandTree) prepareStreams(command *exec.Cmd) error {
	stdout, stderr := command.Stdout, command.Stderr
	if stdout != nil {
		if _, file := stdout.(*os.File); !file {
			stream, err := newCopiedStream(stdout)
			if err != nil {
				return err
			}
			tree.streams = append(tree.streams, stream)
			command.Stdout = stream.write
		}
	}
	if stderr != nil {
		if _, file := stderr.(*os.File); !file {
			if sameWriter(stdout, stderr) && len(tree.streams) > 0 {
				// CombinedOutput uses one bytes.Buffer for both streams.
				// A shared pipe preserves write ordering and avoids racing it.
				command.Stderr = tree.streams[0].write
			} else {
				stream, err := newCopiedStream(stderr)
				if err != nil {
					return err
				}
				tree.streams = append(tree.streams, stream)
				command.Stderr = stream.write
			}
		}
	}
	return nil
}

func sameWriter(a, b io.Writer) bool {
	return a != nil && b != nil && reflect.TypeOf(a).Comparable() && a == b
}

func newCopiedStream(to io.Writer) (*copiedStream, error) {
	read, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	return &copiedStream{read: read, write: write, to: to}, nil
}

func (tree *commandTree) attach(ctx context.Context, command *exec.Cmd) error {
	pid := command.Process.Pid
	tree.cancelDone = make(chan struct{})
	tree.exitDone = make(chan struct{})
	tree.copyDone = make(chan struct{})
	// Closing the parent's write descriptors before draining makes EOF depend
	// solely on the Go process and its descendants.
	for _, stream := range tree.streams {
		_ = stream.write.Close()
	}
	var copies sync.WaitGroup
	for _, stream := range tree.streams {
		copies.Add(1)
		go func() {
			defer copies.Done()
			_, err := io.Copy(stream.to, stream.read)
			if err != nil {
				// Each stream owns a distinct destination except CombinedOutput,
				// which uses one pipe. The first error is enough for Wait.
				tree.recordCopyError(err)
			}
		}()
	}
	if len(tree.streams) == 0 {
		close(tree.copyDone)
	} else {
		go func() {
			copies.Wait()
			close(tree.copyDone)
		}()
	}
	tree.stopCancel = context.AfterFunc(ctx, func() {
		_ = tree.kill(pid)
		close(tree.cancelDone)
	})
	go func() {
		tree.exitErr = waitDirectExit(pid)
		if tree.exitErr == nil {
			// A successful leader can still leave same-group children. Signal
			// them before returning, just as we do on cancellation.
			_ = tree.kill(pid)
		} else if errors.Is(tree.exitErr, syscall.ECHILD) {
			// If another waiter consumed the child, its numeric group ID is
			// no longer ours to signal. Disarm cancellation immediately.
			tree.stopWatcher()
		} else {
			// An unsupported/broken waitid must not leave a live Go command
			// unbounded. ECHILD is different: the PID may already be recycled.
			_ = tree.kill(pid)
		}
		close(tree.exitDone)
	}()
	return nil
}

func (tree *commandTree) recordCopyError(err error) {
	// Copy failures are exceptional (normal destinations are buffers); using
	// one small mutex avoids extra per-command channels.
	tree.copyMu.Lock()
	if tree.copyErr == nil {
		tree.copyErr = err
	}
	tree.copyMu.Unlock()
}

func (tree *commandTree) kill(pid int) error {
	tree.killOnce.Do(func() { tree.killErr = killProcessGroup(pid) })
	return tree.killErr
}

func (tree *commandTree) beforeWait() error {
	// The platform exit observer leaves the leader unreaped. Until Cmd.Wait
	// runs, its PID/PGID remains pinned, so cancellation and normal-exit
	// signaling cannot target a recycled numeric group ID.
	<-tree.exitDone
	// SIGKILL delivery is asynchronous. Draining inherited output pipes
	// bounds common descendants, but Unix cannot reap grandchildren or prove
	// that every group member has exited (especially with file-backed output).
	// This is a signal-and-drain boundary, not a strict tree-termination wait.
	if tree.exitErr != nil {
		for _, stream := range tree.streams {
			_ = stream.read.Close()
		}
		<-tree.copyDone
		tree.stopWatcher()
		return tree.withSignalError(tree.exitErr)
	}
	if len(tree.streams) > 0 {
		select {
		case <-tree.copyDone:
			// Most commands have already drained their output by leader exit.
		default:
			timer := time.NewTimer(goCommandWaitDelay)
			defer timer.Stop()
			select {
			case <-tree.copyDone:
			case <-timer.C:
				// A detached descendant can keep an output pipe open even after
				// its original process group was signaled. Bound the drain.
				for _, stream := range tree.streams {
					_ = stream.read.Close()
				}
				<-tree.copyDone
				tree.stopWatcher()
				return tree.withSignalError(exec.ErrWaitDelay)
			}
		}
	}
	tree.stopWatcher()
	// A failed group signal must fail the staged build. Otherwise a Go leader
	// could exit successfully while an unsignaled child still uses staged files.
	return tree.withSignalError(tree.copyErr)
}

func (tree *commandTree) withSignalError(err error) error {
	if tree.killErr == nil {
		return err
	}
	return errors.Join(err, tree.killErr)
}

func (tree *commandTree) stopWatcher() {
	if tree.stopCancel == nil {
		return
	}
	if !tree.stopCancel() {
		<-tree.cancelDone
	}
	tree.stopCancel = nil
}

func (tree *commandTree) terminateAndWait() error {
	// beforeWait has already signaled the private group with the leader
	// still waitable; never signal a numeric PGID after Cmd.Wait reaps it.
	return nil
}

func (tree *commandTree) closeStreams() {
	for _, stream := range tree.streams {
		_ = stream.write.Close()
		_ = stream.read.Close()
	}
}

func (tree *commandTree) close() {
	tree.stopWatcher()
	tree.closeStreams()
}

func killProcessGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return normalizeGroupSignalError(pid, err)
}
