//go:build !windows

package wagobench

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago"
	core "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"github.com/wago-org/wasi/p1"
)

func lifecycleStateModule(startTrap bool) []byte {
	definitions := []struct {
		name   string
		params []byte
		result bool
	}{
		{"args_sizes_get", []byte{0x7f, 0x7f}, true},
		{"environ_sizes_get", []byte{0x7f, 0x7f}, true},
		{"fd_read", []byte{0x7f, 0x7f, 0x7f, 0x7f}, true},
		{"fd_write", []byte{0x7f, 0x7f, 0x7f, 0x7f}, true},
		{"fd_prestat_dir_name", []byte{0x7f, 0x7f, 0x7f}, true},
		{"path_open", []byte{0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7e, 0x7e, 0x7f, 0x7f}, true},
		{"fd_close", []byte{0x7f}, true},
		{"proc_exit", []byte{0x7f}, false},
		{"args_get", []byte{0x7f, 0x7f}, true},
		{"environ_get", []byte{0x7f, 0x7f}, true},
	}
	var types, imports, funcs, exports, bodies [][]byte
	for i, d := range definitions {
		sig := append([]byte{0x60, byte(len(d.params))}, d.params...)
		if d.result {
			sig = append(sig, 1, 0x7f)
		} else {
			sig = append(sig, 0)
		}
		types = append(types, sig)
		imp := append(wasmtest.Name("wasi_snapshot_preview1"), wasmtest.Name(d.name)...)
		imports = append(imports, append(imp, 0, byte(i)))
		funcs = append(funcs, []byte{byte(i)})
		exports = append(exports, wasmtest.ExportEntry(d.name, 0, uint32(len(definitions)+i)))
		var body []byte
		for j := range d.params {
			body = append(body, 0x20, byte(j))
		}
		body = append(body, 0x10, byte(i), 0x0b)
		bodies = append(bodies, wasmtest.Code(body))
	}
	types = append(types, []byte{0x60, 0, 0})
	funcs = append(funcs, []byte{byte(len(definitions))})
	exports = append(exports, wasmtest.ExportEntry("trap", 0, uint32(2*len(definitions))), wasmtest.ExportEntry("memory", 2, 0))
	bodies = append(bodies, wasmtest.Code([]byte{0, 0x0b}))
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(5, []byte{1, 1, 1, 2}), wasmtest.Section(7, wasmtest.Vec(exports...))}
	if startTrap {
		sections = append(sections, wasmtest.Section(8, []byte{byte(2 * len(definitions))}))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(bodies...)))
	return wasmtest.Module(sections...)
}

func TestImportLifecycleIndependentCommands(t *testing.T) {
	c, err := wago.Compile(nil, lifecycleStateModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, parallel := range []bool{false, true} {
		t.Run(fmt.Sprintf("parallel=%t", parallel), func(t *testing.T) {
			before := core.ProcessNativeMemoryStats()
			t.Cleanup(func() {
				if after := core.ProcessNativeMemoryStats(); after.Supported && after.Active != before.Active {
					t.Errorf("active native mappings: before=%d after=%d", before.Active, after.Active)
				}
			})
			for i := 0; i < 8; i++ {
				t.Run(fmt.Sprint(i), func(t *testing.T) {
					if parallel {
						t.Parallel()
					}
					base := t.TempDir()
					root := filepath.Join(base, "root")
					if err := os.Mkdir(root, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(base, "outside"), []byte("not granted"), 0600); err != nil {
						t.Fatal(err)
					}
					value := byte('A' + i)
					if err := os.WriteFile(filepath.Join(root, "input"), []byte{value}, 0600); err != nil {
						t.Fatal(err)
					}
					args := make([]string, i+1)
					for j := range args {
						args[j] = fmt.Sprint(i, j)
					}
					mount := fmt.Sprintf("/mount%d", i)
					var stdout bytes.Buffer
					im := p1.Imports(p1.Config{Args: args, Env: []string{fmt.Sprintf("KEY=%d", i)}, Stdin: bytes.NewReader([]byte{value, value + 1}), Stdout: &stdout, Mounts: []p1.Preopen{{GuestPath: mount, HostPath: root, Read: true, Write: i%2 == 0}}})
					in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: im})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					mem := in.Memory().UnsafeBytes()
					call := func(name string, args ...uint64) uint64 {
						t.Helper()
						result, err := in.Invoke(name, args...)
						if err != nil || len(result) != 1 {
							t.Fatalf("%s: %v %v", name, result, err)
						}
						return result[0]
					}
					if code := call("args_sizes_get", 0, 4); code != 0 || binary.LittleEndian.Uint32(mem) != uint32(i+1) {
						t.Fatalf("argv: errno=%d memory=%v", code, mem[:8])
					}
					if code := call("environ_sizes_get", 0, 4); code != 0 || binary.LittleEndian.Uint32(mem) != 1 {
						t.Fatalf("environment: errno=%d", code)
					}
					binary.LittleEndian.PutUint32(mem[16:], 64)
					binary.LittleEndian.PutUint32(mem[20:], 1)
					for _, want := range []byte{value, value + 1} {
						if code := call("fd_read", 0, 16, 1, 24); code != 0 || mem[64] != want || binary.LittleEndian.Uint32(mem[24:]) != 1 {
							t.Fatalf("stdin: errno=%d byte=%d want=%d", code, mem[64], want)
						}
					}
					if code := call("fd_write", 1, 16, 1, 24); code != 0 || stdout.String() != string([]byte{value + 1}) {
						t.Fatalf("stdout: errno=%d output=%q", code, stdout.String())
					}
					if code := call("fd_prestat_dir_name", 3, 96, uint64(len(mount))); code != 0 || string(mem[96:96+len(mount)]) != mount {
						t.Fatalf("preopen: errno=%d", code)
					}
					copy(mem[32:], "../outside")
					if code := call("path_open", 3, 0, 32, 10, 0, 2, 0, 0, 128); code != 76 {
						t.Fatalf("path escaped mount or wrong errno: %d", code)
					}
					copy(mem[32:], "input")
					if code := call("path_open", 3, 0, 32, 5, 0, 2, 0, 0, 128); code != 0 {
						t.Fatalf("read open: errno=%d", code)
					}
					fd := uint64(binary.LittleEndian.Uint32(mem[128:]))
					if code := call("fd_read", fd, 16, 1, 24); code != 0 || mem[64] != value {
						t.Fatalf("file isolation: errno=%d byte=%d", code, mem[64])
					}
					if code := call("fd_close", fd); code != 0 {
						t.Fatalf("close file: %d", code)
					}
					code := call("path_open", 3, 0, 32, 5, 0, 64, 0, 0, 128)
					if i%2 == 0 {
						if code != 0 {
							t.Fatalf("write permission: %d", code)
						}
						if code := call("fd_close", uint64(binary.LittleEndian.Uint32(mem[128:]))); code != 0 {
							t.Fatal(code)
						}
					} else if code == 0 {
						t.Fatal("read-only mount allowed write")
					}
					if code := call("fd_close", 3); code != 0 {
						t.Fatalf("close raw WASI preopen: %d", code)
					}
					if i%2 == 0 {
						if _, err := in.Invoke("proc_exit", 0); !commandExitOK(err) {
							t.Fatal(err)
						}
					} else if _, err := in.Invoke("trap"); err == nil {
						t.Fatal("missing trap")
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					if _, err := in.Invoke("args_sizes_get", 0, 4); err == nil {
						t.Fatal("closed instance accepted call")
					}
				})
			}
		})
	}
}

func TestImportLifecycleSetupFailure(t *testing.T) {
	for _, startTrap := range []bool{false, true} {
		c, err := wago.Compile(nil, lifecycleStateModule(startTrap))
		if err != nil {
			t.Fatal(err)
		}
		before := core.ProcessNativeMemoryStats()
		var im *wago.Imports
		if startTrap {
			im = p1.Imports(p1.Config{Args: []string{"failed-start"}})
		}
		in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: im})
		if err == nil {
			in.Close()
			t.Fatal("setup unexpectedly succeeded")
		}
		if in != nil {
			t.Fatal("failed setup returned an instance")
		}
		if after := core.ProcessNativeMemoryStats(); after.Supported && before.Active != after.Active {
			t.Fatalf("setup leaked native mapping: before=%+v after=%+v", before, after)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
