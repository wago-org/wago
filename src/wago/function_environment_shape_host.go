//go:build (linux || (darwin && arm64)) && !riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"sync"
)

// Keep these target shapes explicit: host builds do not use RV64 exception
// scratch or dependency receipt snapshots. Common ownership operations live in
// the Linux implementation files; RV64 keeps its original fields and offsets.
// Direct fields also preserve the existing internal keyed construction API.
type managedFunctionCollector struct {
	mu                  sync.Mutex // observer journal only; maps are collector-domain serialized
	graph               *managedFunctionGraph
	collector           *gc.Collector
	typeID              gc.TypeID
	envs                map[gc.Ref]*managedFunctionEnv    // weak, removed before handle reuse
	byOwner             map[*Instance]*managedFunctionEnv // same weak identity
	byDescriptor        map[uint64]*managedFunctionEnv
	shadows             map[gc.Ref]managedFunctionEdge   // inline first producer; null tombstones preserve Tiny cursors
	extraShadows        map[gc.Ref][]managedFunctionEdge // cold overflow for multiple producers
	aggregate           *managedFunctionAggregateScratch
	globalOwners        map[uint32]*managedFunctionMember
	tableOwners         map[uint32]*managedFunctionMember
	hostEnvSlots        map[uint32]gc.Ref                 // floating roots, excluded from the quiescent joint mark
	conversionHostSlots map[uint32]*gcConversionContainer // collector-local physical host receipts
	conversionFloating  map[uint32]*gcConversionIdentity
	hostFreeSlots       []uint32
	authorized          bool // only a validated joint mark may make slots conditional
	liveOwners          map[*managedFunctionMember]bool
	reclaimed           []*managedFunctionEnv
	closed              bool
}

type managedFunctionMark struct {
	graph              *managedFunctionGraph
	epoch              uint64
	domains            []*managedFunctionMarkDomain
	byCollector        map[*gc.Collector]*managedFunctionMarkDomain
	live               map[*managedFunctionMember]bool
	owners             []*managedFunctionMember
	ownerAt, ownerSlot int
	edges              map[*managedFunctionMember][]*managedFunctionMember
	incoming           map[*managedFunctionMember]int
	err                error
	complete           bool
	members            []*managedFunctionMember
	nativeCursor       managedFunctionNativeRootCursor
	nativeDone         bool
}
