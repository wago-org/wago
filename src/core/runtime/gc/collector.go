package gc

import (
	"errors"
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
	Profile              Profile
	Allocator            AllocatorKind
	Runtime              RuntimeKind
	NurseryBytes         uint32
	OldBlockBytes        uint32
	LargeObjectBytes     uint32
	CollectEveryAlloc    bool
	StressNurseryBytes   uint32
	ForceMajorEveryMinor bool
	VerifyAfterCollect   bool
	PoisonFreed          bool
	StressBarriers       bool
	DisableMovingNursery bool
	// DisableCollection keeps every object in the bounded throughput heap and
	// returns an allocation error on exhaustion. It is used by general WasmGC
	// code until native frame roots can be published at every safepoint.
	DisableCollection     bool
	TinyHeapBytes         uint32
	TinyBlockBytes        uint32
	TinyStepBudget        uint32
	TinyCollectEveryAlloc bool
	TinyStepEveryAlloc    bool
	ThroughputHeapBytes   uint32
	ThroughputPageBytes   uint32
	// ThroughputClassLimit is zero for the default or exactly one of the
	// built-in throughput size classes. Values between classes are rejected rather
	// than rounded. Objects above the limit use large-span allocation.
	ThroughputClassLimit uint32
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
	tiny        tinyHeap
	tinyGC      tinyGC
	throughput  throughputHeap
	handles     []handleEntry // index 0 is never used; Ref stores index<<1.
	freeHandles []uint32
	mark        []bool
	markStack   []uint32
	remembered  []uint32
	objectCards []objectCard
	slotCards   []slotCard
	globalSlots []Ref
	tableSlots  []Ref
	stats       Stats
	closed      bool
}

const defaultNursery = 64 << 10
const defaultLarge = 32 << 10

var errCollectorClosed = errors.New("gc: collector closed")

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

// Close releases heap backing storage and makes live heap operations return
// errCollectorClosed. It is idempotent; Stats remains safe for post-close
// counters, while unchecked root-slot reads return null after slots are released.
func (c *Collector) Close() {
	c.closed = true
	c.nursery = nil
	c.tiny.Close()
	c.throughput.Close()
	c.handles = nil
	c.freeHandles = nil
	c.mark = nil
	c.markStack = nil
	c.remembered = nil
	c.objectCards = nil
	c.slotCards = nil
	c.globalSlots = nil
	c.tableSlots = nil
	c.tinyGC.color = nil
	c.tinyGC.grayStack = nil
}

func (c *Collector) errIfClosed() error {
	if c.closed {
		return errCollectorClosed
	}
	return nil
}

// Stats returns collection/allocation counters. It remains safe after Close;
// LiveObjects is recomputed from retained handles and is zero once Close releases
// the handle table.
func (c *Collector) Stats() Stats { s := c.stats; s.LiveObjects = c.liveCount(); return s }
