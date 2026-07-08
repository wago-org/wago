package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"github.com/wago-org/wago/src/core/runtime"
)

// SnapshotKind selects what state a Snapshot captures.
type SnapshotKind uint8

const (
	// SnapshotInit captures the module immediately after instantiation: initialized
	// memory, globals, and table, with the start function (if any) already run. No
	// additional warm function is executed.
	SnapshotInit SnapshotKind = iota
	// SnapshotWarm additionally runs a warm-up function before capturing, so the
	// snapshot reflects post-warm state (e.g. a runtime that lazily builds tables on
	// first entry). See SnapshotOptions.WarmFunc for resolution.
	SnapshotWarm
)

// SnapshotOptions configures Snapshot.
type SnapshotOptions struct {
	// Kind selects init vs warm capture. The zero value is SnapshotInit.
	Kind SnapshotKind

	// Imports available while creating the snapshot. They are retained on the
	// snapshot and re-applied to every instance restored from it (including pooled
	// instances), so restore takes no imports of its own.
	Imports Imports

	// GC configuration used while creating the snapshot and reused on restore.
	GC GCConfig

	// WarmFunc names the export to run for SnapshotWarm. If empty, Snapshot tries
	// "_start" then "_instantiate" and uses the first that is exported. Ignored for
	// SnapshotInit.
	WarmFunc string

	// WarmArgs are passed to WarmFunc (raw uint64 slots, per the export's signature).
	WarmArgs []uint64
}

// defaultWarmFuncs is the resolution order when SnapshotOptions.WarmFunc is empty.
var defaultWarmFuncs = []string{"_start", "_instantiate"}

// Snapshot is a captured runtime state of a module — linear memory, global
// values, and passive-data drop state — from which fresh instances can be created
// in that exact state without re-running data-segment init or the start function.
// It also carries the imports and GC config used at capture, so restored
// instances need none of their own.
//
// The default representation lives in local memory (Instance-independent heap
// copies). MarshalBinary/WriteFile convert it to a self-contained blob (embedding
// the compiled module) for storage on disk; LoadSnapshot/ReadSnapshotFile bring
// one back. A Snapshot is safe for concurrent use by Instantiate and Pool: it is
// read-only after creation.
//
// Scope of this prototype: linear memory (current, possibly grown size), all
// module-local globals, and passive-data descriptor lengths are captured. Imported
// globals are not — their state is the host's. Table contents are reconstructed
// from the module's element segments at restore, so runtime table.set mutations
// are not preserved. Only
// explicit-bounds modules are supported; signals-based (guard-page) instances are
// rejected, matching Compiled.MarshalBinary.
type Snapshot struct {
	// c is the module the snapshot restores against, kept for the in-memory path.
	// After a disk round-trip LoadSnapshot rebuilds it from the embedded blob.
	c *Compiled

	imports Imports  // re-applied to every restored instance
	gc      GCConfig // reused on restore
	kind    SnapshotKind

	memPages        uint32       // linear-memory size at capture, in 64 KiB wasm pages
	memory          []byte       // full linear-memory image (length == memPages*65536)
	globals         []globalSnap // one entry per global cell, indexed by wasm global index
	passiveDataLens []uint32     // current descriptor lengths, indexed by wasm data segment
}

type globalSnap struct {
	typ  ValType
	bits uint64
	vec  V128
}

// Snapshot instantiates c under opts, optionally runs a warm function, and
// captures the resulting memory and globals into a reusable Snapshot. The
// temporary capture instance is closed before returning; the snapshot is
// independent of it.
func Capture(c *Compiled, opts SnapshotOptions) (*Snapshot, error) {
	if c == nil {
		return nil, errors.New("wago: Snapshot: nil compiled module")
	}
	if c.boundsMode == BoundsChecksSignalsBased {
		return nil, errors.New("wago: signals-based (guard-page) modules cannot be snapshotted yet")
	}
	in, err := instantiateCore(c, InstantiateOptions{Imports: opts.Imports, GC: opts.GC})
	if err != nil {
		return nil, err
	}
	defer in.Close()
	if !in.ownsMem {
		return nil, errors.New("wago: cannot snapshot a module whose memory is host-imported or shared")
	}

	if opts.Kind == SnapshotWarm {
		if err := runWarm(in, opts); err != nil {
			return nil, err
		}
	}

	// linkModule may return a specialized *Compiled (import-bound); capture against
	// the instance's actual module so restore recompiles identically.
	s := &Snapshot{
		c:       in.c,
		imports: opts.Imports,
		gc:      opts.GC,
		kind:    opts.Kind,
	}
	live := in.memory.Bytes()
	s.memory = make([]byte, len(live))
	copy(s.memory, live)
	s.memPages = uint32(len(live) / 65536)
	s.globals = make([]globalSnap, len(in.globalCells))
	for i, g := range in.globalCells {
		if g == nil {
			continue
		}
		s.globals[i] = globalSnap{typ: g.Type, bits: readGlobalObject(g, g.Type), vec: readGlobalObjectV128(g)}
	}
	s.passiveDataLens = capturePassiveDataLens(in)
	return s, nil
}

// runWarm resolves and invokes the warm-up function for a SnapshotWarm capture.
func runWarm(in *Instance, opts SnapshotOptions) error {
	name := opts.WarmFunc
	if name == "" {
		for _, cand := range defaultWarmFuncs {
			if _, ok := in.c.Exports[cand]; ok {
				name = cand
				break
			}
		}
		if name == "" {
			return fmt.Errorf("wago: warm snapshot: no WarmFunc set and none of %v are exported", defaultWarmFuncs)
		}
	}
	if _, err := in.Invoke(name, opts.WarmArgs...); err != nil {
		return fmt.Errorf("wago: warm function %q: %w", name, err)
	}
	return nil
}

func capturePassiveDataLens(in *Instance) []uint32 {
	if in == nil || len(in.passiveDataDesc) == 0 || len(in.c.PassiveData) == 0 {
		return nil
	}
	lens := make([]uint32, len(in.c.PassiveData))
	for i := range lens {
		off := i*runtime.PassiveDataDescBytes + 8
		lens[i] = binary.LittleEndian.Uint32(in.passiveDataDesc[off:])
	}
	return lens
}

func snapshotPassiveDataLens(s *Snapshot) []uint32 {
	if s == nil || s.c == nil || len(s.c.PassiveData) == 0 {
		return nil
	}
	if len(s.passiveDataLens) == len(s.c.PassiveData) {
		return s.passiveDataLens
	}
	return compiledPassiveDataLens(s.c)
}

func compiledPassiveDataLens(c *Compiled) []uint32 {
	if c == nil || len(c.PassiveData) == 0 {
		return nil
	}
	lens := make([]uint32, len(c.PassiveData))
	for i, d := range c.PassiveData {
		lens[i] = uint32(len(d.Bytes))
	}
	return lens
}

func validatePassiveDataLens(c *Compiled, lens []uint32) error {
	if c == nil || len(c.PassiveData) == 0 {
		if len(lens) != 0 {
			return fmt.Errorf("snapshot has %d passive data lengths for module with none", len(lens))
		}
		return nil
	}
	if len(lens) != len(c.PassiveData) {
		return fmt.Errorf("length count %d does not match module passive data count %d", len(lens), len(c.PassiveData))
	}
	for i, n := range lens {
		full := uint32(len(c.PassiveData[i].Bytes))
		if n != 0 && n != full {
			return fmt.Errorf("segment %d length %d is neither dropped nor full length %d", i, n, full)
		}
	}
	return nil
}

// Module returns the compiled module this snapshot restores against.
func (s *Snapshot) Module() *Compiled { return s.c }

// Kind reports whether this is an init or warm snapshot.
func (s *Snapshot) Kind() SnapshotKind { return s.kind }

const snapshotMagic = "WGSN"
const snapshotVersion = 2

// MarshalBinary encodes the snapshot to a self-contained blob: the compiled
// module followed by the captured memory and globals. Trailing zero bytes of
// linear memory are trimmed (restore re-zeroes the tail), so a snapshot of a
// program that only touched the low end of a large heap stays small.
func (s *Snapshot) MarshalBinary() ([]byte, error) {
	if s == nil || s.c == nil {
		return nil, errors.New("wago: cannot marshal a snapshot with no bound module")
	}
	cb, err := s.c.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("wago: snapshot module: %w", err)
	}

	mem := s.memory
	n := len(mem)
	for n > 0 && mem[n-1] == 0 {
		n--
	}
	mem = mem[:n]

	passiveDataLens := snapshotPassiveDataLens(s)
	out := make([]byte, 0, len(snapshotMagic)+2+len(cb)+len(mem)+len(s.globals)*17+len(passiveDataLens)*5+40)
	out = append(out, snapshotMagic...)
	out = append(out, snapshotVersion, byte(s.kind))
	out = binary.AppendUvarint(out, uint64(len(cb)))
	out = append(out, cb...)
	out = binary.AppendUvarint(out, uint64(s.memPages))
	out = binary.AppendUvarint(out, uint64(len(mem)))
	out = append(out, mem...)
	out = binary.AppendUvarint(out, uint64(len(s.globals)))
	for _, g := range s.globals {
		out = append(out, byte(g.typ))
		if g.typ == ValV128 {
			out = append(out, g.vec[:]...)
		} else {
			var b [16]byte
			binary.LittleEndian.PutUint64(b[:8], g.bits)
			out = append(out, b[:]...)
		}
	}
	out = binary.AppendUvarint(out, uint64(len(passiveDataLens)))
	for _, n := range passiveDataLens {
		out = binary.AppendUvarint(out, uint64(n))
	}
	return out, nil
}

// IsSnapshot reports whether b is a wago snapshot blob (vs a compiled module or
// raw wasm).
func IsSnapshot(b []byte) bool {
	return len(b) >= len(snapshotMagic)+2 && string(b[:len(snapshotMagic)]) == snapshotMagic
}

// LoadSnapshot decodes a blob produced by MarshalBinary, rebuilding the embedded
// compiled module so the snapshot is ready to Instantiate or Pool. Restored
// instances take no imports (the snapshot's disk form cannot carry host function
// closures); attach them by re-creating the snapshot in-process if needed.
func LoadSnapshot(b []byte) (*Snapshot, error) {
	if !IsSnapshot(b) {
		return nil, errors.New("wago: not a snapshot blob")
	}
	p := b[len(snapshotMagic):]
	version := p[0]
	if version != 1 && version != snapshotVersion {
		return nil, fmt.Errorf("wago: snapshot version %d unsupported (want 1 or %d)", version, snapshotVersion)
	}
	kind := SnapshotKind(p[1])
	rd := &snapReader{buf: p[2:]}

	cb := rd.bytes(int(rd.uvarint()))
	memPages := rd.uvarint()
	memStored := rd.bytes(int(rd.uvarint()))
	globals := make([]globalSnap, rd.uvarint())
	for i := range globals {
		t := ValType(rd.byte())
		raw := rd.bytes(16)
		g := globalSnap{typ: t}
		if t == ValV128 {
			copy(g.vec[:], raw)
		} else if len(raw) == 16 {
			g.bits = binary.LittleEndian.Uint64(raw)
		}
		globals[i] = g
	}
	var passiveDataLens []uint32
	if version >= 2 {
		passiveDataLens = make([]uint32, rd.uvarint())
		for i := range passiveDataLens {
			v := rd.uvarint()
			if v > uint64(^uint32(0)) {
				rd.err = fmt.Errorf("passive data segment %d length %d overflows u32", i, v)
				break
			}
			passiveDataLens[i] = uint32(v)
		}
	}
	if rd.err != nil {
		return nil, fmt.Errorf("wago: invalid snapshot: %w", rd.err)
	}

	c, err := Load(cb)
	if err != nil {
		return nil, fmt.Errorf("wago: snapshot module: %w", err)
	}
	if version == 1 {
		passiveDataLens = compiledPassiveDataLens(c)
	} else if err := validatePassiveDataLens(c, passiveDataLens); err != nil {
		return nil, fmt.Errorf("wago: snapshot passive data: %w", err)
	}
	mem := make([]byte, int(memPages)*65536)
	copy(mem, memStored)
	return &Snapshot{c: c, kind: kind, memPages: uint32(memPages), memory: mem, globals: globals, passiveDataLens: passiveDataLens}, nil
}

// WriteFile marshals the snapshot and writes it to path.
func (s *Snapshot) WriteFile(path string) error {
	b, err := s.MarshalBinary()
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ReadSnapshotFile reads and decodes a snapshot blob from path.
func ReadSnapshotFile(path string) (*Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadSnapshot(b)
}

// snapReader is a tiny fail-slow byte cursor: once a read runs past the end it
// latches an error and later reads return zero, so callers check err once.
type snapReader struct {
	buf []byte
	err error
}

func (r *snapReader) uvarint() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.buf)
	if n <= 0 {
		r.err = errors.New("bad uvarint")
		return 0
	}
	r.buf = r.buf[n:]
	return v
}

func (r *snapReader) bytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.buf) {
		r.err = fmt.Errorf("want %d bytes, have %d", n, len(r.buf))
		return nil
	}
	b := r.buf[:n]
	r.buf = r.buf[n:]
	return b
}

func (r *snapReader) byte() byte {
	b := r.bytes(1)
	if b == nil {
		return 0
	}
	return b[0]
}
