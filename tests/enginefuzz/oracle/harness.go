package oracle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"

	wago "github.com/wago-org/wago"
)

const (
	defaultEventLimit = 8192
	memoryLimitBytes  = 2 * 65536
	tableEntryLimit   = 32
)

const (
	rawInputI32 uint8 = iota
	rawInputI64
	rawMark
	rawObserveI32
	rawObserveI64
)

type rawEvent struct {
	kind uint8
	a    uint64
	b    uint64
}

// Observation is one complete, canonical engine-state result.
type Observation struct {
	Events []Event `json:"events,omitempty"`
	JSON   []byte  `json:"-"`
	Hash   string  `json:"hash"`
}

// Harness is a declarative Wago plug-in and its bounded per-worker controller.
// One Harness accepts one active case at a time. Use one Harness per parallel
// worker instead of sharing it across concurrent instances.
type Harness struct {
	mu         sync.Mutex
	resolver   *wago.CallerResolver
	pending    *Case
	active     *Case
	eventLimit int
}

// Case owns all imported state for one engine-state execution.
type Case struct {
	harness  *Harness
	seed     uint64
	identity wago.InstanceIdentity
	events   []rawEvent
	overflow bool
	global   *wago.Global
	memory   *wago.Memory
	table    *wago.Table
	finished bool
	closed   bool
}

var Definition = wago.PluginDefinition{
	ID:          "github.com/wago-org/wago/tests/enginefuzz/oracle",
	Name:        "Engine State Oracle",
	Version:     "0.1.0",
	Description: "Records deterministic Starshine engine-state events.",
	Stability:   wago.Experimental,
	Provenance: wago.PluginProvenance{
		Repository: "https://github.com/wago-org/wago",
		License:    "Apache-2.0",
	},
	Authorities: []wago.AuthorityRequest{
		{
			Name:   wago.AuthorityHostImportDefine,
			Mode:   wago.AuthorityRequired,
			Reason: "provide the fixed __fuzz observation ABI",
			Scope:  wago.AuthorityScope{Modules: []string{"__fuzz"}},
		},
		{
			Name:   wago.AuthorityHostCallerIdentify,
			Mode:   wago.AuthorityRequired,
			Reason: "bind events to the exact active fuzz instance",
		},
		{
			Name:   wago.AuthorityInstanceInstantiateIntercept,
			Mode:   wago.AuthorityRequired,
			Reason: "attach case state before the start function",
		},
	},
}

// NewHarness creates a bounded, initially idle harness.
func NewHarness() *Harness { return &Harness{eventLimit: defaultEventLimit} }

// PluginSet returns the exact provider and reviewed grants for this harness.
func (h *Harness) PluginSet() (wago.PluginSet, error) {
	digest, err := wago.DefinitionDigest(Definition)
	if err != nil {
		return wago.PluginSet{}, err
	}
	grants := make([]wago.AuthorityGrant, 0, len(Definition.Authorities))
	for _, request := range Definition.Authorities {
		grants = append(grants, wago.AuthorityGrant{Name: request.Name, Scope: request.Scope})
	}
	return wago.PluginSet{
		Providers: []wago.PluginProvider{{Definition: Definition, New: func() wago.Plugin { return h }}},
		Selections: []wago.PluginSelection{{
			ID:               Definition.ID,
			DefinitionDigest: digest,
			Direct:           true,
			Dependencies:     map[string]string{},
			Grants:           grants,
		}},
	}, nil
}

// Register declares the fixed __fuzz ABI and attaches pending state before a
// module's start function runs.
func (h *Harness) Register(reg *wago.Registrar) error {
	resolver, err := reg.HostCallers()
	if err != nil {
		return err
	}
	h.resolver = resolver
	imports, err := reg.HostImports()
	if err != nil {
		return err
	}
	module, err := imports.Module("__fuzz")
	if err != nil {
		return err
	}
	module.Func("input_i32", h.inputI32).Params(wago.ValI32).Results(wago.ValI32)
	module.Func("input_i64", h.inputI64).Params(wago.ValI32).Results(wago.ValI64)
	module.Func("mark", h.mark).Params(wago.ValI32)
	module.Func("observe_i32", h.observeI32).Params(wago.ValI32, wago.ValI32)
	module.Func("observe_i64", h.observeI64).Params(wago.ValI32, wago.ValI64)
	interceptor, err := reg.InstanceInstantiateInterceptor()
	if err != nil {
		return err
	}
	return interceptor.After(func(event wago.InstantiationEvent) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.pending == nil || h.active != nil {
			return fmt.Errorf("engine-state harness has no unique pending case")
		}
		h.pending.identity = event.Instance
		h.active, h.pending = h.pending, nil
		return nil
	})
}

// Begin creates the fresh resource imports and event buffer for one case.
func (h *Harness) Begin(seed uint64) (*Case, error) {
	memory, err := wago.NewMemory(1, 2)
	if err != nil {
		return nil, err
	}
	table, err := wago.NewTable(4, 8)
	if err != nil {
		_ = memory.Close()
		return nil, err
	}
	c := &Case{
		harness: h,
		seed:    seed,
		events:  make([]rawEvent, 0, min(h.eventLimit, 256)),
		global:  wago.NewGlobalI32(0, true),
		memory:  memory,
		table:   table,
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending != nil || h.active != nil {
		_ = c.closeResources()
		return nil, fmt.Errorf("engine-state harness already has an active case")
	}
	h.pending = c
	return c, nil
}

// InstantiateOptions returns fresh host-owned imports retained for trap-state
// observation. Supplying undeclared resources is harmless for other profiles.
func (c *Case) InstantiateOptions() []wago.InstantiateOption {
	return []wago.InstantiateOption{
		wago.WithImport("__fuzz", "state_global_i32", c.global),
		wago.WithImport("__fuzz", "state_memory", c.memory),
		wago.WithImport("__fuzz", "state_table", c.table),
	}
}

func (h *Harness) current(caller wago.HostModule) *Case {
	identity, err := h.resolver.Resolve(caller)
	if err != nil {
		panic(wago.HostTrap{Err: err})
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == nil || h.active.identity != identity {
		panic(wago.HostTrap{Err: fmt.Errorf("engine-state event from an unattached instance")})
	}
	return h.active
}

func (h *Harness) appendCase(c *Case, event rawEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active != c {
		panic(wago.HostTrap{Err: fmt.Errorf("engine-state event from an inactive case")})
	}
	if len(c.events) >= h.eventLimit {
		c.overflow = true
		return
	}
	c.events = append(c.events, event)
}

func (h *Harness) inputI32(caller wago.HostModule, params, results []uint64) {
	c := h.current(caller)
	channel := uint32(params[0])
	value := MixInput64(c.seed, channel, I32Salt)
	results[0] = uint64(uint32(value))
	h.appendCase(c, rawEvent{kind: rawInputI32, a: uint64(channel), b: value})
}

func (h *Harness) inputI64(caller wago.HostModule, params, results []uint64) {
	c := h.current(caller)
	channel := uint32(params[0])
	value := MixInput64(c.seed, channel, I64Salt)
	results[0] = value
	h.appendCase(c, rawEvent{kind: rawInputI64, a: uint64(channel), b: value})
}

func (h *Harness) mark(caller wago.HostModule, params, _ []uint64) {
	h.appendCase(h.current(caller), rawEvent{kind: rawMark, a: uint64(uint32(params[0]))})
}

func (h *Harness) observeI32(caller wago.HostModule, params, _ []uint64) {
	h.appendCase(h.current(caller), rawEvent{kind: rawObserveI32, a: uint64(uint32(params[0])), b: uint64(uint32(params[1]))})
}

func (h *Harness) observeI64(caller wago.HostModule, params, _ []uint64) {
	h.appendCase(h.current(caller), rawEvent{kind: rawObserveI64, a: uint64(uint32(params[0])), b: params[1]})
}

func hex32(value uint32) string { return fmt.Sprintf("%08x", value) }
func hex64(value uint64) string { return fmt.Sprintf("%016x", value) }

func expandRaw(raw []rawEvent) []Event {
	events := make([]Event, 1, len(raw)+1)
	events[0] = Event{"schema", Schema}
	for _, event := range raw {
		switch event.kind {
		case rawInputI32:
			events = append(events, Event{"input_i32", hex32(uint32(event.a)), hex32(uint32(event.b))})
		case rawInputI64:
			events = append(events, Event{"input_i64", hex32(uint32(event.a)), hex64(event.b)})
		case rawMark:
			events = append(events, Event{"mark", hex32(uint32(event.a))})
		case rawObserveI32:
			events = append(events, Event{"observe_i32", hex32(uint32(event.a)), hex32(uint32(event.b))})
		case rawObserveI64:
			events = append(events, Event{"observe_i64", hex32(uint32(event.a)), hex64(event.b)})
		}
	}
	return events
}

func trapClass(err error) (string, bool) {
	var trap *wago.TrapError
	if !errors.As(err, &trap) {
		return "", false
	}
	switch trap.Code {
	case wago.TrapUnreachable:
		return "explicit-unreachable", true
	case wago.TrapDivZero:
		return "integer-divide-by-zero", true
	case wago.TrapDivOverflow:
		return "signed-integer-division-overflow", true
	case wago.TrapTruncOverflow:
		return "invalid-conversion-to-integer", true
	case wago.TrapLinMemOutOfBounds, wago.TrapLinkedMemOutOfBounds:
		return "out-of-bounds-memory-access", true
	case wago.TrapTableOutOfBounds, wago.TrapIndirectOutOfBounds:
		return "out-of-bounds-table-access", true
	default:
		return trap.Code.String(), true
	}
}

func resourceNames(names []string, kind string, index int) ([]string, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("%s %d has no observation export", kind, index)
	}
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out, nil
}

func appendGlobals(events []Event, c *Case, metadata wago.ModuleMetadata, instance *wago.Instance) ([]Event, error) {
	for _, global := range metadata.Globals {
		names, err := resourceNames(global.Exports, "global", global.Index)
		if err != nil {
			return nil, err
		}
		if global.Type != wago.ValI32 {
			return nil, fmt.Errorf("global %d has unsupported observation type %s", global.Index, global.Type)
		}
		var bits uint64
		if instance != nil {
			value, err := instance.GlobalValue(names[0])
			if err != nil {
				return nil, err
			}
			bits = value.Bits()
		} else if global.ImportModule == "__fuzz" && global.ImportName == "state_global_i32" {
			bits = c.global.Get()
		} else {
			return nil, fmt.Errorf("global %d is unavailable after trapping start", global.Index)
		}
		events = append(events, Event{"global", global.Index, names, "i32", hex32(uint32(bits))})
	}
	return events, nil
}

func memoryHash(bytes []byte) string {
	digest := sha256.Sum256(bytes)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func appendMemories(events []Event, c *Case, metadata wago.ModuleMetadata, instance *wago.Instance) ([]Event, error) {
	for _, memory := range metadata.Memories {
		names, err := resourceNames(memory.Exports, "memory", memory.Index)
		if err != nil {
			return nil, err
		}
		var resource *wago.Memory
		if instance != nil {
			resource, err = instance.ExportedMemory(names[0])
		} else if memory.ImportModule == "__fuzz" && memory.ImportName == "state_memory" {
			resource = c.memory
		} else {
			return nil, fmt.Errorf("memory %d is unavailable after trapping start", memory.Index)
		}
		if err != nil {
			return nil, err
		}
		bytes := resource.UnsafeBytes()
		if len(bytes) > memoryLimitBytes {
			return nil, fmt.Errorf("memory %d has %d bytes, limit %d", memory.Index, len(bytes), memoryLimitBytes)
		}
		events = append(events, Event{"memory", memory.Index, names, len(bytes), memoryHash(bytes)})
	}
	return events, nil
}

func appendTables(events []Event, c *Case, metadata wago.ModuleMetadata, instance *wago.Instance) ([]Event, error) {
	for _, table := range metadata.Tables {
		names, err := resourceNames(table.Exports, "table", table.Index)
		if err != nil {
			return nil, err
		}
		var resource *wago.Table
		if instance != nil {
			resource, err = instance.ExportedTable(names[0])
		} else if table.ImportModule == "__fuzz" && table.ImportName == "state_table" {
			resource = c.table
		} else {
			return nil, fmt.Errorf("table %d is unavailable after trapping start", table.Index)
		}
		if err != nil {
			return nil, err
		}
		length := resource.Size()
		if length > tableEntryLimit {
			return nil, fmt.Errorf("table %d has %d entries, limit %d", table.Index, length, tableEntryLimit)
		}
		relations := make([]string, length)
		for entry := range relations {
			if instance == nil {
				null, err := resource.EntryIsNull(uint64(entry))
				if err != nil {
					return nil, err
				}
				if null {
					relations[entry] = "null"
				} else {
					// JavaScript retains imported tables after a trapping start, but
					// does not expose the partial instance needed to relate a guest
					// function to an export. Preserve the portable nullness state.
					relations[entry] = "non-null"
				}
				continue
			}
			function, nonNull, err := instance.TableFunctionIndex(names[0], uint64(entry))
			if err != nil {
				return nil, err
			}
			if nonNull {
				relations[entry] = fmt.Sprintf("funcidx:%d", function)
			} else {
				relations[entry] = "null"
			}
		}
		events = append(events, Event{"table", table.Index, names, length, relations})
	}
	return events, nil
}

// Finish appends the execution outcome and complete post-start resource state,
// then returns the exact bytes and SHA-256 hash. The instance must remain open
// until Finish returns. A trapping start passes a nil instance and its error.
func (c *Case) Finish(metadata wago.ModuleMetadata, instance *wago.Instance, executionErr error) (Observation, error) {
	h := c.harness
	h.mu.Lock()
	if c.finished {
		h.mu.Unlock()
		return Observation{}, fmt.Errorf("engine-state case is already finished")
	}
	c.finished = true
	if h.pending == c {
		h.pending = nil
	}
	if h.active == c {
		h.active = nil
	}
	raw := append([]rawEvent(nil), c.events...)
	overflow := c.overflow
	h.mu.Unlock()
	if overflow {
		return Observation{}, fmt.Errorf("engine-state event limit %d exceeded", h.eventLimit)
	}
	events := expandRaw(raw)
	if executionErr == nil {
		events = append(events, Event{"outcome", "returned"})
	} else if class, trapped := trapClass(executionErr); trapped {
		events = append(events, Event{"outcome", "trapped", class})
	} else {
		return Observation{}, fmt.Errorf("engine-state instantiation failed: %w", executionErr)
	}
	var err error
	events, err = appendGlobals(events, c, metadata, instance)
	if err == nil {
		events, err = appendMemories(events, c, metadata, instance)
	}
	if err == nil {
		events, err = appendTables(events, c, metadata, instance)
	}
	if err != nil {
		return Observation{}, err
	}
	canonical, err := Marshal(events)
	if err != nil {
		return Observation{}, err
	}
	return Observation{Events: events, JSON: canonical, Hash: Hash(canonical)}, nil
}

func (c *Case) closeResources() error {
	// A trapping start can leave a guest function in the imported table. Close
	// the table first so that reference releases can finish partial-instance
	// teardown before the scalar resources check for live importers.
	tableErr := c.table.Close()
	memoryErr := c.memory.Close()
	globalErr := c.global.Close()
	return errors.Join(tableErr, memoryErr, globalErr)
}

// Close releases the host-owned resources after the guest instance is closed.
func (c *Case) Close() error {
	h := c.harness
	h.mu.Lock()
	if c.closed {
		h.mu.Unlock()
		return nil
	}
	c.closed = true
	if h.pending == c {
		h.pending = nil
	}
	if h.active == c {
		h.active = nil
	}
	h.mu.Unlock()
	return c.closeResources()
}
