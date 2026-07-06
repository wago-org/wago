// Package core is the shared implementation behind the versioned WASI plugins.
// The minimal snapshot surface (stdio, args/env, clock, random, exit) is the same
// across wasi_unstable (pre-preview1) and wasi_snapshot_preview1; only the wasm
// import module name and extension identity differ, so both wrap this package with
// their own module string. It is internal: use the plugins/wasi/p1 or
// plugins/wasi/unstable wrappers.
package core

import (
	"crypto/rand"
	"encoding/binary"
	"io"

	wago "github.com/wago-org/wago"
)

// Cap is the capability guarding the whole WASI surface. A policy can allow or
// deny it; with no policy it is permitted.
const Cap = wago.CapWASI

// WASI errno values (subset used here); identical across snapshots.
const (
	wasiOK      = 0
	wasiEBadf   = 8
	wasiEInval  = 28
	wasiESpipe  = 29
	wasiENosys  = 52
	wasiENotsup = 58
)

// Config configures the WASI host bundle. A nil writer/reader discards/EOFs; a
// nil Now yields a fixed clock (handy for deterministic tests); a nil Rand uses
// crypto/rand.
type Config struct {
	Stdout, Stderr io.Writer
	Stdin          io.Reader
	Args           []string     // argv; Args[0] is conventionally the program name
	Env            []string     // "KEY=VALUE" entries
	Now            func() int64 // wall-clock nanoseconds for clock_time_get
	Rand           io.Reader    // random source for random_get
}

// Extension is a WASI extension bound to one wasm import module name. p1 and
// unstable construct it with their own module string and identity. It implements
// wago.Extension.
type Extension struct {
	module string
	info   wago.ExtensionInfo
	cfg    Config
}

// New builds a WASI extension that binds its imports under module, identifying
// itself with info.
func New(module string, info wago.ExtensionInfo, cfg Config) *Extension {
	return &Extension{module: module, info: info, cfg: cfg}
}

// Imports returns the host bundle for module on the low-level
// wago.Instantiate(c, imports) path, keyed "<module>.<name>".
func Imports(module string, cfg Config) wago.Imports {
	return New(module, wago.ExtensionInfo{}, cfg).Imports()
}

// Info identifies the extension.
func (e *Extension) Info() wago.ExtensionInfo { return e.info }

// Register wires the host imports onto reg under the extension's module name.
func (e *Extension) Register(reg *wago.Registry) error {
	reg.Capability(Cap, wago.CapabilityDocs("wasi: stdio, args/env, clock, random, process exit"))
	m := reg.ImportModule(e.module)
	for _, b := range e.bindings() {
		m.Func(b.name, b.fn).Params(b.params...).Results(b.results...).Capability(Cap).Docs(b.docs)
	}
	return nil
}

// Imports returns the host bundle for the low-level wago.Instantiate(c, imports)
// path, keyed "<module>.<name>".
func (e *Extension) Imports() wago.Imports {
	out := make(wago.Imports)
	for _, b := range e.bindings() {
		out[e.module+"."+b.name] = b.fn
	}
	return out
}

// binding is one host function with its declared signature and docs. Register and
// Imports both derive from bindings so the plugin and raw-bundle paths never drift.
type binding struct {
	name            string
	fn              wago.HostFunc
	params, results []wago.ValType
	docs            string
}

// errStub is a host function that ignores its args and returns a fixed errno —
// used for the parts of the snapshot wago does not implement, so a guest (which
// links imports for the whole surface) still instantiates and gets a clean error
// rather than a missing-import failure.
func errStub(errno uint64) wago.HostFunc {
	return func(_ wago.HostModule, _, r []uint64) {
		if len(r) > 0 {
			r[0] = errno
		}
	}
}

func (e *Extension) bindings() []binding {
	i32 := []wago.ValType{wago.ValI32}
	i32x2 := []wago.ValType{wago.ValI32, wago.ValI32}
	i32x3 := []wago.ValType{wago.ValI32, wago.ValI32, wago.ValI32}
	i32x4 := []wago.ValType{wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32}
	i64 := wago.ValI64
	i32v := wago.ValI32

	stub := func(name string, errno uint64, docs string) binding {
		return binding{name: name, fn: errStub(errno), results: i32, docs: docs}
	}

	return []binding{
		{"fd_write", e.fdWrite, i32x4, i32, "write iovecs to a file descriptor (stdout/stderr)"},
		{"fd_read", e.fdRead, i32x4, i32, "read into iovecs from a file descriptor (stdin)"},
		{"fd_close", e.fdClose, i32, i32, "close a file descriptor (streams: no-op)"},
		{"fd_seek", e.fdSeek, []wago.ValType{i32v, i64, i32v, i32v}, i32, "seek a file descriptor (streams: ESPIPE)"},
		{"fd_fdstat_get", e.fdFdstatGet, i32x2, i32, "report fd stat (streams: character device)"},
		{"fd_prestat_get", e.fdPrestatGet, i32x2, i32, "report a preopen (none: EBADF)"},
		{"fd_prestat_dir_name", e.fdPrestatDirName, i32x3, i32, "report a preopen dir name (none: EBADF)"},
		{"proc_exit", e.procExit, i32, nil, "terminate the program with an exit code"},
		{"args_sizes_get", e.argsSizesGet, i32x2, i32, "report argc and argv byte size"},
		{"args_get", e.argsGet, i32x2, i32, "write argv pointers and bytes"},
		{"environ_sizes_get", e.environSizesGet, i32x2, i32, "report environ count and byte size"},
		{"environ_get", e.environGet, i32x2, i32, "write environ pointers and bytes"},
		{"clock_time_get", e.clockTimeGet, []wago.ValType{i32v, i64, i32v}, i32, "read a clock's current time"},
		{"clock_res_get", e.clockResGet, i32x2, i32, "read a clock's resolution"},
		{"random_get", e.randomGet, i32x2, i32, "fill a buffer with random bytes"},

		// Benign no-ops (hints / flushes / cooperative yield): success.
		stub("sched_yield", wasiOK, "cooperative yield (no-op)"),
		stub("fd_advise", wasiOK, "access-pattern hint (no-op)"),
		stub("fd_datasync", wasiOK, "flush data (no-op)"),
		stub("fd_sync", wasiOK, "flush (no-op)"),
		stub("fd_fdstat_set_flags", wasiOK, "set fd flags (no-op)"),

		// Not implemented (filesystem, sockets, polling, timers): a clean errno so
		// guests fall back gracefully instead of failing to instantiate.
		stub("fd_allocate", wasiENosys, "not implemented"),
		stub("fd_fdstat_set_rights", wasiENosys, "not implemented"),
		stub("fd_filestat_get", wasiENosys, "not implemented"),
		stub("fd_filestat_set_size", wasiENosys, "not implemented"),
		stub("fd_filestat_set_times", wasiENosys, "not implemented"),
		stub("fd_pread", wasiENosys, "not implemented"),
		stub("fd_pwrite", wasiENosys, "not implemented"),
		stub("fd_readdir", wasiENosys, "not implemented"),
		stub("fd_renumber", wasiENosys, "not implemented"),
		stub("fd_tell", wasiESpipe, "streams are not seekable"),
		stub("path_create_directory", wasiENosys, "not implemented"),
		stub("path_filestat_get", wasiENosys, "not implemented"),
		stub("path_filestat_set_times", wasiENosys, "not implemented"),
		stub("path_link", wasiENosys, "not implemented"),
		stub("path_open", wasiEBadf, "no preopened dirs"),
		stub("path_readlink", wasiENosys, "not implemented"),
		stub("path_remove_directory", wasiENosys, "not implemented"),
		stub("path_rename", wasiENosys, "not implemented"),
		stub("path_symlink", wasiENosys, "not implemented"),
		stub("path_unlink_file", wasiENosys, "not implemented"),
		stub("poll_oneoff", wasiENosys, "not implemented"),
		stub("proc_raise", wasiENosys, "not implemented"),
		stub("sock_accept", wasiENotsup, "sockets not supported"),
		stub("sock_recv", wasiENotsup, "sockets not supported"),
		stub("sock_send", wasiENotsup, "sockets not supported"),
		stub("sock_shutdown", wasiENotsup, "sockets not supported"),
	}
}

// --- memory helpers (bounds-checked; malformed pointers yield EINVAL, never a
// Go panic that would abort the whole instance) ---

func le32(mem []byte, off uint32) (uint32, bool) {
	if int(off)+4 > len(mem) {
		return 0, false
	}
	return binary.LittleEndian.Uint32(mem[off:]), true
}

func putLe32(mem []byte, off, v uint32) bool {
	if int(off)+4 > len(mem) {
		return false
	}
	binary.LittleEndian.PutUint32(mem[off:], v)
	return true
}

func putLe64(mem []byte, off uint32, v uint64) bool {
	if int(off)+8 > len(mem) {
		return false
	}
	binary.LittleEndian.PutUint64(mem[off:], v)
	return true
}

// --- fd_* ---

func (e *Extension) fdWrite(m wago.HostModule, p, r []uint64) {
	fd, iovs, n, nwrittenPtr := int32(p[0]), uint32(p[1]), uint32(p[2]), uint32(p[3])
	var out io.Writer
	switch fd {
	case 1:
		out = e.cfg.Stdout
	case 2:
		out = e.cfg.Stderr
	default:
		r[0] = wasiEBadf
		return
	}
	mem := m.Memory()
	var total uint32
	for i := uint32(0); i < n; i++ {
		base, ok1 := le32(mem, iovs+i*8)
		length, ok2 := le32(mem, iovs+i*8+4)
		if !ok1 || !ok2 || int(base)+int(length) > len(mem) {
			r[0] = wasiEInval
			return
		}
		if out != nil {
			nn, err := out.Write(mem[base : base+length])
			total += uint32(nn)
			if err != nil {
				r[0] = wasiEInval
				return
			}
		} else {
			total += length
		}
	}
	if !putLe32(mem, nwrittenPtr, total) {
		r[0] = wasiEInval
		return
	}
	r[0] = wasiOK
}

func (e *Extension) fdRead(m wago.HostModule, p, r []uint64) {
	fd, iovs, n, nreadPtr := int32(p[0]), uint32(p[1]), uint32(p[2]), uint32(p[3])
	if fd != 0 || e.cfg.Stdin == nil {
		if fd == 0 { // stdin with no reader: clean EOF
			if putLe32(m.Memory(), nreadPtr, 0) {
				r[0] = wasiOK
				return
			}
		}
		r[0] = wasiEBadf
		return
	}
	mem := m.Memory()
	var total uint32
	for i := uint32(0); i < n; i++ {
		base, ok1 := le32(mem, iovs+i*8)
		length, ok2 := le32(mem, iovs+i*8+4)
		if !ok1 || !ok2 || int(base)+int(length) > len(mem) {
			r[0] = wasiEInval
			return
		}
		nn, err := e.cfg.Stdin.Read(mem[base : base+length])
		total += uint32(nn)
		if err != nil { // EOF or error: stop after this partial read
			break
		}
	}
	if !putLe32(mem, nreadPtr, total) {
		r[0] = wasiEInval
		return
	}
	r[0] = wasiOK
}

func (e *Extension) fdClose(_ wago.HostModule, p, r []uint64) { r[0] = wasiOK }

func (e *Extension) fdSeek(_ wago.HostModule, p, r []uint64) { r[0] = wasiESpipe } // streams are not seekable

func (e *Extension) fdFdstatGet(m wago.HostModule, p, r []uint64) {
	fd, buf := int32(p[0]), uint32(p[1])
	if fd < 0 || fd > 2 {
		r[0] = wasiEBadf
		return
	}
	mem := m.Memory()
	if int(buf)+24 > len(mem) {
		r[0] = wasiEInval
		return
	}
	for i := uint32(0); i < 24; i++ {
		mem[buf+i] = 0
	}
	mem[buf] = 2 // fs_filetype = CHARACTER_DEVICE
	r[0] = wasiOK
}

func (e *Extension) fdPrestatGet(_ wago.HostModule, p, r []uint64) { r[0] = wasiEBadf } // no preopened dirs

func (e *Extension) fdPrestatDirName(_ wago.HostModule, p, r []uint64) { r[0] = wasiEBadf }

// --- process / args / env ---

func (e *Extension) procExit(_ wago.HostModule, p, r []uint64) {
	panic(wago.HostExit{Code: int32(uint32(p[0]))})
}

func (e *Extension) argsSizesGet(m wago.HostModule, p, r []uint64) {
	r[0] = writeCounts(m.Memory(), uint32(p[0]), uint32(p[1]), e.cfg.Args)
}

func (e *Extension) argsGet(m wago.HostModule, p, r []uint64) {
	r[0] = writeStrings(m.Memory(), uint32(p[0]), uint32(p[1]), e.cfg.Args)
}

func (e *Extension) environSizesGet(m wago.HostModule, p, r []uint64) {
	r[0] = writeCounts(m.Memory(), uint32(p[0]), uint32(p[1]), e.cfg.Env)
}

func (e *Extension) environGet(m wago.HostModule, p, r []uint64) {
	r[0] = writeStrings(m.Memory(), uint32(p[0]), uint32(p[1]), e.cfg.Env)
}

// writeCounts writes the item count and the total NUL-terminated byte size.
func writeCounts(mem []byte, countPtr, sizePtr uint32, items []string) uint64 {
	total := 0
	for _, s := range items {
		total += len(s) + 1
	}
	if !putLe32(mem, countPtr, uint32(len(items))) || !putLe32(mem, sizePtr, uint32(total)) {
		return wasiEInval
	}
	return wasiOK
}

// writeStrings writes the pointer array then the packed NUL-terminated strings.
func writeStrings(mem []byte, ptrArray, buf uint32, items []string) uint64 {
	cur := buf
	for i, s := range items {
		if !putLe32(mem, ptrArray+uint32(i)*4, cur) {
			return wasiEInval
		}
		if int(cur)+len(s)+1 > len(mem) {
			return wasiEInval
		}
		copy(mem[cur:], s)
		mem[cur+uint32(len(s))] = 0
		cur += uint32(len(s)) + 1
	}
	return wasiOK
}

// --- clock / random ---

func (e *Extension) clockTimeGet(m wago.HostModule, p, r []uint64) {
	var now int64
	if e.cfg.Now != nil {
		now = e.cfg.Now()
	}
	if !putLe64(m.Memory(), uint32(p[2]), uint64(now)) {
		r[0] = wasiEInval
		return
	}
	r[0] = wasiOK
}

// clockResGet writes a coarse clock resolution (1ns) and succeeds.
func (e *Extension) clockResGet(m wago.HostModule, p, r []uint64) {
	if !putLe64(m.Memory(), uint32(p[1]), 1) {
		r[0] = wasiEInval
		return
	}
	r[0] = wasiOK
}

func (e *Extension) randomGet(m wago.HostModule, p, r []uint64) {
	buf, n := uint32(p[0]), uint32(p[1])
	mem := m.Memory()
	if int(buf)+int(n) > len(mem) {
		r[0] = wasiEInval
		return
	}
	src := e.cfg.Rand
	if src == nil {
		src = rand.Reader
	}
	if _, err := io.ReadFull(src, mem[buf:buf+n]); err != nil {
		r[0] = wasiEInval
		return
	}
	r[0] = wasiOK
}
