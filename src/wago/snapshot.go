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
	// memory and globals, with the start function (if any) already run. Table
	// modules are rejected until table snapshotting is implemented. No additional
	// warm function is executed.
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
	// snapshot and re-applied to every instance restored from it, so restore takes
	// no imports of its own.
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
// instances need none of their own. Modules with tables are rejected until table
// state is captured/restored too.
//
// The default representation lives in local memory (Instance-independent heap
// copies). MarshalBinary/WriteFile convert it to a self-contained blob (embedding
// the compiled module) for storage on disk; LoadSnapshot/ReadSnapshotFile bring
// one back. A Snapshot is safe for concurrent use by Instantiate: it is
// read-only after creation.
//
// Scope of this prototype: linear memory (current, possibly grown size), numeric
// and vector module-local globals, and passive-data descriptor lengths are
// captured. Reference globals are rejected until a live-state resolver exists.
// Imported globals are not — their state is the host's. Tables are not snapshotted yet;
// Capture rejects modules with local or imported tables instead of silently losing
// table.set/fill/grow/init/drop state. Only explicit-bounds modules are supported;
// signals-based (guard-page) instances are rejected, matching Compiled.MarshalBinary.
type Snapshot struct {
	// c is the module the snapshot restores against, kept for the in-memory path.
	// After a disk round-trip LoadSnapshot rebuilds it from the embedded blob.
	c *Compiled

	imports Imports  // re-applied to every restored instance
	gc      GCConfig // reused on restore
	kind    SnapshotKind

	memPages uint32 // legacy/single-memory cache; aliases memories[0] when present
	memory   []byte // legacy/single-memory cache; aliases memories[0] when present

	memories        []memorySnap // one entry per owned local memory, in wasm index order
	globals         []globalSnap // one entry per global cell, indexed by wasm global index
	passiveDataLens []uint32     // current descriptor lengths, indexed by wasm data segment
}

type memorySnap struct {
	pages uint32
	image []byte // captured bytes; a blob-loaded image may omit a zero-filled tail
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
	if err := validateSnapshotModule(c); err != nil {
		return nil, err
	}
	in, err := instantiateCore(c, InstantiateOptions{Imports: opts.Imports, GC: opts.GC})
	if err != nil {
		return nil, err
	}
	defer in.Close()
	if !in.ownsMem {
		return nil, errors.New("wago: cannot snapshot a module whose memory is host-imported or shared")
	}
	if in.memoryDir != nil {
		for i, memory := range in.memoryDir.memories {
			if memory == nil || i >= len(in.memoryDir.owns) || !in.memoryDir.owns[i] {
				return nil, fmt.Errorf("wago: cannot snapshot memory %d because it is imported, shared, or unavailable", i)
			}
		}
	}

	if opts.Kind == SnapshotWarm {
		if err := runWarm(in, opts); err != nil {
			return nil, err
		}
	}

	return captureInstanceSnapshot(in, opts), nil
}

// captureInstanceSnapshot copies the reusable state of an already-initialized
// instance. Callers must first enforce the Snapshot admission and owned-memory
// boundaries without exporting mutable snapshot internals.
func captureInstanceSnapshot(in *Instance, opts SnapshotOptions) *Snapshot {
	// Capture against the instance's actual immutable compiled image. Concrete
	// import targets remain per-instance dispatch state and are never serialized.
	s := &Snapshot{
		c:       in.c,
		imports: opts.Imports,
		gc:      opts.GC,
		kind:    opts.Kind,
	}
	if in.c.memoryCount() == 0 {
		// The runtime keeps one legacy scratch page even for memory-free modules;
		// it is not a declared Wasm memory and must not enter snapshot-v3 records.
		s.memories = nil
	} else if in.memoryDir == nil {
		live := in.memory.Bytes()
		s.memory = append([]byte(nil), live...)
		s.memPages = uint32(len(live) / 65536)
		s.memories = []memorySnap{{pages: s.memPages, image: s.memory}}
	} else {
		s.memories = make([]memorySnap, len(in.memoryDir.memories))
		for i, memory := range in.memoryDir.memories {
			live := memory.Bytes()
			s.memories[i] = memorySnap{pages: uint32(len(live) / 65536), image: append([]byte(nil), live...)}
		}
		if len(s.memories) != 0 {
			s.memPages, s.memory = s.memories[0].pages, s.memories[0].image
		}
	}
	s.globals = make([]globalSnap, len(in.globalCells))
	for i, g := range in.globalCells {
		if g == nil {
			continue
		}
		s.globals[i] = globalSnap{typ: g.Type, bits: readGlobalObject(g, g.Type), vec: readGlobalObjectV128(g)}
	}
	s.passiveDataLens = capturePassiveDataLens(in)
	return s
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

func snapshotMemories(s *Snapshot) []memorySnap {
	if s == nil {
		return nil
	}
	if len(s.memories) != 0 {
		return s.memories
	}
	if s.memPages != 0 || len(s.memory) != 0 || (s.c != nil && s.c.memoryCount() == 1) {
		return []memorySnap{{pages: s.memPages, image: s.memory}}
	}
	return nil
}

func validateSnapshotMemories(c *Compiled, memories []memorySnap) error {
	if c == nil {
		return errors.New("snapshot has no bound module")
	}
	if len(memories) != c.memoryCount() {
		return fmt.Errorf("memory count %d does not match module memory count %d", len(memories), c.memoryCount())
	}
	for i, memory := range memories {
		def := c.memoryDef(i)
		if uint64(memory.pages) < def.Min {
			return fmt.Errorf("memory %d page count %d is below declared minimum %d", i, memory.pages, def.Min)
		}
		if def.HasMax && uint64(memory.pages) > def.Max {
			return fmt.Errorf("memory %d page count %d exceeds declared maximum %d", i, memory.pages, def.Max)
		}
		bytes := uint64(memory.pages) * 65536
		if uint64(len(memory.image)) > bytes {
			return fmt.Errorf("memory %d image length %d exceeds page count %d", i, len(memory.image), memory.pages)
		}
	}
	return nil
}

func validateSnapshotModule(c *Compiled) error {
	if c == nil {
		return errors.New("wago: snapshot has no bound module")
	}
	if (c.requiredFeatures|compiledStructuralRequiredFeatures(c))&CoreFeatureExceptionHandling != 0 {
		return errors.New("wago: exception-handling tag identity and active native handlers cannot be snapshotted")
	}
	if (c.requiredFeatures|compiledStructuralRequiredFeatures(c))&(CoreFeatureTypedFunctionReferences|CoreFeatureTailCall) != 0 {
		return errors.New("wago: typed function references and tail-call contexts cannot be snapshotted until descriptor owners and tail frames have a persisted resolver")
	}
	if err := c.validateSnapshotReferenceGlobals(); err != nil {
		return err
	}
	if c.HasTable {
		return errors.New("wago: modules with tables cannot be snapshotted yet")
	}
	for i := 0; i < c.memoryCount(); i++ {
		def := c.memoryDef(i)
		if def.Addr64 {
			return errors.New("wago: memory64 modules cannot be snapshotted until 64-bit address-form lifecycle metadata is admitted")
		}
		if def.ImportKey != "" || def.Shared {
			if c.memoryCount() > 1 {
				return errors.New("wago: modules with multiple memories that are imported or shared cannot be snapshotted; reject before retaining imports or mutating store state")
			}
			return errors.New("wago: modules whose memory is imported or shared cannot be snapshotted")
		}
	}
	if c.memoryCount() > 1 && (len(c.Imports) != 0 || len(c.GlobalImports) != 0 || c.tableImportCount() != 0) {
		return errors.New("wago: owned local modules with multiple memories and function, global, or table imports cannot be snapshotted; reject before retaining imports or running start")
	}
	return nil
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
const snapshotVersion = 3

// MarshalBinary encodes the snapshot to a self-contained blob: the compiled
// module followed by the captured memory and globals. Trailing zero bytes of
// linear memory are trimmed (restore re-zeroes the tail), so a snapshot of a
// program that only touched the low end of a large heap stays small.
func (s *Snapshot) MarshalBinary() ([]byte, error) {
	if s == nil || s.c == nil {
		return nil, errors.New("wago: cannot marshal a snapshot with no bound module")
	}
	if err := validateSnapshotModule(s.c); err != nil {
		return nil, err
	}
	cb, err := s.c.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("wago: snapshot module: %w", err)
	}

	memories := snapshotMemories(s)
	if err := validateSnapshotMemories(s.c, memories); err != nil {
		return nil, fmt.Errorf("wago: snapshot memories: %w", err)
	}
	trimmed := make([][]byte, len(memories))
	storedBytes := 0
	for i, memory := range memories {
		n := len(memory.image)
		for n > 0 && memory.image[n-1] == 0 {
			n--
		}
		trimmed[i] = memory.image[:n]
		storedBytes += n
	}

	passiveDataLens := snapshotPassiveDataLens(s)
	out := make([]byte, 0, len(snapshotMagic)+2+len(cb)+storedBytes+len(memories)*12+len(s.globals)*17+len(passiveDataLens)*5+40)
	out = append(out, snapshotMagic...)
	out = append(out, snapshotVersion, byte(s.kind))
	out = binary.AppendUvarint(out, uint64(len(cb)))
	out = append(out, cb...)
	out = binary.AppendUvarint(out, uint64(len(memories)))
	for i, memory := range memories {
		out = binary.AppendUvarint(out, uint64(memory.pages))
		out = binary.AppendUvarint(out, uint64(len(trimmed[i])))
		out = append(out, trimmed[i]...)
	}
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
// compiled module so the snapshot is ready to Instantiate. Restored
// instances take no imports (the snapshot's disk form cannot carry host function
// closures); attach them by re-creating the snapshot in-process if needed.
func LoadSnapshot(b []byte) (*Snapshot, error) {
	return loadSnapshotWith(b, Load)
}

func loadSnapshotWith(b []byte, loadCompiled func([]byte) (*Compiled, error)) (*Snapshot, error) {
	if !IsSnapshot(b) {
		return nil, errors.New("wago: not a snapshot blob")
	}
	p := b[len(snapshotMagic):]
	version := p[0]
	if version < 1 || version > snapshotVersion {
		return nil, fmt.Errorf("wago: snapshot version %d unsupported (want 1 through %d)", version, snapshotVersion)
	}
	kind := SnapshotKind(p[1])
	rd := &snapReader{buf: p[2:]}

	cb := rd.sizedBytes("compiled module")
	var memories []memorySnap
	if version >= 3 {
		memories = make([]memorySnap, rd.count("memory", 2))
		for i := range memories {
			pages := rd.uvarint()
			if pages > uint64(^uint32(0)) {
				rd.err = fmt.Errorf("memory %d page count %d overflows u32", i, pages)
				break
			}
			image := rd.sizedBytes(fmt.Sprintf("memory %d image", i))
			memories[i] = memorySnap{pages: uint32(pages), image: image}
		}
	} else {
		pages := rd.uvarint()
		if pages > uint64(^uint32(0)) {
			rd.err = fmt.Errorf("memory page count %d overflows u32", pages)
		}
		image := rd.sizedBytes("memory image")
		memories = []memorySnap{{pages: uint32(pages), image: image}}
	}
	globals := make([]globalSnap, rd.count("global", 17))
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
		passiveDataLens = make([]uint32, rd.count("passive data length", 1))
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

	c, err := loadCompiled(cb)
	if err != nil {
		return nil, fmt.Errorf("wago: snapshot module: %w", err)
	}
	if err := validateSnapshotModule(c); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("wago: snapshot module: %w", err)
	}
	if version < 3 && c.memoryCount() == 0 && len(memories) == 1 && memories[0].pages == 0 && len(memories[0].image) == 0 {
		memories = nil // legacy snapshots always carried an empty memory-0 record
	}
	if err := validateSnapshotMemories(c, memories); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("wago: snapshot memories: %w", err)
	}
	if version == 1 {
		passiveDataLens = compiledPassiveDataLens(c)
	} else if err := validatePassiveDataLens(c, passiveDataLens); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("wago: snapshot passive data: %w", err)
	}
	for i := range memories {
		memories[i].image = append([]byte(nil), memories[i].image...)
	}
	s := &Snapshot{c: c, kind: kind, memories: memories, globals: globals, passiveDataLens: passiveDataLens}
	if len(memories) != 0 {
		s.memPages, s.memory = memories[0].pages, memories[0].image
	}
	return s, nil
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

func (r *snapReader) sizedBytes(label string) []byte {
	n := r.count(label+" byte", 1)
	return r.bytes(n)
}

func (r *snapReader) count(label string, minBytesPerItem int) int {
	v := r.uvarint()
	if r.err != nil {
		return 0
	}
	if v > uint64(maxInt()) {
		r.err = fmt.Errorf("%s count %d overflows int", label, v)
		return 0
	}
	if minBytesPerItem > 0 && v > uint64(len(r.buf)/minBytesPerItem) {
		r.err = fmt.Errorf("%s count %d exceeds remaining snapshot bytes %d", label, v, len(r.buf))
		return 0
	}
	return int(v)
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
