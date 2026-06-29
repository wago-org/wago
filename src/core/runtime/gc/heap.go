package gc

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Profile selects a supported allocator/runtime preset.
type Profile uint8

const (
	// ProfileThroughput uses wago's higher-throughput generational scaffold with
	// nursery allocation and reusable old/large-object spaces.
	ProfileThroughput Profile = iota
	// ProfileTiny uses a fixed-size, non-moving heap with compact block metadata
	// and incremental tri-color mark/sweep for constrained targets.
	ProfileTiny
)

// AllocatorKind selects the heap allocator family after Config normalization.
type AllocatorKind uint8

const (
	AllocatorPagedSizeClass AllocatorKind = iota
	AllocatorTinyFixedBlock
)

// RuntimeKind selects the GC runtime family after Config normalization.
type RuntimeKind uint8

const (
	RuntimeGenerational RuntimeKind = iota
	RuntimeIncrementalMarkSweep
)

type Config struct {
	// Profile selects the heap profile. The zero value preserves the default
	// throughput collector behavior.
	Profile               Profile
	Allocator             AllocatorKind
	Runtime               RuntimeKind
	NurseryBytes          uint32
	OldBlockBytes         uint32
	LargeObjectBytes      uint32
	CollectEveryAlloc     bool
	StressNurseryBytes    uint32
	ForceMajorEveryMinor  bool
	VerifyAfterCollect    bool
	PoisonFreed           bool
	StressBarriers        bool
	DisableMovingNursery  bool
	TinyHeapBytes         uint32
	TinyBlockBytes        uint32
	TinyStepBudget        uint32
	TinyCollectEveryAlloc bool
	TinyStepEveryAlloc    bool
	ThroughputHeapBytes   uint32
	ThroughputPageBytes   uint32
	ThroughputClassLimit  uint32
}

type Stats struct {
	Allocations      uint64
	MinorCollections uint64
	FullCollections  uint64
	LiveObjects      uint32
}

type spaceKind uint8

const (
	spaceFree spaceKind = iota
	spaceNursery
	spaceOld
	spaceLarge
	spaceTiny
)

type handleEntry struct {
	off, size uint32
	allocSize uint32
	class     uint16
	space     spaceKind
}

type Collector struct {
	cfg         Config
	types       []TypeDesc
	typeIndex   []int
	nursery     []byte
	nurseryBump uint32
	old         []byte
	large       []byte
	tiny        tinyHeap
	tinyGC      tinyGC
	throughput  throughputHeap
	handles     []handleEntry // index 0 is never used; Ref stores index<<1.
	freeHandles []uint32
	mark        []bool
	markStack   []uint32
	remembered  []uint32
	cards       []uint32
	globalSlots []Ref
	tableSlots  []Ref
	stats       Stats
	closed      bool
}

const defaultNursery = 64 << 10
const defaultLarge = 32 << 10

func NewCollector(config Config, types []TypeDesc) (*Collector, error) {
	var err error
	config, err = normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	if config.Profile == ProfileTiny {
		return newTinyCollector(config, types)
	}
	if config.StressNurseryBytes != 0 {
		config.NurseryBytes = config.StressNurseryBytes
	}
	if config.NurseryBytes == 0 {
		config.NurseryBytes = defaultNursery
	}
	if config.LargeObjectBytes == 0 {
		config.LargeObjectBytes = defaultLarge
	}
	if err := ValidateTypeDescs(types); err != nil {
		return nil, err
	}
	c := &Collector{cfg: config, types: append([]TypeDesc(nil), types...), nursery: make([]byte, config.NurseryBytes), handles: []handleEntry{{}}}
	if err := c.initTypeIndex(); err != nil {
		return nil, err
	}
	if err := c.throughput.Init(config); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Collector) Close() {
	c.closed = true
	c.nursery = nil
	c.old = nil
	c.large = nil
	c.tiny.Close()
	c.throughput.Close()
	c.handles = nil
}
func (c *Collector) Stats() Stats { s := c.stats; s.LiveObjects = c.liveCount(); return s }

func (c *Collector) NewStruct(typeID TypeID) (Ref, error) { return c.NewStructDefault(typeID) }
func (c *Collector) NewStructDefault(typeID TypeID) (Ref, error) {
	return c.NewStructDefaultWithRoots(typeID, nil)
}
func (c *Collector) NewStructDefaultWithRoots(typeID TypeID, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if err := checkDefaultable(d); err != nil {
		return Null(), err
	}
	sz, err := StructSize(d)
	if err != nil {
		return Null(), err
	}
	r, err := c.alloc(d, sz, 0, roots)
	if err != nil {
		return Null(), err
	}
	c.zeroObjectPayload(r)
	return r, nil
}
func (c *Collector) NewArray(typeID TypeID, length uint32, init Value) (Ref, error) {
	return c.NewArrayWithRoots(typeID, length, init, nil)
}
func (c *Collector) NewArrayWithRoots(typeID TypeID, length uint32, init Value, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if err := checkValueCompatible(d.Elem, init); err != nil {
		return Null(), err
	}
	if isRefKind(d.Elem) {
		if err := c.validateStoredRef(init.Ref, d.Elem == StorageRefNull); err != nil {
			return Null(), err
		}
	}
	sz, err := ArraySize(d, length)
	if err != nil {
		return Null(), err
	}
	r, err := c.alloc(d, sz, length, roots)
	if err != nil {
		return Null(), err
	}
	for i := uint32(0); i < length; i++ {
		if err := c.storeValue(r, d, uint64(PayloadOffset)+uint64(i)*uint64(d.ElemSize), d.Elem, init); err != nil {
			return Null(), err
		}
	}
	return r, nil
}
func (c *Collector) NewArrayDefault(typeID TypeID, length uint32) (Ref, error) {
	return c.NewArrayDefaultWithRoots(typeID, length, nil)
}
func (c *Collector) NewArrayDefaultWithRoots(typeID TypeID, length uint32, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if err := checkDefaultable(d); err != nil {
		return Null(), err
	}
	sz, err := ArraySize(d, length)
	if err != nil {
		return Null(), err
	}
	r, err := c.alloc(d, sz, length, roots)
	if err != nil {
		return Null(), err
	}
	c.zeroObjectPayload(r)
	return r, nil
}

func (c *Collector) ArrayLen(ref Ref) (uint32, error) {
	d, e := c.refDesc(ref)
	if e != nil {
		return 0, e
	}
	if d.Kind != KindArray {
		return 0, errors.New("gc: not array")
	}
	return c.header(ref).Aux, nil
}
func (c *Collector) StructGet(ref Ref, field uint32) (Value, error) {
	d, e := c.refDesc(ref)
	if e != nil {
		return Value{}, e
	}
	if d.Kind != KindStruct {
		return Value{}, errors.New("gc: not struct")
	}
	if field >= uint32(len(d.Fields)) {
		return Value{}, errors.New("gc: field out of range")
	}
	f := d.Fields[field]
	return c.loadValue(ref, uint64(PayloadOffset+f.Offset), f.Kind)
}
func (c *Collector) StructSet(ref Ref, field uint32, value Value) error {
	d, e := c.refDesc(ref)
	if e != nil {
		return e
	}
	if d.Kind != KindStruct {
		return errors.New("gc: not struct")
	}
	if field >= uint32(len(d.Fields)) {
		return errors.New("gc: field out of range")
	}
	f := d.Fields[field]
	if err := checkValueCompatible(f.Kind, value); err != nil {
		return err
	}
	if isRefKind(f.Kind) {
		if err := c.validateStoredRef(value.Ref, f.Kind == StorageRefNull); err != nil {
			return err
		}
		c.WriteBarrierObject(ref, value.Ref)
	}
	return c.storeValue(ref, d, uint64(PayloadOffset+f.Offset), f.Kind, value)
}
func (c *Collector) ArrayGet(ref Ref, index uint32) (Value, error) {
	d, e := c.refDesc(ref)
	if e != nil {
		return Value{}, e
	}
	if d.Kind != KindArray {
		return Value{}, errors.New("gc: not array")
	}
	ln := c.header(ref).Aux
	if index >= ln {
		return Value{}, errors.New("gc: index out of range")
	}
	return c.loadValue(ref, uint64(PayloadOffset)+uint64(index)*uint64(d.ElemSize), d.Elem)
}
func (c *Collector) ArraySet(ref Ref, index uint32, value Value) error {
	d, e := c.refDesc(ref)
	if e != nil {
		return e
	}
	if d.Kind != KindArray {
		return errors.New("gc: not array")
	}
	ln := c.header(ref).Aux
	if index >= ln {
		return errors.New("gc: index out of range")
	}
	if err := checkValueCompatible(d.Elem, value); err != nil {
		return err
	}
	if isRefKind(d.Elem) {
		if err := c.validateStoredRef(value.Ref, d.Elem == StorageRefNull); err != nil {
			return err
		}
		c.WriteBarrierObject(ref, value.Ref)
		c.CardMarkArray(ref, index)
	}
	return c.storeValue(ref, d, uint64(PayloadOffset)+uint64(index)*uint64(d.ElemSize), d.Elem, value)
}

func (c *Collector) alloc(d TypeDesc, size, aux uint32, roots RootSet) (Ref, error) {
	if c.cfg.Profile == ProfileTiny {
		return c.tinyAlloc(d, size, aux, roots)
	}
	if c.closed {
		return Null(), errors.New("gc: collector closed")
	}
	if c.cfg.CollectEveryAlloc {
		if roots == nil {
			return Null(), errors.New("gc: allocation-triggered collection requires roots")
		}
		if err := c.CollectMinor(roots); err != nil {
			return Null(), err
		}
	}
	sp := spaceNursery
	var off uint32
	var e handleEntry
	if size >= c.cfg.LargeObjectBytes {
		var err error
		e, err = c.throughput.alloc(size, spaceLarge)
		if err != nil {
			if roots == nil {
				return Null(), errors.New("gc: large-object space exhausted and no roots were supplied")
			}
			if err := c.CollectFull(roots); err != nil {
				return Null(), err
			}
			e, err = c.throughput.alloc(size, spaceLarge)
			if err != nil {
				return Null(), err
			}
		}
		sp = spaceLarge
		off = e.off
	} else {
		if size > uint32(len(c.nursery))-c.nurseryBump {
			if roots == nil {
				return Null(), errors.New("gc: nursery exhausted and no roots were supplied")
			}
			if err := c.CollectMinor(roots); err != nil {
				return Null(), err
			}
			if size > uint32(len(c.nursery))-c.nurseryBump {
				return Null(), errors.New("gc: nursery exhausted without roots")
			}
		}
		off = c.nurseryBump
		c.nurseryBump += size
	}
	if sp == spaceNursery {
		e = handleEntry{off: off, size: size, allocSize: size, space: sp}
	}
	h := c.newHandle(e)
	r := makeObjRef(h)
	flags := uint32(0)
	if !d.HasRefs {
		flags |= FlagPointerFree
	}
	if sp == spaceLarge {
		flags |= FlagLarge
	}
	c.writeHeader(r, ObjHeader{TypeID: uint32(d.ID), Size: size, Aux: aux, Flags: flags})
	c.stats.Allocations++
	return r, nil
}
func (c *Collector) newHandle(e handleEntry) uint32 {
	if n := len(c.freeHandles); n > 0 {
		h := c.freeHandles[n-1]
		c.freeHandles = c.freeHandles[:n-1]
		c.handles[h] = e
		return h
	}
	c.handles = append(c.handles, e)
	c.mark = append(c.mark, false)
	c.tinyGC.color = append(c.tinyGC.color, tinyWhite)
	return uint32(len(c.handles) - 1)
}

func (c *Collector) desc(id TypeID) (TypeDesc, error) {
	if int(id) >= len(c.typeIndex) || c.typeIndex[id] < 0 {
		return TypeDesc{}, fmt.Errorf("gc: unknown type id %d", id)
	}
	return c.types[c.typeIndex[id]], nil
}
func (c *Collector) refDesc(r Ref) (TypeDesc, error) {
	if !r.IsObj() {
		return TypeDesc{}, errors.New("gc: ref is not object")
	}
	h := handleOf(r)
	if h == 0 || int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
		return TypeDesc{}, errors.New("gc: invalid object ref")
	}
	return c.desc(TypeID(c.header(r).TypeID))
}
func (c *Collector) validObjectRef(r Ref) bool {
	if !r.IsObj() {
		return false
	}
	h := handleOf(r)
	return h != 0 && int(h) < len(c.handles) && c.handles[h].space != spaceFree
}

func (c *Collector) validateStoredRef(r Ref, nullable bool) error {
	if r.IsNull() {
		if nullable {
			return nil
		}
		return errors.New("gc: cannot store null in non-null ref slot")
	}
	if r.IsI31() {
		return nil
	}
	if !c.validObjectRef(r) {
		return errors.New("gc: invalid object ref")
	}
	return nil
}

func (c *Collector) entry(r Ref) *handleEntry { return &c.handles[handleOf(r)] }
func (c *Collector) bytes(r Ref) []byte {
	e := c.entry(r)
	switch e.space {
	case spaceNursery:
		return c.nursery[e.off : e.off+e.size]
	case spaceOld, spaceLarge:
		if c.throughput.limit != 0 {
			return c.throughput.bytes(*e)
		}
		if e.space == spaceOld {
			return c.old[e.off : e.off+e.size]
		}
		return c.large[e.off : e.off+e.size]
	case spaceTiny:
		return c.tiny.bytes(e.off, e.size)
	default:
		return nil
	}
}
func (c *Collector) header(r Ref) ObjHeader {
	b := c.bytes(r)
	return ObjHeader{binary.LittleEndian.Uint32(b[0:4]), binary.LittleEndian.Uint32(b[4:8]), binary.LittleEndian.Uint32(b[8:12]), binary.LittleEndian.Uint32(b[12:16])}
}
func (c *Collector) writeHeader(r Ref, h ObjHeader) {
	b := c.bytes(r)
	binary.LittleEndian.PutUint32(b[0:4], h.TypeID)
	binary.LittleEndian.PutUint32(b[4:8], h.Size)
	binary.LittleEndian.PutUint32(b[8:12], h.Aux)
	binary.LittleEndian.PutUint32(b[12:16], h.Flags)
}

func (c *Collector) zeroObjectPayload(r Ref) {
	if !c.validObjectRef(r) {
		return
	}
	e := c.entry(r)
	end := e.off + e.size
	if e.allocSize > e.size {
		end = e.off + e.allocSize
	}
	start := e.off + PayloadOffset
	if start > end {
		return
	}
	var b []byte
	switch e.space {
	case spaceNursery:
		if end > uint32(len(c.nursery)) {
			end = uint32(len(c.nursery))
		}
		b = c.nursery[start:end]
	case spaceOld, spaceLarge:
		if c.throughput.limit != 0 {
			if end > uint32(len(c.throughput.mem)) {
				end = uint32(len(c.throughput.mem))
			}
			b = c.throughput.mem[start:end]
		}
	case spaceTiny:
		if end > uint32(len(c.tiny.mem)) {
			end = uint32(len(c.tiny.mem))
		}
		b = c.tiny.mem[start:end]
	}
	for i := range b {
		b[i] = 0
	}
}

func (c *Collector) loadValue(r Ref, off uint64, k StorageKind) (Value, error) {
	b := c.bytes(r)
	_, size, err := storageLayout(k)
	if err != nil {
		return Value{}, err
	}
	if off > uint64(len(b)) || uint64(len(b))-off < uint64(size) {
		return Value{}, errors.New("gc: load out of bounds")
	}
	switch k {
	case StorageI8:
		return Value{Kind: k, Bits: uint64(b[off])}, nil
	case StorageI16:
		return Value{Kind: k, Bits: uint64(binary.LittleEndian.Uint16(b[off:]))}, nil
	case StorageI32, StorageF32:
		return Value{Kind: k, Bits: uint64(binary.LittleEndian.Uint32(b[off:]))}, nil
	case StorageI64, StorageF64:
		return Value{Kind: k, Bits: binary.LittleEndian.Uint64(b[off:])}, nil
	case StorageRef, StorageRefNull:
		return Value{Kind: k, Ref: Ref(binary.LittleEndian.Uint32(b[off:]))}, nil
	}
	return Value{}, errors.New("gc: bad kind")
}
func (c *Collector) storeValue(r Ref, d TypeDesc, off uint64, k StorageKind, v Value) error {
	_ = d
	b := c.bytes(r)
	_, size, err := storageLayout(k)
	if err != nil {
		return err
	}
	if off > uint64(len(b)) || uint64(len(b))-off < uint64(size) {
		return errors.New("gc: store out of bounds")
	}
	if err := checkValueCompatible(k, v); err != nil {
		return err
	}
	switch k {
	case StorageI8:
		b[off] = byte(v.Bits)
	case StorageI16:
		binary.LittleEndian.PutUint16(b[off:], uint16(v.Bits))
	case StorageI32, StorageF32:
		binary.LittleEndian.PutUint32(b[off:], uint32(v.Bits))
	case StorageI64, StorageF64:
		binary.LittleEndian.PutUint64(b[off:], v.Bits)
	case StorageRef, StorageRefNull:
		binary.LittleEndian.PutUint32(b[off:], uint32(v.Ref))
	default:
		return errors.New("gc: bad kind")
	}
	return nil
}

func checkDefaultable(d TypeDesc) error {
	switch d.Kind {
	case KindStruct:
		for i, f := range d.Fields {
			if f.Kind == StorageRef {
				return fmt.Errorf("gc: struct type %d field %d is non-null ref and not defaultable", d.ID, i)
			}
		}
	case KindArray:
		if d.Elem == StorageRef {
			return fmt.Errorf("gc: array type %d element is non-null ref and not defaultable", d.ID)
		}
	}
	return nil
}

func checkValueCompatible(k StorageKind, v Value) error {
	switch k {
	case StorageI8, StorageI16:
		if v.Kind != StorageI32 && v.Kind != k {
			return fmt.Errorf("gc: value kind %d incompatible with packed storage %d", v.Kind, k)
		}
	case StorageI32, StorageI64, StorageF32, StorageF64:
		if v.Kind != k {
			return fmt.Errorf("gc: value kind %d incompatible with storage %d", v.Kind, k)
		}
	case StorageRef:
		if !isRefKind(v.Kind) {
			return fmt.Errorf("gc: value kind %d incompatible with non-null ref storage", v.Kind)
		}
		if v.Ref.IsNull() {
			return errors.New("gc: cannot store null in non-null ref slot")
		}
	case StorageRefNull:
		if !isRefKind(v.Kind) {
			return fmt.Errorf("gc: value kind %d incompatible with nullable ref storage", v.Kind)
		}
	default:
		return errors.New("gc: bad kind")
	}
	return nil
}
