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
	case "wasi", "ashell":
		cfg := p1.Config{
			Args: commandArgs(m), Stdin: bytes.NewReader(stdin),
			Stdout: stdout, Stderr: stderr,
			Clocks: fixedCommandClock{},
		}
		if dir := commandPreopen(m); dir != "" {
			cfg.Mounts = []p1.Preopen{{GuestPath: "/", HostPath: dir, Read: true, Write: true, MutateDirectory: true}}
		}
		imports := p1.Imports(cfg)
		if m.Command.Runtime == "ashell" {
			imports.HostFunc(p1.Module, "ashell_getcwd", func(caller wago.Caller, call wago.HostCall) {
				call.SetI32(0, int32(ashellGetcwd(caller.Memory(), uint32(call.I32(0)), uint32(call.I32(1)), uint32(call.I32(2)))))
			}).Params(wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32)
			imports.HostFunc(p1.Module, "ashell_getenv", func(caller wago.Caller, call wago.HostCall) {
				call.SetI32(0, int32(ashellGetenv(caller.Memory(), uint32(call.I32(0)), uint32(call.I32(1)), uint32(call.I32(2)), uint32(call.I32(3)), uint32(call.I32(4)))))
			}).Params(wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32)
			// Guest processes and shell state are outside this isolated command host.
			imports.HostFunc(p1.Module, "ashell_chdir", func(int32, int32) int32 { return ashellErrnoNosys })
			imports.HostFunc(p1.Module, "ashell_system", func(int32, int32) int32 { return ashellErrnoNosys })
		}
		return imports, nil
	default:
		return nil, fmt.Errorf("unsupported command runtime %q", m.Command.Runtime)
	}
}
