package wago

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

// offHeapPtr reinterprets a known off-heap address — JIT arena / table-descriptor
// memory, kept live by arena/table ownership and never on the Go heap — as an
// unsafe.Pointer. Routing through *uintptr avoids a direct uintptr→unsafe.Pointer
// conversion, which go vet's unsafeptr pass flags (it cannot prove the target is
// non-heap). Use ONLY for addresses into that off-heap memory; there is no
// live-pointer hazard there.
func offHeapPtr(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

// Instance is ready for repeated Invoke calls.
type Instance struct {
	c                      *Compiled
	eng                    *runtime.Engine
	jm                     *runtime.JobMemory
	memory                 *Memory // the memory object (owned or host-imported)
	ar                     *runtime.Arena
	base                   uintptr
	hosts                  map[string]HostFunc
	imports                Imports // the imports as provided to Instantiate
	hostLog                []byte
	ctrl                   []byte                              // sync host-call control frame (nil in async mode)
	syncHosts              []HostFunc                          // per import-func-index host, sync mode only
	hostCall               runtime.HostCall                    // per-instance sync host dispatcher, allocated once
	pluginState            atomic.Pointer[instancePluginState] // allocated only after privileged instance services activate
	globals                []byte                              // pointer table handed to JIT code
	globalCells            []*Global
	table                  *Table        // lazily created importer-owned local export-handle chain
	tableDescPtr           uintptr       // local/imported descriptor address; arena/table ownership keeps it live
	tableDescLen           int           // descriptor byte length for safe slice reconstruction
	funcRefDescs           []byte        // canonical funcref descriptor handles for this instance's function index space
	passiveDataDesc        []byte        // per-instance data-segment descriptors; active slots start dropped
	thunkMem               []byte        // executable mapping for host-func-in-table log thunks (nil if none)
	gc                     *gc.Collector // nil for modules with no Wasm GC descriptors/runtime use
	serArgs, results, trap []byte
	resultVals             []uint64       // reusable Invoke result buffer (valid until the next call)
	ic                     [4]invokeCache // tiny fixed export resolution cache
	icNext                 uint8          // round-robin replacement cursor
	refStore               *referenceStore
	lifeMu                 sync.Mutex
	resourceRefs           int
	closed                 bool // logical close; retained references may defer physical release
	resourcesClosed        bool
	ownsMem                bool    // false when memory is host-imported (don't close it)
	syncMode               bool    // true when host imports use the synchronous re-entry protocol
	nativeControlShared    bool    // entered from another instance; prepared control fields may be overwritten
	nativeContext          uintptr // arena-backed context bytes rebound before every native entry

	// rt is set when the instance is created through Runtime.Instantiate, so
	// Instance.Call and Instance.Close can fire lifecycle hooks. It is nil for
	// low-level package-level Instantiate, which stays hook-free.
	rt *Runtime
}

// invokeCache memoizes per-export work so hot Invoke loops skip the exports map
// probe and the fat ValType width comparisons on every call. Instance keeps a
// few fixed slots because real AS loops commonly interleave the business export
// with __collect, __pin, or paired request/response exports.
type invokeCache struct {
	export            string
	valid             bool
	li                int // local index, or -1-import index for an InstanceExport re-export
	paramSlots        int
	resultSlots       int
	hasFuncRefParams  bool
	hasFuncRefResults bool
	resultWide        []bool // one entry per returned uint64 slot; false means read low 32 bits
}
