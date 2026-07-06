package wago

import (
	"crypto/rand"
	"encoding/binary"
	"io"
)

// WASI preview 1 errno values (subset used here).
const (
	wasiOK      = 0
	wasiEBadf   = 8
	wasiEInval  = 28
	wasiESpipe  = 29
	wasiENosys  = 52
	wasiENotsup = 58
)

// errStub is a host function that ignores its args and returns a fixed errno —
// used for the parts of preview1 wago does not implement, so that a wasip1 binary
// (which links imports for the whole surface) still instantiates and gets a clean
// error rather than a missing-import failure.
func errStub(errno uint64) HostFunc {
	return func(_ HostModule, _, r []uint64) {
		if len(r) > 0 {
			r[0] = errno
		}
	}
}

// WASIConfig configures the minimal wasi_snapshot_preview1 host bundle returned
// by WASI. A nil writer/reader discards/EOFs; a nil Now yields a fixed clock
// (handy for deterministic tests); a nil Rand uses crypto/rand.
type WASIConfig struct {
	Stdout, Stderr io.Writer
	Stdin          io.Reader
	Args           []string     // argv; Args[0] is conventionally the program name
	Env            []string     // "KEY=VALUE" entries
	Now            func() int64 // wall-clock nanoseconds for clock_time_get
	Rand           io.Reader    // random source for random_get
}

// WASI returns an Imports bundle implementing a minimal wasi_snapshot_preview1:
// enough for programs that read/write the standard streams, exit, and query
// args/env/clock/random. Host functions access guest memory through the
// HostModule they receive. Bind it under the "wasi_snapshot_preview1" module:
//
//	in, err := wago.Instantiate(c, wago.WASI(wago.WASIConfig{Stdout: os.Stdout}))
//	_, err = in.Invoke("_start")
func WASI(cfg WASIConfig) Imports {
	w := &wasiHost{cfg: cfg}
	const p = "wasi_snapshot_preview1."
	return Imports{
		p + "fd_write":            HostFunc(w.fdWrite),
		p + "fd_read":             HostFunc(w.fdRead),
		p + "fd_close":            HostFunc(w.fdClose),
		p + "fd_seek":             HostFunc(w.fdSeek),
		p + "fd_fdstat_get":       HostFunc(w.fdFdstatGet),
		p + "fd_prestat_get":      HostFunc(w.fdPrestatGet),
		p + "fd_prestat_dir_name": HostFunc(w.fdPrestatDirName),
		p + "proc_exit":           HostFunc(w.procExit),
		p + "args_sizes_get":      HostFunc(w.argsSizesGet),
		p + "args_get":            HostFunc(w.argsGet),
		p + "environ_sizes_get":   HostFunc(w.environSizesGet),
		p + "environ_get":         HostFunc(w.environGet),
		p + "clock_time_get":      HostFunc(w.clockTimeGet),
		p + "clock_res_get":       HostFunc(w.clockResGet),
		p + "random_get":          HostFunc(w.randomGet),

		// Benign no-ops (hints / flushes / cooperative yield): success.
		p + "sched_yield":         errStub(wasiOK),
		p + "fd_advise":           errStub(wasiOK),
		p + "fd_datasync":         errStub(wasiOK),
		p + "fd_sync":             errStub(wasiOK),
		p + "fd_fdstat_set_flags": errStub(wasiOK),

		// Not implemented yet (filesystem, sockets, polling, timers): a clean
		// errno so guests fall back gracefully instead of failing to instantiate.
		p + "fd_allocate":             errStub(wasiENosys),
		p + "fd_fdstat_set_rights":    errStub(wasiENosys),
		p + "fd_filestat_get":         errStub(wasiENosys),
		p + "fd_filestat_set_size":    errStub(wasiENosys),
		p + "fd_filestat_set_times":   errStub(wasiENosys),
		p + "fd_pread":                errStub(wasiENosys),
		p + "fd_pwrite":               errStub(wasiENosys),
		p + "fd_readdir":              errStub(wasiENosys),
		p + "fd_renumber":             errStub(wasiENosys),
		p + "fd_tell":                 errStub(wasiESpipe),
		p + "path_create_directory":   errStub(wasiENosys),
		p + "path_filestat_get":       errStub(wasiENosys),
		p + "path_filestat_set_times": errStub(wasiENosys),
		p + "path_link":               errStub(wasiENosys),
		p + "path_open":               errStub(wasiEBadf),
		p + "path_readlink":           errStub(wasiENosys),
		p + "path_remove_directory":   errStub(wasiENosys),
		p + "path_rename":             errStub(wasiENosys),
		p + "path_symlink":            errStub(wasiENosys),
		p + "path_unlink_file":        errStub(wasiENosys),
		p + "poll_oneoff":             errStub(wasiENosys),
		p + "proc_raise":              errStub(wasiENosys),
		p + "sock_accept":             errStub(wasiENotsup),
		p + "sock_recv":               errStub(wasiENotsup),
		p + "sock_send":               errStub(wasiENotsup),
		p + "sock_shutdown":           errStub(wasiENotsup),
	}
}

// clockResGet writes a coarse clock resolution (1ns) and succeeds.
func (w *wasiHost) clockResGet(m HostModule, p, r []uint64) {
	if !putLe64(m.Memory(), uint32(p[1]), 1) {
		r[0] = wasiEInval
		return
	}
	r[0] = wasiOK
}

type wasiHost struct{ cfg WASIConfig }

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

func (w *wasiHost) fdWrite(m HostModule, p, r []uint64) {
	fd, iovs, n, nwrittenPtr := int32(p[0]), uint32(p[1]), uint32(p[2]), uint32(p[3])
	var out io.Writer
	switch fd {
	case 1:
		out = w.cfg.Stdout
	case 2:
		out = w.cfg.Stderr
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

func (w *wasiHost) fdRead(m HostModule, p, r []uint64) {
	fd, iovs, n, nreadPtr := int32(p[0]), uint32(p[1]), uint32(p[2]), uint32(p[3])
	if fd != 0 || w.cfg.Stdin == nil {
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
		nn, err := w.cfg.Stdin.Read(mem[base : base+length])
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

func (w *wasiHost) fdClose(_ HostModule, p, r []uint64) { r[0] = wasiOK }

func (w *wasiHost) fdSeek(_ HostModule, p, r []uint64) { r[0] = wasiESpipe } // streams are not seekable

func (w *wasiHost) fdFdstatGet(m HostModule, p, r []uint64) {
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

func (w *wasiHost) fdPrestatGet(_ HostModule, p, r []uint64) { r[0] = wasiEBadf } // no preopened dirs

func (w *wasiHost) fdPrestatDirName(_ HostModule, p, r []uint64) { r[0] = wasiEBadf }

// --- process / args / env ---

func (w *wasiHost) procExit(_ HostModule, p, r []uint64) {
	panic(HostExit{Code: int32(uint32(p[0]))})
}

func (w *wasiHost) argsSizesGet(m HostModule, p, r []uint64) {
	r[0] = writeCounts(m.Memory(), uint32(p[0]), uint32(p[1]), w.cfg.Args)
}

func (w *wasiHost) argsGet(m HostModule, p, r []uint64) {
	r[0] = writeStrings(m.Memory(), uint32(p[0]), uint32(p[1]), w.cfg.Args)
}

func (w *wasiHost) environSizesGet(m HostModule, p, r []uint64) {
	r[0] = writeCounts(m.Memory(), uint32(p[0]), uint32(p[1]), w.cfg.Env)
}

func (w *wasiHost) environGet(m HostModule, p, r []uint64) {
	r[0] = writeStrings(m.Memory(), uint32(p[0]), uint32(p[1]), w.cfg.Env)
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

func (w *wasiHost) clockTimeGet(m HostModule, p, r []uint64) {
	var now int64
	if w.cfg.Now != nil {
		now = w.cfg.Now()
	}
	if !putLe64(m.Memory(), uint32(p[2]), uint64(now)) {
		r[0] = wasiEInval
		return
	}
	r[0] = wasiOK
}

func (w *wasiHost) randomGet(m HostModule, p, r []uint64) {
	buf, n := uint32(p[0]), uint32(p[1])
	mem := m.Memory()
	if int(buf)+int(n) > len(mem) {
		r[0] = wasiEInval
		return
	}
	src := w.cfg.Rand
	if src == nil {
		src = rand.Reader
	}
	if _, err := io.ReadFull(src, mem[buf:buf+n]); err != nil {
		r[0] = wasiEInval
		return
	}
	r[0] = wasiOK
}
