package core

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type dispatchTestClock struct{}

func (dispatchTestClock) Realtime() (uint64, uint64, error)   { return 42, 1, nil }
func (dispatchTestClock) Monotonic() (uint64, uint64, error)  { return 42, 1, nil }
func (dispatchTestClock) ProcessCPU() (uint64, uint64, error) { return 42, 1, nil }
func (dispatchTestClock) ThreadCPU() (uint64, uint64, error)  { return 42, 1, nil }

func dispatchTestModule(name string, params, results []wago.ValType) []byte {
	rawType := func(v wago.ValType) byte {
		switch v {
		case wago.ValI32:
			return 0x7f
		case wago.ValI64:
			return 0x7e
		default:
			panic("unexpected WASI value type")
		}
	}
	sig := []byte{0x60, byte(len(params))}
	for _, v := range params {
		sig = append(sig, rawType(v))
	}
	sig = append(sig, byte(len(results)))
	for _, v := range results {
		sig = append(sig, rawType(v))
	}
	imp := append(wasmtest.Name("wasi_snapshot_preview1"), wasmtest.Name(name)...)
	imp = append(imp, 0, 0)
	body := []byte{0}
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x10, 0, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(sig)), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(5, []byte{1, 1, 1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("memory", 2, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestHostCallDoesNotAllocatePluginCopy(t *testing.T) {
	c, err := wago.Compile(nil, dispatchTestModule("args_sizes_get", []wago.ValType{wago.ValI32, wago.ValI32}, []wago.ValType{wago.ValI32}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: Imports("wasi_snapshot_preview1", Config{Args: []string{"command", "one"}})})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(1000, func() {
		r, err := fn.Invoke(0, 4)
		if err != nil || len(r) != 1 || r[0] != 0 {
			t.Fatalf("args_sizes_get: %v %v", r, err)
		}
	})
	if allocs > 1 {
		t.Fatalf("host call allocates %.0f objects, want at most the one Caller interface value", allocs)
	}
}

func TestHostDispatchMatchesReference(t *testing.T) {
	reference := map[string]func(*Plugin, wago.HostModule, []uint64, []uint64){
		"fd_write":                (*Plugin).fdWrite,
		"fd_read":                 (*Plugin).fdRead,
		"fd_close":                (*Plugin).fdClose,
		"fd_seek":                 (*Plugin).fdSeek,
		"fd_fdstat_get":           (*Plugin).fdFdstatGet,
		"fd_prestat_get":          (*Plugin).fdPrestatGet,
		"fd_prestat_dir_name":     (*Plugin).fdPrestatDirName,
		"proc_exit":               (*Plugin).procExit,
		"args_sizes_get":          (*Plugin).argsSizesGet,
		"args_get":                (*Plugin).argsGet,
		"environ_sizes_get":       (*Plugin).environSizesGet,
		"environ_get":             (*Plugin).environGet,
		"clock_time_get":          (*Plugin).clockTimeGet,
		"clock_res_get":           (*Plugin).clockResGet,
		"random_get":              (*Plugin).randomGet,
		"sched_yield":             (*Plugin).schedYield,
		"fd_advise":               (*Plugin).fdAdvise,
		"fd_allocate":             (*Plugin).fdAllocate,
		"fd_datasync":             (*Plugin).fdDatasync,
		"fd_sync":                 (*Plugin).fdSync,
		"fd_fdstat_set_flags":     (*Plugin).fdFdstatSetFlags,
		"fd_fdstat_set_rights":    (*Plugin).fdFdstatSetRights,
		"fd_filestat_get":         (*Plugin).fdFilestatGet,
		"fd_filestat_set_size":    (*Plugin).fdFilestatSetSize,
		"fd_filestat_set_times":   (*Plugin).fdFilestatSetTimes,
		"fd_pread":                (*Plugin).fdPread,
		"fd_pwrite":               (*Plugin).fdPwrite,
		"fd_readdir":              (*Plugin).fdReaddir,
		"fd_renumber":             (*Plugin).fdRenumber,
		"fd_tell":                 (*Plugin).fdTell,
		"path_create_directory":   (*Plugin).pathCreateDirectory,
		"path_filestat_get":       (*Plugin).pathFilestatGet,
		"path_filestat_set_times": (*Plugin).pathFilestatSetTimes,
		"path_link":               (*Plugin).pathLink,
		"path_open":               (*Plugin).pathOpen,
		"path_readlink":           (*Plugin).pathReadlink,
		"path_remove_directory":   (*Plugin).pathRemoveDirectory,
		"path_rename":             (*Plugin).pathRename,
		"path_symlink":            (*Plugin).pathSymlink,
		"path_unlink_file":        (*Plugin).pathUnlinkFile,
		"poll_oneoff":             (*Plugin).pollOneoff,
		"proc_raise":              (*Plugin).procRaise,
		"sock_accept":             (*Plugin).sockAccept,
		"sock_recv":               (*Plugin).sockRecv,
		"sock_send":               (*Plugin).sockSend,
		"sock_shutdown":           (*Plugin).sockShutdown,
	}
	if len(reference) != len(importBindings) {
		t.Fatal("reference does not cover every import")
	}
	for _, b := range importBindings {
		t.Run(b.name, func(t *testing.T) {
			ref := reference[b.name]
			if ref == nil {
				t.Fatal("missing reference")
			}
			c, err := wago.Compile(nil, dispatchTestModule(b.name, b.params, b.results))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, value := range []uint64{0, 0xffffffff} {
				var actualOut, wantOut bytes.Buffer
				cfg := func(out *bytes.Buffer) Config {
					return Config{Args: []string{"command", "argument"}, Env: []string{"KEY=value"}, Stdin: bytes.NewBufferString("input"), Stdout: out, Stderr: out, Rand: bytes.NewReader(make([]byte, 65536)), Clocks: dispatchTestClock{}, Context: context.Background()}
				}
				actual := newTestPlugin(t, cfg(&actualOut))
				want := newTestPlugin(t, cfg(&wantOut))
				defer actual.closeAll()
				defer want.closeAll()
				in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: actual.Imports()})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				p := make([]uint64, len(b.params))
				for i := range p {
					p[i] = value
				}
				expected := make([]uint64, len(b.results))
				memory := make([]byte, 65536)
				var trapped any
				func() { defer func() { trapped = recover() }(); ref(want, testModule{mem: memory}, p, expected) }()
				got, callErr := in.Invoke("run", p...)
				if trapped == nil && callErr != nil || trapped != nil && callErr == nil {
					t.Fatalf("value=%x error=%v reference trap=%v", value, callErr, trapped)
				}
				if trapped != nil {
					expectedExit, ok := trapped.(wago.HostExit)
					var actualExit *wago.ExitError
					if !ok || !errors.As(callErr, &actualExit) || actualExit.Code != expectedExit.Code {
						t.Fatalf("trap=%v reference=%v", callErr, trapped)
					}
				}
				if trapped == nil && !reflect.DeepEqual(got, expected) {
					t.Fatalf("value=%x result=%v reference=%v", value, got, expected)
				}
				if !bytes.Equal(in.Memory().UnsafeBytes(), memory) || actualOut.String() != wantOut.String() {
					t.Fatalf("value=%x guest memory or stream differs", value)
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				actual.closeAll()
				want.closeAll()
			}
		})
	}
}
