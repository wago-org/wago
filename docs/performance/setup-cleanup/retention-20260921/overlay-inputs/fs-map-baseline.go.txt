package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	wago "github.com/wago-org/wago"
)

const maxInt64Value = uint64(^uint64(0) >> 1)

type fsState struct {
	mu     sync.Mutex
	fds    map[uint32]*fdEntry
	nextFD uint32
	maxFDs uint32
}

type fsGuard struct {
	mu       sync.Mutex
	resolver *wago.CallerResolver
	states   map[wago.InstanceIdentity]*fsState
	claimed  bool
	closed   bool
}

type fdEntry struct {
	file       *os.File
	reader     io.Reader
	writer     io.Writer
	mount      string
	root       string
	preopen    string
	flags      uint16
	rights     uint64
	inheriting uint64
	dirIter    *os.File
	dirCookie  uint64
	dirIssued  uint64
}

func (e *Plugin) resetFS() {
	e.guard = &fsGuard{states: make(map[wago.InstanceIdentity]*fsState)}
}

func (e *Plugin) initFS(strict bool) error {
	state, err := e.makeFS(strict)
	if err != nil {
		return err
	}
	e.guard.mu.Lock()
	e.fs = state
	e.guard.closed = false
	e.guard.claimed = false
	e.guard.mu.Unlock()
	return nil
}

func (e *Plugin) makeFS(strict bool) (*fsState, error) {
	maxFDs := e.cfg.MaxOpenFiles
	if maxFDs == 0 {
		maxFDs = 1024
	}
	if maxFDs < 3 {
		maxFDs = 3
	}
	s := &fsState{fds: make(map[uint32]*fdEntry), nextFD: 3, maxFDs: maxFDs}
	for fd := uint32(0); fd < 3; fd++ {
		rights := rightFDFilestatGet | rightPollFDReadWrite
		if fd == 0 {
			rights |= rightFDRead
		} else {
			rights |= rightFDWrite
		}
		entry := &fdEntry{rights: rights}
		switch fd {
		case 0:
			entry.reader = e.cfg.Stdin
		case 1:
			entry.writer = e.cfg.Stdout
		case 2:
			entry.writer = e.cfg.Stderr
		}
		s.fds[fd] = entry
	}
	type mountConfig struct {
		name, host        string
		rights, inherited uint64
	}
	mounts := make([]mountConfig, 0, len(e.cfg.Mounts))
	for _, mount := range e.cfg.Mounts {
		var rights, inherited uint64
		if mount.Read {
			rights |= directoryReadRights
			inherited |= directoryReadRights | fileReadRights
		}
		if mount.Write {
			rights |= rightPathOpen | rightPathFilestatSetSize | rightPathFilestatSetTimes | rightFDFilestatSetTimes
			inherited |= fileWriteRights | rightPathOpen | rightPathFilestatSetSize | rightPathFilestatSetTimes
		}
		if mount.MutateDirectory {
			rights |= directoryMutationRights
			inherited |= directoryMutationRights
		}
		mounts = append(mounts, mountConfig{name: mount.GuestPath, host: mount.HostPath, rights: rights, inherited: inherited})
	}
	sort.Slice(mounts, func(i, j int) bool { return mounts[i].name < mounts[j].name })
	for _, mount := range mounts {
		name := mount.name
		if uint32(len(s.fds)) >= maxFDs {
			if strict {
				closeFS(s)
				return nil, fmt.Errorf("wasi: preopens exceed maxOpenFiles %d", maxFDs)
			}
			break
		}
		host, err := filepath.Abs(mount.host)
		if err != nil {
			if strict {
				closeFS(s)
				return nil, fmt.Errorf("wasi: resolve preopen %q: %w", name, err)
			}
			continue
		}
		f, err := openPreopen(host, mount.rights&(rightFDFilestatSetTimes|rightPathFilestatSetTimes) != 0)
		if err != nil {
			if strict {
				closeFS(s)
				return nil, fmt.Errorf("wasi: open preopen %q (%s): %w", name, host, err)
			}
			continue
		}
		info, err := f.Stat()
		if err != nil || !info.IsDir() {
			_ = f.Close()
			if strict {
				closeFS(s)
				if err != nil {
					return nil, fmt.Errorf("wasi: stat preopen %q (%s): %w", name, host, err)
				}
				return nil, fmt.Errorf("wasi: preopen %q (%s) is not a directory", name, host)
			}
			continue
		}
		root, err := hostMountRoot(f, host)
		if err != nil {
			_ = f.Close()
			if strict {
				closeFS(s)
				return nil, fmt.Errorf("wasi: resolve preopen root %q (%s): %w", name, host, err)
			}
			continue
		}
		fd := s.nextFD
		s.nextFD++
		s.fds[fd] = &fdEntry{file: f, mount: host, root: root, preopen: name, rights: mount.rights, inheriting: mount.inherited}
	}
	return s, nil
}

func (e *Plugin) stateFor(m wago.HostModule) (*fsState, uint64) {
	e.guard.mu.Lock()
	if e.guard.closed || e.fs == nil {
		e.guard.mu.Unlock()
		return nil, wasiEBadf
	}
	state := e.fs
	if e.guard.resolver != nil {
		identity, err := e.guard.resolver.Resolve(m)
		if err != nil {
			e.guard.mu.Unlock()
			return nil, wasiEPerm
		}
		state = e.guard.states[identity]
		if state == nil {
			if !e.guard.claimed {
				state = e.fs
				e.guard.claimed = true
			} else {
				state, err = e.makeFS(true)
				if err != nil {
					e.guard.mu.Unlock()
					return nil, wasiEIo
				}
			}
			e.guard.states[identity] = state
		}
	}
	state.mu.Lock()
	e.guard.mu.Unlock()
	return state, wasiOK
}

func setStateError(results []uint64, errno uint64) {
	if len(results) != 0 {
		results[0] = errno
	}
}

func closeFS(state *fsState) {
	if state == nil {
		return
	}
	for _, entry := range state.fds {
		if entry.dirIter != nil {
			_ = entry.dirIter.Close()
		}
		if entry.file != nil {
			_ = entry.file.Close()
		}
	}
	clear(state.fds)
}

func (e *Plugin) closeInstance(identity wago.InstanceIdentity) {
	e.guard.mu.Lock()
	state := e.guard.states[identity]
	delete(e.guard.states, identity)
	e.guard.mu.Unlock()
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	closeFS(state)
}

func (e *Plugin) closeAll() {
	if e.guard == nil {
		return
	}
	e.guard.mu.Lock()
	unique := make(map[*fsState]struct{}, len(e.guard.states)+1)
	if e.fs != nil {
		unique[e.fs] = struct{}{}
	}
	for _, state := range e.guard.states {
		unique[state] = struct{}{}
	}
	clear(e.guard.states)
	e.fs = nil
	e.guard.closed = true
	e.guard.mu.Unlock()
	for state := range unique {
		state.mu.Lock()
		closeFS(state)
		state.mu.Unlock()
	}
}

func (e *Plugin) entry(fd uint32) (*fdEntry, uint64) {
	f := e.fs.fds[fd]
	if f == nil {
		return nil, wasiEBadf
	}
	return f, wasiOK
}

func require(f *fdEntry, right uint64) uint64 {
	if f.rights&right != right {
		return wasiENotcapable
	}
	return wasiOK
}

func guestBytes(mem []byte, ptr, n uint32) (string, uint64) {
	if uint64(ptr)+uint64(n) > uint64(len(mem)) {
		return "", wasiEFault
	}
	b := mem[ptr : ptr+n]
	if strings.IndexByte(string(b), 0) >= 0 {
		return "", wasiEInval
	}
	return string(b), wasiOK
}

// resolve validates a guest path and returns its directory capability. Kernel
// operations below additionally enforce RESOLVE_BENEATH so validation and use
// cannot be separated by a symlink race.
func (e *Plugin) resolve(fd uint32, guest string) (*fdEntry, string, uint64) {
	d, code := e.entry(fd)
	if code != 0 {
		return nil, "", code
	}
	if d.file == nil {
		return nil, "", wasiENotdir
	}
	st, err := d.file.Stat()
	if err != nil {
		return nil, "", errno(err)
	}
	if !st.IsDir() {
		return nil, "", wasiENotdir
	}
	if guest == "" || strings.HasPrefix(guest, "/") {
		return nil, "", wasiENotcapable
	}
	if pathEscapes(guest) {
		return nil, "", wasiENotcapable
	}
	return d, path.Clean(guest), wasiOK
}

func pathEscapes(guest string) bool {
	depth := 0
	for start := 0; start < len(guest); {
		end := strings.IndexByte(guest[start:], '/')
		if end < 0 {
			end = len(guest)
		} else {
			end += start
		}
		switch guest[start:end] {
		case "", ".":
		case "..":
			depth--
		default:
			depth++
		}
		if depth < 0 {
			return true
		}
		if end == len(guest) {
			break
		}
		start = end + 1
	}
	return false
}

func capabilityErr(err error) uint64 {
	if errors.Is(err, hostErrno.EXDEV) {
		return wasiENotcapable
	}
	return errno(err)
}

func openParent(d *fdEntry, name string) (*os.File, string, uint64) {
	parent, leaf := path.Split(name)
	parent = strings.TrimSuffix(parent, "/")
	if parent == "" {
		parent = "."
	}
	f, code := openAt(d, parent, hostOpenReadOnly|hostOpenDirectory, 0)
	return f, leaf, code
}

func (e *Plugin) alloc(entry *fdEntry) (uint32, uint64) {
	if uint32(len(e.fs.fds)) >= e.fs.maxFDs {
		return 0, wasiEMfile
	}
	fd := e.fs.nextFD
	for e.fs.fds[fd] != nil {
		fd++
	}
	e.fs.nextFD = fd + 1
	e.fs.fds[fd] = entry
	return fd, wasiOK
}

func (e *Plugin) iovecs(mem []byte, ptr, count uint32) ([][]byte, uint64) {
	if count > e.cfg.MaxIOVecs {
		return nil, wasiENomem
	}
	if uint64(ptr)+uint64(count)*8 > uint64(len(mem)) {
		return nil, wasiEFault
	}
	bufs := make([][]byte, 0, count)
	for i := uint32(0); i < count; i++ {
		base := binary.LittleEndian.Uint32(mem[ptr+i*8:])
		n := binary.LittleEndian.Uint32(mem[ptr+i*8+4:])
		if uint64(base)+uint64(n) > uint64(len(mem)) {
			return nil, wasiEFault
		}
		bufs = append(bufs, mem[base:base+n])
	}
	return bufs, wasiOK
}

func writeFilestat(mem []byte, ptr uint32, info os.FileInfo) uint64 {
	if uint64(ptr)+64 > uint64(len(mem)) {
		return wasiEFault
	}
	b := mem[ptr : ptr+64]
	clear(b)
	dev, ino, nlink, atim, ctim := hostFileStat(info)
	binary.LittleEndian.PutUint64(b[0:], dev)
	binary.LittleEndian.PutUint64(b[8:], ino)
	b[16] = filetype(info)
	binary.LittleEndian.PutUint64(b[24:], nlink)
	binary.LittleEndian.PutUint64(b[32:], uint64(info.Size()))
	binary.LittleEndian.PutUint64(b[40:], uint64(atim))
	binary.LittleEndian.PutUint64(b[48:], uint64(info.ModTime().UnixNano()))
	binary.LittleEndian.PutUint64(b[56:], uint64(ctim))
	return wasiOK
}

func (e *Plugin) fdAdvise(_ wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	if code == 0 {
		code = require(f, rightFDAdvise)
	}
	if code == 0 && p[3] > 5 {
		code = wasiEInval
	}
	r[0] = code
}

func (e *Plugin) fdAllocate(_ wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	if code == 0 {
		code = require(f, rightFDAllocate)
	}
	if code == 0 && f.file == nil {
		code = wasiEBadf
	}
	end := p[1] + p[2]
	if code == 0 && (end < p[1] || end > uint64(^uint64(0)>>1)) {
		code = wasiEInval
	}
	if code == 0 {
		code = errno(allocateFile(f.file, int64(p[1]), int64(p[2])))
	}
	r[0] = code
}

func (e *Plugin) fdDatasync(_ wago.HostModule, p, r []uint64) {
	e.syncFD(uint32(p[0]), rightFDDataSync, r)
}
func (e *Plugin) fdSync(_ wago.HostModule, p, r []uint64) { e.syncFD(uint32(p[0]), rightFDSync, r) }
func (e *Plugin) syncFD(fd uint32, right uint64, r []uint64) {
	f, code := e.entry(fd)
	if code == 0 {
		code = require(f, right)
	}
	if code == 0 && f.file != nil {
		code = errno(f.file.Sync())
	}
	r[0] = code
}

func (e *Plugin) fdFdstatSetFlags(_ wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	flags := uint16(p[1])
	if code == 0 {
		code = require(f, rightFDStatSetFlags)
	}
	if code == 0 && p[1]&^uint64(0x1f) != 0 {
		code = wasiEInval
	}
	if code == 0 && flags&0x1a != 0 {
		code = wasiENotsup
	}
	if code == 0 {
		hostFile := f.file
		if hostFile == nil {
			hostFile, _ = f.reader.(*os.File)
		}
		if hostFile == nil {
			hostFile, _ = f.writer.(*os.File)
		}
		if hostFile != nil {
			code = errno(setHostFileFlags(f, hostFile, flags))
		} else if flags != 0 {
			code = wasiENotsup
		}
		if code == 0 {
			f.flags = flags
		}
	}
	r[0] = code
}

func (e *Plugin) fdFdstatSetRights(_ wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	base, inheriting := p[1], p[2]
	if code == 0 && (base&^f.rights != 0 || inheriting&^f.inheriting != 0) {
		code = wasiENotcapable
	}
	if code == 0 {
		f.rights, f.inheriting = base, inheriting
	}
	r[0] = code
}

func (e *Plugin) fdFilestatGet(m wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	if code == 0 {
		code = require(f, rightFDFilestatGet)
	}
	if code == 0 && f.file == nil {
		code = wasiEBadf
	}
	if code == 0 {
		st, err := f.file.Stat()
		if err != nil {
			code = errno(err)
		} else {
			code = writeFilestat(m.Memory(), uint32(p[1]), st)
		}
	}
	r[0] = code
}

func (e *Plugin) fdFilestatSetSize(_ wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	if code == 0 {
		code = require(f, rightFDFilestatSetSize)
	}
	if code == 0 && f.file == nil {
		code = wasiEBadf
	}
	if code == 0 && p[1] > maxInt64Value {
		code = wasiEOverflow
	}
	if code == 0 {
		code = errno(f.file.Truncate(int64(p[1])))
	}
	r[0] = code
}

func validFstFlags(flags uint64) bool {
	return flags&^uint64(15) == 0 && flags&3 != 3 && flags&12 != 12
}

func timesFor(info os.FileInfo, atim, mtim uint64, flags uint64, now time.Time) ([]time.Time, uint64) {
	if !validFstFlags(flags) {
		return nil, wasiEInval
	}
	if flags&1 != 0 && atim > maxInt64Value || flags&4 != 0 && mtim > maxInt64Value {
		return nil, wasiEOverflow
	}
	a, mt := hostAccessTime(info), info.ModTime()
	if flags&1 != 0 {
		a = time.Unix(0, int64(atim))
	}
	if flags&2 != 0 {
		a = now
	}
	if flags&4 != 0 {
		mt = time.Unix(0, int64(mtim))
	}
	if flags&8 != 0 {
		mt = now
	}
	return []time.Time{a, mt}, wasiOK
}

func (e *Plugin) filesystemNow() (time.Time, uint64) {
	now, _, err := e.cfg.Clocks.Realtime()
	if err != nil || now > maxInt64Value {
		return time.Time{}, wasiENotsup
	}
	return time.Unix(0, int64(now)), wasiOK
}

func (e *Plugin) fdFilestatSetTimes(_ wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	if code == 0 {
		code = require(f, rightFDFilestatSetTimes)
	}
	if code == 0 && f.file == nil {
		code = wasiEBadf
	}
	if code == 0 {
		st, err := f.file.Stat()
		if err != nil {
			code = errno(err)
		} else {
			var now time.Time
			if p[3]&0xa != 0 {
				now, code = e.filesystemNow()
			}
			var times []time.Time
			if code == 0 {
				times, code = timesFor(st, p[1], p[2], p[3], now)
			}
			if code == 0 {
				code = errno(setFileTimes(f.file, times))
			}
		}
	}
	r[0] = code
}

func (e *Plugin) fdPread(m wago.HostModule, p, r []uint64)  { e.readAt(m, p, r) }
func (e *Plugin) fdPwrite(m wago.HostModule, p, r []uint64) { e.writeAt(m, p, r) }

func (e *Plugin) readAt(m wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	if code == 0 {
		code = require(f, rightFDRead|rightFDSeek)
	}
	bufs, memCode := e.iovecs(m.Memory(), uint32(p[1]), uint32(p[2]))
	if code == 0 {
		code = memCode
	}
	var total uint32
	if code == 0 && p[3] > maxInt64Value {
		code = wasiEOverflow
	}
	off := int64(p[3])
	if code == 0 && f.file == nil {
		code = wasiEBadf
	}
	for _, b := range bufs {
		if code != 0 {
			break
		}
		n, err := f.file.ReadAt(b, off)
		total += uint32(n)
		off += int64(n)
		if err != nil && err != io.EOF {
			code = errno(err)
		}
		if n < len(b) {
			break
		}
	}
	if code == 0 && !putLe32(m.Memory(), uint32(p[4]), total) {
		code = wasiEFault
	}
	r[0] = code
}

func (e *Plugin) writeAt(m wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	if code == 0 {
		code = require(f, rightFDWrite|rightFDSeek)
	}
	bufs, memCode := e.iovecs(m.Memory(), uint32(p[1]), uint32(p[2]))
	if code == 0 {
		code = memCode
	}
	var total uint32
	if code == 0 && p[3] > maxInt64Value {
		code = wasiEOverflow
	}
	off := int64(p[3])
	if code == 0 && f.file == nil {
		code = wasiEBadf
	}
	for _, b := range bufs {
		if code != 0 {
			break
		}
		var n int
		var err error
		if f.flags&1 != 0 {
			n, err = appendWriteAt(f.file, b, off)
		} else {
			n, err = f.file.WriteAt(b, off)
		}
		total += uint32(n)
		off += int64(n)
		if err != nil {
			code = errno(err)
		}
	}
	if code == 0 && !putLe32(m.Memory(), uint32(p[4]), total) {
		code = wasiEFault
	}
	r[0] = code
}

func (e *Plugin) fdReaddir(m wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	if code == 0 {
		code = require(f, rightFDReadDir)
	}
	if code == 0 && f.file == nil {
		code = wasiEBadf
	}
	cookie := p[3]
	buf, bufLen := uint32(p[1]), uint32(p[2])
	mem := m.Memory()
	if code == 0 && uint64(buf)+uint64(bufLen) > uint64(len(mem)) {
		code = wasiEFault
	}
	var used uint32
	if code == 0 {
		code = positionDirectory(f, cookie)
	}
	for i := cookie; code == 0; i++ {
		name := "."
		var info os.FileInfo
		var err error
		if i == 0 {
			info, err = f.file.Stat()
		} else if i == 1 {
			name = ".."
			info, err = f.file.Stat()
		} else {
			entries, readErr := f.dirIter.ReadDir(1)
			if errors.Is(readErr, io.EOF) || len(entries) == 0 {
				break
			}
			if readErr != nil {
				code = errno(readErr)
				break
			}
			f.dirCookie = i + 1
			entry := entries[0]
			name = entry.Name()
			var entryCode uint64
			info, entryCode = directoryEntryInfo(f, entry)
			if entryCode != wasiOK {
				code = entryCode
				break
			}
		}
		if err != nil {
			code = errno(err)
			break
		}
		rec := make([]byte, 24+len(name))
		binary.LittleEndian.PutUint64(rec[0:], i+1)
		binary.LittleEndian.PutUint64(rec[8:], hostInode(info))
		binary.LittleEndian.PutUint32(rec[16:], uint32(len(name)))
		rec[20] = filetype(info)
		copy(rec[24:], name)
		remaining := int(bufLen - used)
		if remaining <= 0 {
			break
		}
		n := len(rec)
		if n > remaining {
			n = remaining
		}
		copy(mem[buf+used:], rec[:n])
		used += uint32(n)
		if n >= 24 && i+1 > f.dirIssued {
			f.dirIssued = i + 1
		}
		if n < len(rec) {
			break
		}
	}
	if code == 0 && !putLe32(mem, uint32(p[4]), used) {
		code = wasiEFault
	}
	r[0] = code
}

func positionDirectory(f *fdEntry, cookie uint64) uint64 {
	if cookie != 0 && cookie > f.dirIssued {
		return wasiENoent
	}
	if f.dirIter != nil && f.dirCookie == cookie {
		return wasiOK
	}
	if f.dirIter != nil {
		_ = f.dirIter.Close()
	}
	dir, code := openAt(f, ".", hostOpenReadOnly|hostOpenDirectory, 0)
	if code != 0 {
		return code
	}
	f.dirIter = dir
	f.dirCookie = 2
	var remaining uint64
	if cookie > 2 {
		remaining = cookie - 2
	}
	for remaining > 0 {
		step := remaining
		if step > 1024 {
			step = 1024
		}
		entries, err := dir.ReadDir(int(step))
		f.dirCookie += uint64(len(entries))
		remaining -= uint64(len(entries))
		if errors.Is(err, io.EOF) || len(entries) == 0 {
			return wasiOK
		}
		if err != nil {
			return errno(err)
		}
	}
	return wasiOK
}

func (e *Plugin) fdRenumber(_ wago.HostModule, p, r []uint64) {
	from, to := uint32(p[0]), uint32(p[1])
	f, code := e.entry(from)
	if code == 0 && to > uint32(^uint32(0)>>1) {
		code = wasiEBadf
	}
	if code == 0 && e.fs.fds[to] == nil {
		code = wasiEBadf
	}
	if code == 0 && from == to {
		r[0] = wasiOK
		return
	}
	if code == 0 {
		if old := e.fs.fds[to]; old != nil {
			if old.dirIter != nil {
				_ = old.dirIter.Close()
			}
			if old.file != nil {
				_ = old.file.Close()
			}
		}
		e.fs.fds[to] = f
		delete(e.fs.fds, from)
	}
	r[0] = code
}

func (e *Plugin) fdTell(m wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	if code == 0 {
		code = require(f, rightFDTell)
	}
	if code == 0 && f.file == nil {
		code = wasiESpipe
	}
	if code == 0 {
		off, err := f.file.Seek(0, io.SeekCurrent)
		if err != nil {
			code = errno(err)
		} else if !putLe64(m.Memory(), uint32(p[1]), uint64(off)) {
			code = wasiEFault
		}
	}
	r[0] = code
}

func (e *Plugin) pathCreateDirectory(m wago.HostModule, p, r []uint64) {
	e.pathUnary(m, p, r, rightPathCreateDirectory, func(parent *os.File, name string) error { return makeDirectoryAt(parent, name, 0o777) })
}

func (e *Plugin) pathUnary(m wago.HostModule, p, r []uint64, right uint64, op func(*os.File, string) error) {
	name, code := guestBytes(m.Memory(), uint32(p[1]), uint32(p[2]))
	d, name, pathCode := e.resolve(uint32(p[0]), name)
	if code == 0 {
		code = pathCode
	}
	if code == 0 {
		code = require(d, right)
	}
	if code == 0 {
		parent, leaf, parentCode := openParent(d, name)
		code = parentCode
		if code == 0 {
			code = errno(op(parent, leaf))
			_ = parent.Close()
		}
	}
	r[0] = code
}

func (e *Plugin) pathFilestatGet(m wago.HostModule, p, r []uint64) {
	name, code := guestBytes(m.Memory(), uint32(p[2]), uint32(p[3]))
	d, name, pathCode := e.resolve(uint32(p[0]), name)
	if code == 0 {
		code = pathCode
	}
	if code == 0 {
		code = require(d, rightPathFilestatGet)
	}
	if code == 0 && p[1]&^uint64(1) != 0 {
		code = wasiEInval
	}
	var st os.FileInfo
	if code == 0 {
		f, openCode := openMetadataAt(d, name, uint16(p[1])&1 != 0)
		code = openCode
		if code == 0 {
			var err error
			st, err = f.Stat()
			_ = f.Close()
			if err != nil {
				code = errno(err)
			}
		}
	}
	if code == 0 {
		code = writeFilestat(m.Memory(), uint32(p[4]), st)
	}
	r[0] = code
}

func (e *Plugin) pathFilestatSetTimes(m wago.HostModule, p, r []uint64) {
	name, code := guestBytes(m.Memory(), uint32(p[2]), uint32(p[3]))
	d, name, pathCode := e.resolve(uint32(p[0]), name)
	if code == 0 {
		code = pathCode
	}
	if code == 0 {
		code = require(d, rightPathFilestatSetTimes)
	}
	if code == 0 && p[1]&^uint64(1) != 0 {
		code = wasiEInval
	}
	if code == 0 {
		follow := uint16(p[1])&1 != 0
		if follow {
			f, openCode := openMetadataAt(d, name, true)
			code = openCode
			if code == 0 {
				st, err := f.Stat()
				if err != nil {
					code = errno(err)
				} else {
					var now time.Time
					if p[6]&0xa != 0 {
						now, code = e.filesystemNow()
					}
					var times []time.Time
					if code == 0 {
						times, code = timesFor(st, p[4], p[5], p[6], now)
					}
					if code == 0 {
						code = errno(setFileTimes(f, times))
					}
				}
				_ = f.Close()
			}
		} else {
			parent, leaf, parentCode := openParent(d, name)
			code = parentCode
			if code == 0 {
				f, openCode := openMetadataAt(d, name, false)
				code = openCode
				if code == 0 {
					st, err := f.Stat()
					_ = f.Close()
					if err != nil {
						code = errno(err)
					} else {
						var now time.Time
						if p[6]&0xa != 0 {
							now, code = e.filesystemNow()
						}
						var times []time.Time
						if code == 0 {
							times, code = timesFor(st, p[4], p[5], p[6], now)
						}
						if code == 0 {
							code = errno(setPathTimes(parent, leaf, times, true))
						}
					}
				}
				_ = parent.Close()
			}
		}
	}
	r[0] = code
}

func (e *Plugin) pathLink(m wago.HostModule, p, r []uint64) {
	oldName, code := guestBytes(m.Memory(), uint32(p[2]), uint32(p[3]))
	newName, code2 := guestBytes(m.Memory(), uint32(p[5]), uint32(p[6]))
	oldTrailing, newTrailing := strings.HasSuffix(oldName, "/"), strings.HasSuffix(newName, "/")
	od, oldName, c1 := e.resolve(uint32(p[0]), oldName)
	nd, newName, c2 := e.resolve(uint32(p[4]), newName)
	for _, c := range []uint64{code2, c1, c2} {
		if code == 0 {
			code = c
		}
	}
	if code == 0 {
		code = require(od, rightPathLinkSource)
	}
	if code == 0 {
		code = require(nd, rightPathLinkTarget)
	}
	oldFlags := uint16(p[1])
	if code == 0 && p[1]&^uint64(1) != 0 {
		code = wasiEInval
	}
	if code == 0 && oldTrailing {
		code = wasiENotdir
	}
	if code == 0 && newTrailing {
		code = wasiENoent
	}
	if code == 0 && od.mount != nd.mount {
		code = 75
	}
	if code == 0 {
		newParent, newLeaf, parentCode := openParent(nd, newName)
		code = parentCode
		if code == 0 {
			if oldFlags&1 != 0 {
				code = linkAtFollow(od, oldName, newParent, newLeaf)
			} else {
				oldParent, oldLeaf, oldCode := openParent(od, oldName)
				code = oldCode
				if code == 0 {
					code = errno(linkAt(oldParent, oldLeaf, newParent, newLeaf))
					_ = oldParent.Close()
				}
			}
			_ = newParent.Close()
		}
	}
	r[0] = code
}

func (e *Plugin) pathOpen(m wago.HostModule, p, r []uint64) {
	name, code := guestBytes(m.Memory(), uint32(p[2]), uint32(p[3]))
	trailingSlash := strings.HasSuffix(name, "/")
	d, name, pathCode := e.resolve(uint32(p[0]), name)
	if code == 0 {
		code = pathCode
	}
	if code == 0 {
		code = require(d, rightPathOpen)
	}
	oflags, rights, inheriting, fdflags := uint16(p[4]), p[5], p[6], uint16(p[7])
	if code == 0 && (p[1]&^uint64(1) != 0 || p[4]&^uint64(15) != 0 || p[7]&^uint64(31) != 0) {
		code = wasiEInval
	}
	if code == 0 && (rights&^d.inheriting != 0 || inheriting&^d.inheriting != 0) {
		code = wasiENotcapable
	}
	if code == 0 && oflags&1 != 0 {
		code = require(d, rightPathCreateFile)
	}
	if code == 0 && oflags&8 != 0 {
		code = require(d, rightPathFilestatSetSize)
	}
	flags := 0
	read, write := rights&rightFDRead != 0, rights&rightFDWrite != 0
	if read && write {
		flags = os.O_RDWR
	} else if write {
		flags = os.O_WRONLY
	} else {
		flags = os.O_RDONLY
	}
	if oflags&1 != 0 {
		flags |= os.O_CREATE
	}
	if oflags&4 != 0 {
		flags |= os.O_EXCL
	}
	if oflags&8 != 0 {
		flags |= os.O_TRUNC
	}
	if fdflags&1 != 0 {
		flags |= os.O_APPEND
	}
	if rights&(rightFDFilestatSetTimes|rightPathFilestatSetTimes) != 0 {
		flags |= hostOpenWriteAttributes
	}
	if oflags&2 != 0 || trailingSlash {
		flags |= hostOpenDirectory
	}
	if uint16(p[1])&1 == 0 {
		flags |= hostOpenNoFollow
	}
	var f *os.File
	if code == 0 {
		f, code = openAt(d, name, flags, 0o666)
	}
	if code == 0 {
		st, err := f.Stat()
		if err != nil {
			code = errno(err)
		}
		if code == 0 && oflags&2 != 0 && !st.IsDir() {
			code = wasiENotdir
		}
		if code == 0 && st.IsDir() && write {
			code = wasiEIsdir
		}
		if code == 0 && st.IsDir() {
			rights &= directoryRights
		}
	}
	if code != 0 {
		if f != nil {
			_ = f.Close()
		}
	} else {
		entry := &fdEntry{file: f, mount: d.mount, root: d.root, flags: fdflags, rights: rights, inheriting: inheriting}
		fd, allocCode := e.alloc(entry)
		if allocCode != 0 {
			_ = f.Close()
			code = allocCode
		} else if !putLe32(m.Memory(), uint32(p[8]), fd) {
			_ = f.Close()
			delete(e.fs.fds, fd)
			code = wasiEFault
		}
	}
	r[0] = code
}

func (e *Plugin) pathReadlink(m wago.HostModule, p, r []uint64) {
	name, code := guestBytes(m.Memory(), uint32(p[1]), uint32(p[2]))
	d, name, pathCode := e.resolve(uint32(p[0]), name)
	if code == 0 {
		code = pathCode
	}
	if code == 0 {
		code = require(d, rightPathReadlink)
	}
	var target string
	if code == 0 {
		parent, leaf, parentCode := openParent(d, name)
		code = parentCode
		if code == 0 {
			buf := make([]byte, 4096)
			n, err := readlinkAt(parent, leaf, buf)
			_ = parent.Close()
			if err != nil {
				code = errno(err)
			} else if n == len(buf) {
				code = wasiENametoolong
			} else {
				target = string(buf[:n])
			}
		}
	}
	buf, n := uint32(p[3]), uint32(p[4])
	if code == 0 && uint64(buf)+uint64(n) > uint64(len(m.Memory())) {
		code = wasiEFault
	}
	if code == 0 {
		used := len(target)
		if used > int(n) {
			used = int(n)
		}
		copy(m.Memory()[buf:buf+uint32(used)], target[:used])
		if !putLe32(m.Memory(), uint32(p[5]), uint32(used)) {
			code = wasiEFault
		}
	}
	r[0] = code
}

func (e *Plugin) pathRemoveDirectory(m wago.HostModule, p, r []uint64) {
	e.pathUnary(m, p, r, rightPathRemoveDirectory, func(parent *os.File, name string) error {
		return removeAt(parent, name, true)
	})
}

func (e *Plugin) pathUnlinkFile(m wago.HostModule, p, r []uint64) {
	name, code := guestBytes(m.Memory(), uint32(p[1]), uint32(p[2]))
	if code == 0 && strings.HasSuffix(name, "/") {
		d, clean, pathCode := e.resolve(uint32(p[0]), name)
		if pathCode != 0 {
			r[0] = pathCode
			return
		}
		if rightsCode := require(d, rightPathUnlinkFile); rightsCode != 0 {
			r[0] = rightsCode
			return
		}
		f, openCode := openMetadataAt(d, clean, false)
		if openCode != 0 {
			r[0] = openCode
			return
		}
		st, err := f.Stat()
		_ = f.Close()
		if err == nil && st.IsDir() {
			r[0] = wasiEIsdir
		} else {
			r[0] = wasiENotdir
		}
		return
	}
	e.pathUnary(m, p, r, rightPathUnlinkFile, func(parent *os.File, name string) error {
		return removeAt(parent, name, false)
	})
}

func (e *Plugin) pathRename(m wago.HostModule, p, r []uint64) {
	oldName, code := guestBytes(m.Memory(), uint32(p[1]), uint32(p[2]))
	newName, code2 := guestBytes(m.Memory(), uint32(p[4]), uint32(p[5]))
	od, oldName, c1 := e.resolve(uint32(p[0]), oldName)
	nd, newName, c2 := e.resolve(uint32(p[3]), newName)
	for _, c := range []uint64{code2, c1, c2} {
		if code == 0 {
			code = c
		}
	}
	if code == 0 {
		code = require(od, rightPathRenameSource)
	}
	if code == 0 {
		code = require(nd, rightPathRenameTarget)
	}
	if code == 0 && od.mount != nd.mount {
		code = 75
	}
	if code == 0 {
		oldParent, oldLeaf, oldCode := openParent(od, oldName)
		code = oldCode
		if code == 0 {
			newParent, newLeaf, newCode := openParent(nd, newName)
			code = newCode
			if code == 0 {
				code = errno(renameAt(oldParent, oldLeaf, newParent, newLeaf))
				_ = newParent.Close()
			}
			_ = oldParent.Close()
		}
	}
	r[0] = code
}

func (e *Plugin) pathSymlink(m wago.HostModule, p, r []uint64) {
	target, code := guestBytes(m.Memory(), uint32(p[0]), uint32(p[1]))
	name, code2 := guestBytes(m.Memory(), uint32(p[3]), uint32(p[4]))
	trailingSlash := strings.HasSuffix(name, "/")
	d, name, pathCode := e.resolve(uint32(p[2]), name)
	for _, c := range []uint64{code2, pathCode} {
		if code == 0 {
			code = c
		}
	}
	if code == 0 {
		code = require(d, rightPathSymlink)
	}
	if code == 0 && strings.HasPrefix(target, "/") {
		code = wasiENotcapable
	}
	if code == 0 && trailingSlash {
		code = wasiENoent
	}
	if code == 0 {
		parent, leaf, parentCode := openParent(d, name)
		code = parentCode
		if code == 0 {
			code = errno(symlinkAt(target, parent, leaf))
			_ = parent.Close()
		}
	}
	r[0] = code
}

func (e *Plugin) schedYield(_ wago.HostModule, _, r []uint64) { r[0] = wasiOK }
func (e *Plugin) procRaise(_ wago.HostModule, _, r []uint64)  { r[0] = wasiENotsup }

func (e *Plugin) sockAccept(_ wago.HostModule, _, r []uint64)   { r[0] = wasiENotsup }
func (e *Plugin) sockRecv(_ wago.HostModule, p, r []uint64)     { e.unsupportedSocket(p, r) }
func (e *Plugin) sockSend(_ wago.HostModule, p, r []uint64)     { e.unsupportedSocket(p, r) }
func (e *Plugin) sockShutdown(_ wago.HostModule, p, r []uint64) { e.unsupportedSocket(p, r) }
func (e *Plugin) unsupportedSocket(p, r []uint64) {
	if _, code := e.entry(uint32(p[0])); code != 0 {
		r[0] = code
	} else {
		r[0] = wasiENotsock
	}
}
