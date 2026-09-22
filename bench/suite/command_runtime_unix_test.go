//go:build !windows

package wagobench

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"

	"github.com/wago-org/wago"
	"github.com/wago-org/wasi/p1"
)

type fixedCommandClock struct{}

func (fixedCommandClock) Realtime() (uint64, uint64, error)   { return 1, 1, nil }
func (fixedCommandClock) Monotonic() (uint64, uint64, error)  { return 1, 1, nil }
func (fixedCommandClock) ProcessCPU() (uint64, uint64, error) { return 0, 1, nil }
func (fixedCommandClock) ThreadCPU() (uint64, uint64, error)  { return 0, 1, nil }

func commandRuntimeImports(m corpusModule, preopenDir string, stdin []byte, stdout, stderr io.Writer) (*wago.Imports, error) {
	switch m.Command.Runtime {
	case "core":
		return nil, nil
	case "wasi", "ashell", "micropython":
		cfg := p1.Config{
			Args: commandArgs(m), Stdin: bytes.NewReader(stdin),
			Stdout: stdout, Stderr: stderr,
			Clocks: fixedCommandClock{},
		}
		if preopenDir != "" {
			cfg.Mounts = []p1.Preopen{{GuestPath: "/", HostPath: preopenDir, Read: true, Write: true, MutateDirectory: true}}
		}
		if m.Command.ReadOnlyPreopen != "" {
			cfg.Mounts = append(cfg.Mounts, p1.Preopen{GuestPath: "/db", HostPath: filepath.Join(corpusDir, m.Command.ReadOnlyPreopen), Read: true})
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
		if m.Command.Runtime == "micropython" {
			imports.HostFunc("micropython_wasm", "host_result_cap", func(_ wago.Caller, call wago.HostCall) {
				call.SetI32(0, 1024)
			}).Results(wago.ValI32)
			// The corpus does not grant Python code any host functions.
			imports.HostFunc("micropython_wasm", "host_call", func(_ wago.Caller, call wago.HostCall) {
				call.SetI32(0, -1)
			}).Params(wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32)
		}
		return imports, nil
	default:
		return nil, fmt.Errorf("unsupported command runtime %q", m.Command.Runtime)
	}
}
