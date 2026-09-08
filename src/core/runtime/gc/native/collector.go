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
	// Telemetry opts this collector into bounded cycle timing and deterministic
	// work counters when built with wago_gcstats. Nil keeps diagnostic builds on
	// the no-telemetry path. Ordinary builds discard the pointer. One recorder
	// must not be attached to multiple collectors concurrently.
	Telemetry *Telemetry

	NurseryBytes uint32
	// SurvivorBytes is the capacity of each of two bounded Throughput survivor
	// semispaces. Zero selects half the normalized Eden capacity. It is ignored
	// when DisableMovingNursery is set.
	SurvivorBytes uint32
	// MinorPauseTargetMicros opts adaptive tenuring into a wall-clock pause
	// target. Zero keeps adaptation deterministic from occupancy and old-space
	// pressure alone and performs no release-build clock reads.
	MinorPauseTargetMicros uint32
	OldBlockBytes          uint32
	LargeObjectBytes       uint32
	StressNurseryBytes     uint32
	TinyHeapBytes          uint32
	TinyBlockBytes         uint32
	// TinyStepBudget is the number of Step calls performed after an allocation
	// when TinyStepEveryAlloc is enabled. It does not scale one Step's internal
	// object-scan work vector.
	TinyStepBudget uint32
	// TinyPacingStepLimit bounds ordinary allocation-debt work before one
	// allocation. Near-exhaustion assistance may use up to eight times this
	// value, capped by the collector's fixed hard maximum. Zero selects one.
	TinyPacingStepLimit uint32
	ThroughputHeapBytes uint32
	ThroughputPageBytes uint32
	// ThroughputClassLimit is zero for the default or exactly one of the
	// built-in throughput size classes. Values between classes are rejected rather
	// than rounded. Objects above the limit use large-span allocation.
	ThroughputClassLimit uint32

	// Profile selects the heap profile. The zero value preserves the default
	// throughput collector behavior.
	Profile   Profile
	Allocator AllocatorKind
	Runtime   RuntimeKind

	CollectEveryAlloc    bool
	ForceMajorEveryMinor bool
	VerifyAfterCollect   bool
	PoisonFreed          bool
	StressBarriers       bool
	DisableMovingNursery bool
	// DisableCollection keeps every object in the bounded throughput heap and
	// returns an allocation error on exhaustion. It is used by general WasmGC
	// code until native frame roots can be published at every safepoint.
	DisableCollection     bool
	TinyCollectEveryAlloc bool
	TinyStepEveryAlloc    bool
}

type Stats struct {
	Allocations            uint64
	MinorCollections       uint64
	FullCollections        uint64
	MinorObjectsScanned    uint64
	MinorRememberedScanned uint64
	YoungBytesCopied       uint64
	PromotedBytes          uint64
	LiveObjects            uint32
	TenuringThreshold      uint8
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
	off, size  uint32
	allocSize  uint32
	cardSlot   uint32 // one-based index in Collector.objectCards
	class      uint16
	space      spaceKind
	remembered bool
}

type Collector struct {
	cfg                 Config
	nativeView          *NativeCollectorView
	nativeStructAlloc   nativeStructAllocState
	nativeAllocEpoch    uint32
	arraySlow           uint8
	types               []TypeDesc
	subtypeIntervals    []uint64 // packed DFS [pre, post] interval by canonical TypeID
	objectAlign         uint32
	nursery             []byte // Eden followed by two survivor semispaces.
	nurseryBump         uint32
	survivorBytes       uint32
	survivorBump        uint32
	survivorFrom        uint8
	tenuringThreshold   uint8
	lastFullCollections uint64
	tiny                tinyHeap
	tinyGC              tinyGC
	throughput          throughputHeap
	handles             []handleEntry // index 0 is never used; Ref stores index<<1.
	freeHandles         []uint32
	nurseryHandles      []uint32 // dense live nursery set; minor collection never scans all old handles
	mark                []bool
	markStack           []uint32
	promotionScratch    []plannedPromotion
	remembered          []uint32
	objectCards         []objectCard
	cardBytes           uint32
	freeObjectCardSlot  uint32 // one-based tombstone free-list head; links use objectCard.next
	slotCards           []slotCard
	globalCardBits      []uint64
	tableCardBits       []uint64
	cardFallback        bool // shared full remembered-object/persistent-root scan
	globalSlots         []Ref
	tableSlots          []Ref
	stats               Stats
	rootMarkMode        uint8
	telemetryRootClass  RootClass
	closed              bool
	checkedHandles      *[]uint64
}

const defaultNursery = 64 << 10
const defaultLarge = 32 << 10

var errCollectorClosed = errors.New("gc: collector closed")

func NewCollector(config Config, types []TypeDesc) (*Collector, error) {
	if err := ValidateNativeABI(); err != nil {
		return nil, err
	}
	var err error
	config, err = normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	if !collectorTelemetryEnabled {
		config.Telemetry = nil
	}
	if config.Profile == ProfileTiny {
		return newTinyCollector(config, types)
	}
	if config.LargeObjectBytes == 0 {
		config.LargeObjectBytes = defaultLarge
	}
	if err := ValidateTypeDescs(types); err != nil {
		return nil, err
	}
	objectAlign := requiredObjectAlignment(types)
	if objectAlign < 16 {
		// Runtime GC domains may append canonically distinct module types after
		// construction. Wasm storage alignment is capped at v128's 16 bytes, so
		// reserving that alignment once avoids relocating a live nursery later.
		objectAlign = 16
	}
	nurseryBackingBytes := align(config.NurseryBytes, 16) + 2*config.SurvivorBytes
	c := &Collector{
		cfg: config, types: append([]TypeDesc(nil), types...), objectAlign: objectAlign,
		nursery: makeAlignedBytes(nurseryBackingBytes, uintptr(objectAlign)), survivorBytes: config.SurvivorBytes,
		tenuringThreshold: defaultTenuringThreshold, handles: []handleEntry{{}}, cardBytes: defaultThroughputCardBytes,
	}
	if config.DisableMovingNursery || config.SurvivorBytes == 0 {
		c.tenuringThreshold = 1
	}
	if c.telemetryEnabled() {
		c.cfg.Telemetry.attach(config.Profile, 0)
	}
	if err := c.initSubtypeIntervals(); err != nil {
		return nil, err
	}
	if err := c.throughput.Init(config); err != nil {
		return nil, err
	}
	c.initNativeView()
	return c, nil
}

// Close releases heap backing storage and makes live heap operations return
// errCollectorClosed. It is idempotent; Stats remains safe for post-close
// counters, while unchecked root-slot reads return null after slots are released.
func (c *Collector) Close() {
	c.discardNativeStructHandles()
	c.closed = true
	c.nursery = nil
	c.tiny.Close()
	c.throughput.Close()
	c.handles = nil
	c.checkedHandles = nil
	c.freeHandles = nil
	c.nurseryHandles = nil
	c.mark = nil
	c.markStack = nil
	c.subtypeIntervals = nil
	c.promotionScratch = nil
	c.remembered = nil
	c.objectCards = nil
	c.freeObjectCardSlot = 0
	c.slotCards = nil
	c.globalCardBits = nil
	c.tableCardBits = nil
	c.globalSlots = nil
	c.tableSlots = nil
	c.refreshNativeView()
	c.tinyGC.color = nil
	c.tinyGC.grayStack = nil
}

// AddTypes appends immutable Runtime-domain type descriptors without relocating
// live objects. Callers serialize this with native readers, allocation, and
// collection. IDs must
// be new, and any appended supertype must already exist or appear in the same
// append batch.
func (c *Collector) AddTypes(types []TypeDesc) error {
	if c == nil || c.closed {
		return errCollectorClosed
	}
	if len(types) == 0 {
		return nil
	}
	combined := make([]TypeDesc, 0, len(c.types)+len(types))
	combined = append(combined, c.types...)
	combined = append(combined, types...)
	if err := ValidateTypeDescs(combined); err != nil {
		return err
	}
	requiredAlign := requiredObjectAlignment(combined)
	if requiredAlign > c.objectAlign {
		return errors.New("gc: appended type alignment exceeds collector backing alignment")
	}
	if c.cfg.Profile == ProfileTiny && requiredAlign > c.tiny.blockBytes {
		return errors.New("gc: appended type alignment exceeds tiny block size")
	}
	oldTypes, oldIntervals := c.types, c.subtypeIntervals
	c.types = combined
	if err := c.initSubtypeIntervals(); err != nil {
		c.types, c.subtypeIntervals = oldTypes, oldIntervals
		return err
	}
	c.refreshNativeView()
	return nil
}

// Profile reports the collector's immutable barrier/allocation profile.
func (c *Collector) Profile() Profile {
	if c == nil {
		return ProfileThroughput
	}
	return c.cfg.Profile
}

func (c *Collector) telemetryEnabled() bool {
	return collectorTelemetryEnabled && c != nil && c.cfg.Telemetry != nil
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
func (c *Collector) Stats() Stats {
	s := c.stats
	s.LiveObjects = c.liveCount()
	s.TenuringThreshold = c.tenuringThreshold
	return s
}
