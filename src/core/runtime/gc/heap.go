package gc

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type Config struct {
	NurseryBytes         uint32
	OldBlockBytes        uint32
	LargeObjectBytes     uint32
	CollectEveryAlloc    bool
	TinyNurseryBytes     uint32
	ForceMajorEveryMinor bool
	VerifyAfterCollect   bool
	PoisonFreed          bool
	StressBarriers       bool
	DisableMovingNursery bool
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
)

type handleEntry struct {
	off, size uint32
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
	if config.TinyNurseryBytes != 0 {
		config.NurseryBytes = config.TinyNurseryBytes
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
	var max TypeID
	for _, d := range c.types {
		if d.ID > max {
			max = d.ID
		}
	}
	c.typeIndex = make([]int, int(max)+1)
	for i := range c.typeIndex {
		c.typeIndex[i] = -1
	}
	for i, d := range c.types {
		if int(d.ID) >= len(c.typeIndex) {
			return nil, errors.New("gc: internal type index error")
		}
		if c.typeIndex[d.ID] != -1 {
			return nil, fmt.Errorf("gc: duplicate type id %d", d.ID)
		}
		c.typeIndex[d.ID] = i
	}
	return c, nil
}

func (c *Collector) Close() {
	c.closed = true
	c.nursery = nil
	c.old = nil
	c.large = nil
	c.handles = nil
}
func (c *Collector) Stats() Stats { s := c.stats; s.LiveObjects = c.liveCount(); return s }

func (c *Collector) NewStruct(typeID TypeID) (Ref, error) { return c.NewStructDefault(typeID) }
func (c *Collector) NewStructDefault(typeID TypeID) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	sz, err := StructSize(d)
	if err != nil {
		return Null(), err
	}
	r, err := c.alloc(d, sz, 0)
	if err != nil {
		return Null(), err
	}
	return r, nil
}
func (c *Collector) NewArray(typeID TypeID, length uint32, init Value) (Ref, error) {
	r, err := c.NewArrayDefault(typeID, length)
	if err != nil {
		return Null(), err
	}
	d, _ := c.refDesc(r)
	for i := uint32(0); i < length; i++ {
		if err := c.storeValue(r, d, uint64(PayloadOffset)+uint64(i)*uint64(d.ElemSize), d.Elem, init); err != nil {
			return Null(), err
		}
	}
	return r, nil
}
func (c *Collector) NewArrayDefault(typeID TypeID, length uint32) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	sz, err := ArraySize(d, length)
	if err != nil {
		return Null(), err
	}
	return c.alloc(d, sz, length)
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
	if isRefKind(f.Kind) {
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
	if isRefKind(d.Elem) {
		c.WriteBarrierObject(ref, value.Ref)
		c.CardMarkArray(ref, index)
	}
	return c.storeValue(ref, d, uint64(PayloadOffset)+uint64(index)*uint64(d.ElemSize), d.Elem, value)
}

func (c *Collector) alloc(d TypeDesc, size, aux uint32) (Ref, error) {
	if c.closed {
		return Null(), errors.New("gc: collector closed")
	}
	if c.cfg.CollectEveryAlloc {
		_ = c.CollectMinor(nil)
	}
	sp := spaceNursery
	var off uint32
	if size >= c.cfg.LargeObjectBytes {
		sp = spaceLarge
		off = uint32(len(c.large))
		c.large = append(c.large, make([]byte, size)...)
	} else {
		if size > uint32(len(c.nursery))-c.nurseryBump {
			if err := c.CollectMinor(nil); err != nil {
				return Null(), err
			}
			if size > uint32(len(c.nursery))-c.nurseryBump {
				return Null(), errors.New("gc: nursery exhausted without roots")
			}
		}
		off = c.nurseryBump
		c.nurseryBump += size
	}
	h := c.newHandle(handleEntry{off: off, size: size, space: sp})
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
func (c *Collector) entry(r Ref) *handleEntry { return &c.handles[handleOf(r)] }
func (c *Collector) bytes(r Ref) []byte {
	e := c.entry(r)
	switch e.space {
	case spaceNursery:
		return c.nursery[e.off : e.off+e.size]
	case spaceOld:
		return c.old[e.off : e.off+e.size]
	case spaceLarge:
		return c.large[e.off : e.off+e.size]
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

func (c *Collector) loadValue(r Ref, off uint64, k StorageKind) (Value, error) {
	b := c.bytes(r)
	if off >= uint64(len(b)) {
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
	b := c.bytes(r)
	if off >= uint64(len(b)) {
		return errors.New("gc: store out of bounds")
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
