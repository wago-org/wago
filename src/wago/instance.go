package wago

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
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
	c                       *Compiled
	eng                     *runtime.Engine
	jm                      *runtime.JobMemory
	memory                  *Memory // the memory object (owned or host-imported)
	ar                      *runtime.Arena
	base                    uintptr
	hosts                   map[string]HostFunc
	imports                 Imports // the imports as provided to Instantiate
	hostLog                 []byte
	ctrl                    []byte                              // sync host-call control frame (nil in async mode)
	syncHosts               []syncHostBinding                   // immutable per-import sync host bindings
	hostCall                runtime.HostCall                    // active instance's bound host imports
	pluginState             atomic.Pointer[instancePluginState] // allocated only after privileged instance services activate
	globals                 []byte                              // pointer table handed to JIT code
	globalCells             []*Global
	table                   *Table        // lazily created importer-owned local export-handle chain
	tableDescPtr            uintptr       // local/imported descriptor address; arena/table ownership keeps it live
	tableDescLen            int           // descriptor byte length for safe slice reconstruction
	funcRefDescs            []byte        // canonical funcref descriptor handles for this instance's function index space
	passiveDataDesc         []byte        // per-instance data-segment descriptors; active slots start dropped
	thunkMem                []byte        // executable mapping for host-func-in-table log thunks (nil if none)
	gc                      *gc.Collector // nil for modules with no Wasm GC descriptors/runtime use
	gcTypeMap               *gcTypeMapping
	gcNativeView            *gc.NativeInstanceView
	serArgs, results, trap  []byte
	resultVals              []uint64       // reusable Invoke result buffer (valid until the next call)
	ic                      [4]invokeCache // tiny fixed export resolution cache
	pluginGCImports         map[uint32]struct{}
	refStore                *referenceStore
	lifeMu                  sync.Mutex
	resourceRefs            int
	invocationState         atomic.Uint32 // high bit closes entry; low bits count active public invocations
	closed                  bool          // logical close; retained references may defer physical release
	finalizing              bool          // one goroutine owns quiescent finalization
	resourcesClosed         bool
	icNext                  uint8 // round-robin invoke-cache replacement cursor
	physicalFinalizer       func()
	ownsMem                 bool                     // false when memory 0 is host-imported (don't close it)
	memoryDir               *instanceMemoryDirectory // allocated only for indexed memory execution
	syncMode                bool                     // true when host imports use the synchronous re-entry protocol
	constructionActive      bool                     // registration through terminal instantiation observation
	constructionReservation *pluginOperationReservation
	executionFlags          atomic.Uint32 // independent eligibility and cross-instance native-control sharing
	nativeContext           uintptr       // arena-backed context bytes rebound before every native entry
	instructionState        instructionState

	// rt is set when the instance is created through Runtime.Instantiate, so
	// Instance.Call and Instance.Close can fire lifecycle hooks. It is nil for
	// low-level package-level Instantiate, which stays hook-free.
	rt *Runtime

	// moduleIdentity is an opaque token, not a Compiled pointer. It lets an
	// instance finish its own lifecycle after its Module wrapper has closed.
	moduleIdentity ModuleIdentity
}

// instanceMemoryDirectory is allocated only after indexed memory execution is
// admitted. Memory 0 stays in Instance.memory/ownsMem so ordinary single-memory
// instances carry only this nil sidecar pointer.
type instanceMemoryDirectory struct {
	memories []*Memory
	owns     []bool
	native   []byte     // fixed 16-byte entries consumed by indexed native code
	invokeMu sync.Mutex // threaded memory zero: protects reusable Invoke scratch state
	nativeMu sync.Mutex // threaded memory zero: one mutable native activation per instance
}

// invokeCache memoizes per-export work so hot Invoke loops skip the exports map
// probe and the fat ValType width comparisons on every call. Instance keeps a
// few fixed slots because real AS loops commonly interleave the business export
// with __collect, __pin, or paired request/response exports.
type invokeCache struct {
	export            string
	valid             bool
	entryMode         preparedEntryMode
	li                int // local index, or -1-import index for an InstanceExport re-export
	paramSlots        int
	resultSlots       int
	hasFuncRefParams  bool
	hasFuncRefResults bool
	resultWide        []bool // one entry per returned uint64 slot; false means read low 32 bits
}
