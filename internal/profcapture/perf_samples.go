package profcapture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/wago-org/wago/profile"
)

const perfDiagnosticLimit = 32 << 10

type perfDiagnostics struct {
	prefix    []byte
	truncated bool
	cancel    context.CancelFunc
}

func (d *perfDiagnostics) Write(p []byte) (int, error) {
	n := len(p)
	remaining := perfDiagnosticLimit - len(d.prefix)
	if len(p) > remaining {
		p = p[:remaining]
		if !d.truncated && d.cancel != nil {
			d.cancel()
		}
		d.truncated = true
	}
	d.prefix = append(d.prefix, p...)
	return n, nil
}

func (d *perfDiagnostics) err() error {
	if d.truncated {
		return fmt.Errorf("diagnostic output truncated after %d bytes; conversion incomplete", perfDiagnosticLimit)
	}
	return nil
}

func injectPerf(input, output string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "perf", "inject", "--jit", "-i", input, "-o", output)
	cmd.WaitDelay = 2 * time.Second
	diagnostics := perfDiagnostics{cancel: cancel}
	cmd.Stdout, cmd.Stderr = &diagnostics, &diagnostics
	runErr := cmd.Run()
	if err := errors.Join(runErr, diagnostics.err()); err != nil {
		return fmt.Errorf("perf inject: %w: %s", err, strings.TrimSpace(string(diagnostics.prefix)))
	}
	return nil
}

// ReadPerfSamples streams the fixed flat perf output into the bounded parser.
// Parse failure stops and reaps the producer; it never returns partial samples.
// Diagnostic output is separately bounded, and truncation fails conversion.
func ReadPerfSamples(path string, maxSamples int) ([]profile.Sample, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "perf", "script", "--hide-call-graph", "--show-lost-events", "--ns", "-i", path, "-F", "time,ip,period")
	cmd.WaitDelay = 2 * time.Second
	diagnostics := perfDiagnostics{cancel: cancel}
	cmd.Stderr = &diagnostics
	reader, writer := io.Pipe()
	cmd.Stdout = writer
	done := make(chan error, 1)
	go func() {
		err := cmd.Run()
		_ = writer.Close()
		done <- err
	}()
	samples, parseErr := profile.ParsePerfScript(reader, maxSamples)
	// Closing the reader also releases an exec copy goroutine blocked in Write
	// when the parser rejects a line or reaches its record limit.
	_ = reader.Close()
	if parseErr != nil {
		cancel()
	}
	runErr := <-done
	if err := errors.Join(parseErr, runErr, diagnostics.err()); err != nil {
		if detail := strings.TrimSpace(string(diagnostics.prefix)); detail != "" {
			return nil, fmt.Errorf("perf script: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("perf script: %w", err)
	}
	return samples, nil
}
