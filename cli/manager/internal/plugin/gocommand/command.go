// Package gocommand runs cancelable Go tool commands. Windows waits for its
// Go-command job to empty; Unix signals a private process group and bounds
// owned output-pipe drains. Unsupported ports cancel only the direct process.
package gocommand

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"time"
)

// Command embeds Cmd so callers can configure its directory, environment, and
// streams as before. Only contexts with Done channels acquire tree tracking.
type Command struct {
	*exec.Cmd
	ctx  context.Context
	tree *commandTree
}

const goCommandWaitDelay = time.Second

func New(ctx context.Context, args ...string) *Command {
	cmd := exec.CommandContext(ctx, "go", args...)
	if ctx.Done() != nil {
		cmd.WaitDelay = goCommandWaitDelay
	}
	return &Command{Cmd: cmd, ctx: ctx}
}

func (command *Command) Start() error {
	if command.ctx.Done() == nil {
		return command.Cmd.Start()
	}
	tree, err := prepareCommandTree(command.Cmd)
	if err != nil {
		return err
	}
	if err := command.Cmd.Start(); err != nil {
		tree.close()
		return err
	}
	if err := tree.attach(command.ctx, command.Cmd); err != nil {
		// A suspended Windows process has no descendants yet. Reap it before
		// returning so its pipes and job do not outlive the staged directory.
		_ = command.Cmd.Process.Kill()
		_ = command.Cmd.Wait()
		tree.close()
		if command.ctx.Err() != nil {
			return command.ctx.Err()
		}
		return err
	}
	if err := command.ctx.Err(); err != nil {
		_ = tree.terminateAndWait()
		_ = tree.beforeWait()
		_ = command.Cmd.Wait()
		tree.close()
		return err
	}
	command.tree = tree
	return nil
}

func (command *Command) Wait() error {
	var err error
	if command.tree != nil {
		// Unix keeps the leader waitable while pipe copies and group signaling
		// complete, so a late cancellation cannot signal a recycled PGID.
		err = command.tree.beforeWait()
	}
	waitErr := command.Cmd.Wait()
	if err == nil {
		err = waitErr
	} else if waitErr != nil {
		err = errors.Join(err, waitErr)
	}
	if command.tree != nil {
		// Wait reaps only the direct Go process. Windows then waits for job
		// members; Unix has already signaled the pinned group before reaping.
		if treeErr := command.tree.terminateAndWait(); treeErr != nil {
			err = errors.Join(err, treeErr)
		}
		command.tree.close()
		command.tree = nil
	}
	if command.ctx.Err() != nil {
		return command.ctx.Err()
	}
	return err
}

func (command *Command) Run() error {
	if command.ctx.Done() == nil {
		return command.Cmd.Run()
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Wait()
}

func (command *Command) CombinedOutput() ([]byte, error) {
	if command.ctx.Done() == nil {
		return command.Cmd.CombinedOutput()
	}
	if command.Stdout != nil || command.Stderr != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return output.Bytes(), err
}

func (command *Command) Output() ([]byte, error) {
	if command.ctx.Done() == nil {
		return command.Cmd.Output()
	}
	if command.Stdout != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	var output bytes.Buffer
	command.Stdout = &output
	var stderr *limitedStderr
	if command.Stderr == nil {
		stderr = &limitedStderr{limit: 32 << 10}
		command.Stderr = stderr
	}
	err := command.Run()
	if exit, ok := err.(*exec.ExitError); ok && stderr != nil {
		exit.Stderr = stderr.Bytes()
	}
	return output.Bytes(), err
}

// Preserve os/exec.Output's first/last 32 KiB diagnostics without permitting
// unbounded stderr memory use during a failing staged build.
type limitedStderr struct {
	limit     int
	prefix    []byte
	suffix    []byte
	suffixOff int
	omitted   int64
}

func (stderr *limitedStderr) Write(data []byte) (int, error) {
	n := len(data)
	if stderr.limit <= 0 {
		return n, nil
	}
	if remain := stderr.limit - len(stderr.prefix); remain > 0 {
		take := min(len(data), remain)
		stderr.prefix = append(stderr.prefix, data[:take]...)
		data = data[take:]
	}
	if len(data) > 0 {
		if len(data) > stderr.limit {
			stderr.omitted += int64(len(data) - stderr.limit)
			data = data[len(data)-stderr.limit:]
		}
		if remain := stderr.limit - len(stderr.suffix); remain > 0 {
			take := min(len(data), remain)
			stderr.suffix = append(stderr.suffix, data[:take]...)
			data = data[take:]
		}
		for len(data) > 0 {
			copied := copy(stderr.suffix[stderr.suffixOff:], data)
			stderr.omitted += int64(copied)
			data = data[copied:]
			stderr.suffixOff += copied
			if stderr.suffixOff == stderr.limit {
				stderr.suffixOff = 0
			}
		}
	}
	return n, nil
}

func (stderr *limitedStderr) Bytes() []byte {
	if stderr.omitted == 0 {
		return append(stderr.prefix, stderr.suffix...)
	}
	var output bytes.Buffer
	output.Grow(len(stderr.prefix) + len(stderr.suffix) + 50)
	output.Write(stderr.prefix)
	output.WriteString("\n... omitting ")
	output.WriteString(strconv.FormatInt(stderr.omitted, 10))
	output.WriteString(" bytes ...\n")
	output.Write(stderr.suffix[stderr.suffixOff:])
	output.Write(stderr.suffix[:stderr.suffixOff])
	return output.Bytes()
}

var _ io.Writer = (*limitedStderr)(nil)
