//go:build !windows

package wagobench

import (
	"bytes"
	"fmt"
	"io"

	"github.com/wago-org/wago"
	"github.com/wago-org/wasi/p1"
)

type fixedCommandClock struct{}

func (fixedCommandClock) Realtime() (uint64, uint64, error)   { return 1, 1, nil }
func (fixedCommandClock) Monotonic() (uint64, uint64, error)  { return 1, 1, nil }
func (fixedCommandClock) ProcessCPU() (uint64, uint64, error) { return 0, 1, nil }
func (fixedCommandClock) ThreadCPU() (uint64, uint64, error)  { return 0, 1, nil }

func commandRuntimeImports(m corpusModule, stdin []byte, stdout, stderr io.Writer) (*wago.Imports, error) {
	switch m.Command.Runtime {
	case "core":
		return nil, nil
	case "wasi":
		cfg := p1.Config{
			Args: commandArgs(m), Stdin: bytes.NewReader(stdin),
			Stdout: stdout, Stderr: stderr,
			Clocks: fixedCommandClock{},
		}
		if dir := commandPreopen(m); dir != "" {
			cfg.Mounts = []p1.Preopen{{GuestPath: "/", HostPath: dir, Read: true, Write: true, MutateDirectory: true}}
		}
		return p1.Imports(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported command runtime %q", m.Command.Runtime)
	}
}
