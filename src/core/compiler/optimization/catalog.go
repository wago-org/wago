// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package optimization owns the compiler optimization catalog shared by
// backends, the CLI, configuration, and standalone cross-compilation.
package optimization

import (
	"fmt"
	"sync"

	"github.com/wago-org/wago/internal/buildfeatures"
)

// Definition describes one user-configurable compiler optimization.
// Register new optimizations here, then bind them to their backend boolean in
// that backend's knobs.go. Every config and CLI surface is derived from this
// catalog.
type Definition struct {
	Name             string
	Label            string
	Description      string
	Default          bool
	Experimental     bool
	Architectures    []string
	requiredFeatures uint64
	requiredArch     string
}

// Info describes one optimization and its selected state.
type Info struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	Desc         string `json:"description"`
	On           bool   `json:"on"`
	Default      bool   `json:"default"`
	Experimental bool   `json:"experimental"`
}

// BindingSpec associates a registered optimization with the boolean currently
// consumed by one architecture backend. Inverted preserves public "on means
// enabled" semantics for legacy negative controls.
type BindingSpec struct {
	Name     string
	Value    *bool
	Inverted bool
	option   *Option
}

// Bind creates an ordinary architecture binding.
func Bind(name string, value *bool) BindingSpec { return BindingSpec{Name: name, Value: value} }

// BindOption binds a process default and resolves its lowering flag together.
func BindOption(name string, value *bool, option *Option) BindingSpec {
	return BindingSpec{Name: name, Value: value, option: option}
}

// BindInverted creates a binding whose implementation boolean disables the
// optimization when true.
func BindInverted(name string, value *bool) BindingSpec {
	return BindingSpec{Name: name, Value: value, Inverted: true}
}

type binding struct {
	definition Definition
	value      *bool
	inverted   bool
}

// Snapshot identifies one process-default selection from one Bindings. Its
// fields are deliberately private: only the owning Bindings can mint a token
// eligible for the revision-matched compile fast path.
type Snapshot struct {
	bindings *Bindings
	revision uint64
}

// ResolveSelection resolves overrides against the snapshot's catalog.
// Complete overrides preserve their captured values across default revisions.
func (s Snapshot) ResolveSelection(overrides map[string]bool) (Selection, error) {
	if s.bindings == nil {
		return Selection{}, nil
	}
	return s.bindings.ResolveSnapshot(overrides, s, nil)
}

// Selection is one immutable, architecture-specific optimization selection.
// The bit index follows the owning Bindings' stable catalog order. It is safe to
// copy into a compilation policy and read concurrently for the compilation's
// lifetime.
type Selection struct {
	bindings *Bindings
	bits     [2]uint64
}

// Option is a pre-resolved flag owned by one architecture's Bindings. Backends
// resolve these once during package initialization so hot lowering decisions
// need only a pointer comparison and bit test, never a string/map lookup.
type Option struct {
	bindings *Bindings
	mask     uint64
	word     uint8
}

// Enabled reports whether name is enabled in this selection. Unknown names and
// selections from another architecture report false.
func (s Selection) Enabled(name string) bool {
	if s.bindings == nil {
		return false
	}
	index, ok := s.bindings.indexOf(name)
	return ok && s.bits[index/64]&(uint64(1)<<uint(index%64)) != 0
}

// EnabledOption reports whether a pre-resolved option is enabled. An option
// from another architecture is rejected just like an unknown string name.
func (s Selection) EnabledOption(option Option) bool {
	return s.bindings != nil && s.bindings == option.bindings && s.bits[option.word]&option.mask != 0
}

// EnabledResolvedOption is the hot-path form for an Option resolved by the
// same Bindings that produced this Selection. Backend initialization validates
// that ownership once, so per-instruction lowering need only test the bit.
func (s *Selection) EnabledResolvedOption(option Option) bool {
	return s.bits[option.word]&option.mask != 0
}

// Valid reports whether the selection was resolved by a Bindings owner.
func (s Selection) Valid() bool { return s.bindings != nil }

// Owns reports whether the supplied selection was resolved by these bindings.
func (b *Bindings) Owns(s Selection) bool { return s.bindings == b }

// Option resolves name once for use on a backend's hot compile path. It panics
// for an unknown name because backend option inventories are initialization
// invariants already checked by NewBindings.
func (b *Bindings) Option(name string) Option {
	index, ok := b.indexOf(name)
	if !ok {
		panic(fmt.Sprintf("unknown %s optimization %q", b.arch, name))
	}
	return Option{bindings: b, mask: uint64(1) << uint(index%64), word: uint8(index / 64)}
}

// Bindings owns the complete, ordered set of bindings for one architecture.
// NewBindings panics on missing, duplicate, unknown, or nil bindings so an
// advertised optimization cannot reach execution without an implementation.
type Bindings struct {
	mu       sync.Mutex
	arch     string
	entries  []binding
	before   []bool
	changed  []int
	revision uint64
}

func NewBindings(arch string, specs ...BindingSpec) *Bindings {
	for index, spec := range specs {
		if spec.Value == nil {
			panic(fmt.Sprintf("%s optimization binding %q has a nil value", arch, spec.Name))
		}
		for previous := 0; previous < index; previous++ {
			if specs[previous].Name == spec.Name {
				panic(fmt.Sprintf("%s optimization binding %q is duplicated", arch, spec.Name))
			}
		}
		if _, ok := Lookup(arch, spec.Name); !ok {
			panic(fmt.Sprintf("%s optimization binding %q is not registered", arch, spec.Name))
		}
	}
	definitions := ForArch(arch)
	bindings := &Bindings{
		arch:     arch,
		entries:  make([]binding, 0, len(specs)),
		before:   make([]bool, len(specs)),
		changed:  make([]int, len(specs)),
		revision: 1,
	}
	for _, definition := range definitions {
		if !definition.availableInBuild(arch) {
			continue
		}
		var spec BindingSpec
		found := false
		for _, candidate := range specs {
			if candidate.Name == definition.Name {
				spec, found = candidate, true
				break
			}
		}
		if !found {
			panic(fmt.Sprintf("%s optimization %q has no backend binding", arch, definition.Name))
		}
		if spec.option != nil {
			index := len(bindings.entries)
			*spec.option = Option{bindings: bindings, mask: uint64(1) << uint(index%64), word: uint8(index / 64)}
		}
		bindings.entries = append(bindings.entries, binding{definition: definition, value: spec.Value, inverted: spec.Inverted})
	}
	return bindings
}

// indexOf resolves names against the small, fixed catalog. Bindings are created
// once during package initialization and options are pre-resolved for hot
// lowering, so a linear lookup avoids two startup hash maps without moving map
// work onto generated-code paths.
func (b *Bindings) indexOf(name string) (int, bool) {
	for index := range b.entries {
		if b.entries[index].definition.Name == name {
			return index, true
		}
	}
	return 0, false
}

// Infos returns current binding values in catalog order.
func (b *Bindings) Infos() []Info {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.infosLocked()
}

// Snapshot returns current binding values and the revision that identifies
// that exact selection. The values and revision are captured under one lock.
func (b *Bindings) Snapshot() ([]Info, Snapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.infosLocked(), b.snapshotLocked()
}

// CurrentSnapshot returns an allocation-free token for the current process-
// default selection.
func (b *Bindings) CurrentSnapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snapshotLocked()
}

// ResolveSnapshot validates and captures one immutable selection without
// installing it into package globals or retaining the Bindings lock. overrides
// is expressed in public sense (on means enabled). A nil map captures current
// process defaults. Snapshot and deltas are accepted for API compatibility; the
// complete overrides map remains authoritative and makes resolution bounded by
// the small optimization catalog.
func (b *Bindings) ResolveSnapshot(overrides map[string]bool, _ Snapshot, _ map[string]bool) (Selection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) > 128 {
		return Selection{}, fmt.Errorf("%s optimization catalog has %d entries, maximum is 128", b.arch, len(b.entries))
	}
	var bits [2]uint64
	matched := 0
	for index, entry := range b.entries {
		on := *entry.value
		if entry.inverted {
			on = !on
		}
		if override, ok := overrides[entry.definition.Name]; ok {
			on = override
			matched++
		}
		if on {
			bits[index/64] |= uint64(1) << uint(index%64)
		}
	}
	// Full RuntimeConfig maps are common. Resolve them in catalog order rather
	// than linearly searching the catalog once per override. Only invalid maps
	// need the name search to report the unknown entry.
	if matched != len(overrides) {
		for name := range overrides {
			if _, ok := b.indexOf(name); !ok && !omitted(b.arch, name) {
				return Selection{}, fmt.Errorf("unknown %s optimization %q", b.arch, name)
			}
		}
	}
	return Selection{bindings: b, bits: bits}, nil
}

func (b *Bindings) snapshotLocked() Snapshot {
	return Snapshot{bindings: b, revision: b.revision}
}

func (b *Bindings) infosLocked() []Info {
	result := make([]Info, len(b.entries))
	for index, entry := range b.entries {
		on := *entry.value
		if entry.inverted {
			on = !on
		}
		result[index] = info(entry.definition, on)
	}
	return result
}

// Set changes one binding's process default. RuntimeConfig selections should be
// preferred; this remains for low-level backend tests and compatibility.
func (b *Bindings) Set(name string, on bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	index, ok := b.indexOf(name)
	if !ok {
		return false
	}
	entry := b.entries[index]
	if entry.inverted {
		on = !on
	}
	if *entry.value != on {
		*entry.value = on
		b.revision++
	}
	return true
}

// Lease holds one installed optimization selection and its compile lock. It is
// deliberately a value rather than a restore closure: that keeps compiler
// inputs out of closure boxes under TinyGo's conservative collector.
type Lease struct {
	bindings *Bindings
	changed  int // -1 restores the complete selection
	active   bool
}

// Restore restores the process defaults and releases the compile lock.
func (l *Lease) Restore() {
	if l == nil || !l.active {
		return
	}
	l.active = false
	b := l.bindings
	if l.changed < 0 {
		for index, entry := range b.entries {
			*entry.value = b.before[index]
		}
	} else {
		for _, index := range b.changed[:l.changed] {
			*b.entries[index].value = b.before[index]
		}
	}
	b.mu.Unlock()
}

// Apply installs overrides for one compile and returns an explicit lease that
// restores the process defaults and releases the compile lock.
func (b *Bindings) Apply(overrides map[string]bool) (Lease, error) {
	return b.ApplySnapshot(overrides, Snapshot{}, nil)
}

// ApplySnapshot installs a captured selection for one compile. When revision
// still names the current process defaults and deltas exactly describe every
// difference in overrides, only deltas are installed. Otherwise overrides is
// validated and applied in full. The revision comparison, installation,
// compilation lease, and restoration all share the same lock, so a concurrent
// Set cannot invalidate the fast path.
func (b *Bindings) ApplySnapshot(overrides map[string]bool, snapshot Snapshot, deltas map[string]bool) (Lease, error) {
	b.mu.Lock()
	if overrides == nil {
		return Lease{bindings: b, active: true}, nil
	}
	if snapshot.bindings == b && snapshot.revision == b.revision && b.deltasMatchLocked(overrides, deltas) {
		changed := 0
		for name := range deltas {
			index, _ := b.indexOf(name)
			b.changed[changed] = index
			changed++
		}
		for _, index := range b.changed[:changed] {
			entry := b.entries[index]
			on := deltas[entry.definition.Name]
			if entry.inverted {
				on = !on
			}
			b.before[index] = *entry.value
			*entry.value = on
		}
		return Lease{bindings: b, changed: changed, active: true}, nil
	}
	for index, entry := range b.entries {
		b.before[index] = *entry.value
	}
	for name, on := range overrides {
		index, ok := b.indexOf(name)
		if !ok {
			if omitted(b.arch, name) {
				continue
			}
			for index, entry := range b.entries {
				*entry.value = b.before[index]
			}
			b.mu.Unlock()
			return Lease{}, fmt.Errorf("unknown %s optimization %q", b.arch, name)
		}
		entry := b.entries[index]
		if entry.inverted {
			on = !on
		}
		*entry.value = on
	}
	return Lease{bindings: b, changed: -1, active: true}, nil
}

// deltasMatchLocked reports whether deltas are a complete, coherent summary of
// overrides relative to the current process defaults. Unknown override names
// deliberately force the full path, which reports the validation error.
func (b *Bindings) deltasMatchLocked(overrides, deltas map[string]bool) bool {
	changed := 0
	for name, on := range overrides {
		index, ok := b.indexOf(name)
		if !ok {
			return false
		}
		current := *b.entries[index].value
		if b.entries[index].inverted {
			current = !current
		}
		if on == current {
			continue
		}
		changed++
		if delta, ok := deltas[name]; !ok || delta != on {
			return false
		}
	}
	return changed == len(deltas)
}

type nativeArchitectures uint8

const (
	nativeAMD64 nativeArchitectures = iota
	nativeARM64
	nativePair
	nativeAll
)

// Catalog entries share these read-only lists. Public definitions clone them.
var catalogArchitectures = [...][]string{
	nativeAMD64: {"amd64"},
	nativeARM64: {"arm64"},
	nativePair:  {"amd64", "arm64"},
	nativeAll:   {"amd64", "arm64", "riscv64"},
}

type catalogEntry struct {
	name, label, description string
	featureArch              string
	features                 uint64
	architectures            nativeArchitectures
	experimental, defaultOff bool
}

// Store constant records as data instead of building each Definition with
// separate initialization code. Keeping the table free of package initialization
// lets standalone launchers discard it when they only import the compiler types.
// Declaration order defines the public order.
var catalogEntries = [...]catalogEntry{
	{name: "bounds-facts", label: "Bounds facts", description: "straight-line bounds-check elision", architectures: nativePair},
	{name: "simd-superopt", label: "simd-superopt", architectures: nativePair, features: buildfeatures.SIMDBit},
	{name: "simd-reduction-borrow", label: "simd-reduction-borrow", architectures: nativeAMD64, features: buildfeatures.SIMDBit},
	{name: "interval-region-pins", label: "Interval-region pins", description: "reuse registers across bounded straight-line local lifetimes", architectures: nativePair},
	{name: "interval-control", label: "Regional control local cache", description: "reuse up to two integer local registers within call and control separated stretches", architectures: nativeAMD64},
	{name: "signed-quotient-direct", label: "Direct signed constant quotient", description: "keep signed i32 constant quotients in their result register and combine exact product shifts and sign correction", architectures: nativeARM64},
	{name: "magic-div", label: "Magic division", description: "lower constant integer division through multiply-high sequences", architectures: nativePair},
	{name: "shared-trap-body", label: "Shared trap bodies", description: "share repeated cold trap bodies in size-oriented code", architectures: nativePair},
	{name: "shared-adapters", label: "Shared adapters", description: "share byte-identical host adapters in size-oriented code", architectures: nativePair},
	{name: "st-flags", label: "Flags results", description: "keep comparison results in the flags register", architectures: nativePair},
	{name: "store8-flags", label: "Byte-store flags", description: "sink comparison results directly into byte stores", architectures: nativeAMD64},
	{name: "reg-merge", label: "Register merge", description: "keep block results in registers across joins", architectures: nativePair},
	{name: "tee-sink", label: "Tee sinking", description: "sink local.tee expressions into local registers", architectures: nativePair},
	{name: "unary-sink", label: "Unary sinking", description: "sink unary and conversion expressions in place", architectures: nativePair},
	{name: "counted-loop-body-align", label: "Counted loop body alignment", description: "align the hot target after a countdown loop zero-trip test", architectures: nativeARM64},
	{name: "register-call-borrowed-args", label: "Borrowed register-call arguments", description: "stage numeric pinned local arguments and immediate arithmetic without temporary copies", architectures: nativeARM64},
	{name: "three-op-sink", label: "Three-operand sinking", description: "sink binary operations into pinned locals", architectures: nativeARM64},
	{name: "olddest-rhs-sink", label: "Old destination reuse", description: "reuse an old destination register as the right operand", architectures: nativeARM64},
	{name: "branch-fold", label: "Branch folding", description: "fold branch pairs into one conditional branch", architectures: nativePair},
	{name: "conditional-bounds-fallthrough", label: "Conditional fallthrough bounds certificates", description: "retain bounds proofs for unchanged locals on conditional fallthrough paths", architectures: nativeARM64},
	{name: "repeated-native-address", label: "Repeated native address reuse", description: "reuse identical adjacent native memory-address calculations while preserving access order", architectures: nativeARM64},
	{name: "repeated-store-region", label: "Repeated store region reuse", description: "remove repeated proven-identical unshared-memory stores while preserving effects", architectures: nativeARM64},
	{name: "arithmetic-tee-branch", label: "Arithmetic tee backedge flags", description: "branch on an assigned arithmetic result using its proven surviving zero flag", architectures: nativeARM64},
	{name: "native-loop-const", label: "Native loop constant leases", description: "lease a register with proved dead exits for repeated loop materializations", architectures: nativeARM64},
	{name: "affine-bounds-loop", label: "Guarded affine scalar memory loops", description: "check bounded affine streams once while preserving the original arithmetic and checked fallback", architectures: nativeARM64},
	{name: "select-subtract", label: "Conditional integer subtraction", description: "share subtraction flags with a select of zero or the subtrahend", architectures: nativeARM64},
	{name: "float-memory-borrow", label: "Borrow float memory operands", description: "store read-only float operands directly and load directly into pinned float destinations", architectures: nativeARM64},
	{name: "byte-store-pair", label: "Adjacent byte stores", description: "combine two byte stores only when the complete access is proven within the memory minimum", architectures: nativeARM64},
	{name: "store-load-fwd", label: "Store/load forwarding", description: "forward native frame stores into later scalar loads", architectures: nativeARM64},
	{name: "memcopy-tail4", label: "memcopy-tail4", architectures: nativeARM64, features: buildfeatures.BulkMemoryOperationsBit},
	{name: "memcopy-qpairs", label: "memcopy-qpairs", architectures: nativeARM64, features: buildfeatures.BulkMemoryOperationsBit},
	{name: "uxtw-add", label: "Extended adds", description: "fold zero-extension into ADD UXTW", architectures: nativeARM64},
	{name: "select-group-guard", label: "Select group guard", description: "skip pure update groups when their unchanged mask has no selected bits", architectures: nativeARM64},
	{name: "dot-loop-vector", label: "Scalar loop vectorization", description: "vectorize proven integer reductions, min-plus updates and independent floating-point maps with guarded scalar fallback", architectures: nativeARM64},
	{name: "regional-condition-fusion", label: "Regional condition fusion", description: "keep pure branch conditions deferred while writing regional locals to their canonical homes", architectures: nativeARM64},
	{name: "pure-reduce-vector", label: "Pure integer reduction vectorization", description: "vectorize independent i32 expressions accumulated into i64 sums with exact scalar tails", architectures: nativeARM64},
	{name: "select-sink-pure-guard", label: "Pure integer select update guard", description: "skip bounded costly pure work when an integer select keeps its pinned local", architectures: nativeARM64},
	{name: "streaming-i32-reduction", label: "Streaming i32 product reductions", description: "reassociate proven wrapping product sums into a concrete streaming accumulator", architectures: nativeARM64},
	{name: "single-negative-move32", label: "Single-word negative i32 constants", description: "use one zero-extending W-MOVN for negative constants representable by its 16-bit immediate", architectures: nativeARM64},
	{name: "select-source-read", label: "Read-only select sink sources", description: "borrow protected integer select sources and share large constants with their comparison", architectures: nativeARM64},
	{name: "crowded-products", label: "Crowded integer product scheduling", description: "finish bounded pure products after pending effects to reduce live operand factors", architectures: nativeARM64},
	{name: "guarded-test-ccmp", label: "Guarded bit test conditional compare", description: "combine adjacent short-circuit bit tests when both continuations overwrite flags", architectures: nativeARM64},
	{name: "inverted-logical", label: "Inverted logical operands", description: "fold integer bitwise complements into logical register operations", architectures: nativeARM64},
	{name: "shifted-register-alu", label: "Shifted-register ALU", description: "fold constant shifts into integer ALU operands", architectures: nativeARM64},
	{name: "fp-immediate-const", label: "Float immediates", description: "materialize exact AArch64 floating-point immediates directly", architectures: nativeARM64},
	{name: "scoped-loop-float-const", label: "Scoped loop float constants", description: "cache scalar float constants inside bounded call-free loops", architectures: nativeARM64},
	{name: "fp-literal-pool", label: "Float literal pool", description: "load non-immediate scalar float constants from a deduplicated PC-relative pool", architectures: nativeARM64},
	{name: "value-facts", label: "Value facts", description: "propagate bounded upper-zero and boolean provenance", architectures: nativePair},
	{name: "load-pair", label: "Adjacent load pairs", description: "combine adjacent full-width scalar loads from one local address into LDP", architectures: nativeARM64},
	{name: "entry-param-pairs", label: "Entry parameter pairs", description: "pair adjacent serialized wrapper parameter homes in function prologues", architectures: nativeARM64},
	{name: "entry-zero-pairs", label: "Entry zero pairs", description: "pair adjacent declared-local zero stores in function prologues", architectures: nativeARM64},
	{name: "entry-arg-pins", label: "Entry argument pins", description: "keep entry arguments in incoming registers", architectures: nativePair},
	{name: "x8-pin", label: "X8 scratch pin", description: "pin a scratch value in call-free functions", architectures: nativeARM64},
	{name: "ext-fp-pins", label: "Extended float pins", description: "use the larger floating-point register pool", architectures: nativePair},
	{name: "merge-next-use", label: "Merge next-use", description: "keep dead forward-merge locals lazy with bounded post-merge lookahead", architectures: nativeARM64},
	{name: "callfree-loop-cold-exit", label: "Call-free loop cold exits", description: "move local reconciliation from a call-free loop's hot conditional fall-through to its taken exit edge", architectures: nativePair},
	{name: "weighted-scalar-merge", label: "Weighted scalar merges", description: "reserve the canonical merge register for loop-hot scalar result joins", architectures: nativeARM64},
	{name: "tree-order", label: "Valent tree ordering", description: "schedule bounded commutative trees by register need", architectures: nativeAMD64},
	{name: "assoc-tree", label: "Associative tree cover", description: "cover high-pressure bounded associative trees with one accumulator", architectures: nativeAMD64},
	{name: "bmi2-rorx", label: "BMI2 rotates", description: "use non-destructive immediate rotates on BMI2 hosts", architectures: nativeAMD64, experimental: true, defaultOff: true},
	{name: "avx512-ternary", label: "avx512-ternary", architectures: nativeAMD64, features: buildfeatures.SIMDBit},
	{name: "avx512-vrotate", label: "avx512-vrotate", architectures: nativeAMD64, features: buildfeatures.SIMDBit},
	{name: "callfree-loop-reg-state", label: "Call-free loop register state", description: "keep written pinned locals register-resident across simple call-free loop backedges", architectures: nativeAMD64},
	{name: "leaf-scratch-pins", label: "Leaf scratch pins", description: "pin scratch values in leaf functions", architectures: nativeARM64},
	{name: "leaf-scratch-memsize", label: "Leaf scratch memory size", description: "cache memory size in backend scratch for straight-line regional leaves", architectures: nativeARM64},
	{name: "loop-trap-cell", label: "Loop trap cell", description: "cache the stable cancellation cell across call-free loops", architectures: nativeARM64},
	{name: "atomic-notify-empty", label: "atomic-notify-empty", architectures: nativePair, features: buildfeatures.ThreadsBit},
	{name: "atomic-wait-zero-timeout", label: "atomic-wait-zero-timeout", architectures: nativePair, features: buildfeatures.ThreadsBit},
	{name: "shared-memory-base", label: "shared-memory-base", architectures: nativePair, features: buildfeatures.ThreadsBit},
	{name: "atomic-lse", label: "atomic-lse", architectures: nativeARM64, features: buildfeatures.ThreadsBit},
	{name: "atomic-static-bounds", label: "atomic-static-bounds", architectures: nativePair, features: buildfeatures.ThreadsBit},
	{name: "counted-loop-latch", label: "Counted loop latch", description: "fold exact top-tested i32 countdown loops into an architecture-safe latch", architectures: nativePair},
	{name: "straight-line-block", label: "Straight-line blocks", description: "retain operand residency through bounded fallthrough blocks and fully spliced private callees", architectures: nativePair},
	{name: "loop-local-params", label: "Loop parameter locals", description: "transfer bounded integer loop parameters directly into pinned local homes", architectures: nativePair},
	{name: "scalar-loop-unroll", label: "Scalar loop unrolling", description: "replicate bounded register-only integer loops and streamline short pinned-local loop backedges", architectures: nativePair},
	{name: "cold-tuple-loop-exit", label: "Cold integer tuple loop exits", description: "defer local-only tuple exit arms so loop continuations fall through", architectures: nativeARM64},
	{name: "guarded-div-rem", label: "Guarded integer division", description: "reuse an immediately preceding local zero guard for one integer division or remainder", architectures: nativeARM64},
	{name: "tuple-if-select", label: "Integer pair selection", description: "select pinned integer pair operands without a structured merge", architectures: nativePair},
	{name: "pure-integer-if-select", label: "Pure integer if selection", description: "select bounded nontrapping integer expressions without a structured merge", architectures: nativePair},
	{name: "inline-integer-tuples", label: "Integer tuple inlining", description: "inline bounded numeric tuple leaves with structured control and private local homes", architectures: nativeARM64},
	{name: "bulk-fill-vector", label: "bulk-fill-vector", architectures: nativePair, features: buildfeatures.BulkMemoryOperationsBit},
	{name: "bulk-copy-vector", label: "bulk-copy-vector", architectures: nativePair, features: buildfeatures.BulkMemoryOperationsBit},
	{name: "bulk-memory-loops", label: "bulk-memory-loops", architectures: nativePair, features: buildfeatures.BulkMemoryOperationsBit},
	{name: "bulk-fill-copy-forward", label: "bulk-fill-copy-forward", architectures: nativePair, features: buildfeatures.BulkMemoryOperationsBit},
	{name: "byte-reduce-loop", label: "Read-only byte reductions", description: "guard complete unsigned byte-reduction ranges and lower modular byte sums in native groups", architectures: nativePair},
	{name: "simd-store-load-address", label: "simd-store-load-address", architectures: nativePair, features: buildfeatures.SIMDBit},
	{name: "simd-load-extract", label: "simd-load-extract", architectures: nativePair, features: buildfeatures.SIMDBit},
	{name: "memory-min-bounds", label: "Declared minimum memory bounds", description: "prove bounded integer address expressions fit the selected memory minimum", architectures: nativePair},
	{name: "linear-integer-tree", label: "Repeated integer linear terms", description: "collect repeated pure local terms in bounded wrapping i32 expressions", architectures: nativePair},
	{name: "linear-sum-loop", label: "Linear sum loops", description: "hoist bounds and split exact i64 memory reductions across four accumulators", architectures: nativePair},
	{name: "prepared-direct-entry", label: "Direct prepared entry", description: "enter compiler-proved integer functions through the register ABI", architectures: nativeAll},
	{name: "prepared-light-entry", label: "Light prepared entry", description: "use caller-clobber proofs to select a smaller native entry thunk", architectures: nativeARM64},
	{name: "prepared-bounded-entry", label: "Bounded prepared entry", description: "keep compiler-proven bounded native invocations on the Go scheduler", architectures: nativeAll},
	{name: "native-leaf-host", label: "Native scalar host leaves", description: "lower proven pure Go scalar callbacks into native import trampolines", architectures: nativeAll},
	{name: "direct-go-host-import", label: "Direct Go host imports", description: "bypass cross-instance marshaling for bound Go imports in bounded numeric functions", architectures: nativePair},
	{name: "wide-loop-int-const", label: "Wide loop integer constants", description: "keep repeatedly materialized non-imm32 i64 loop constants in otherwise-idle registers", architectures: nativeAMD64},
	{name: "compact-loop-align32", label: "Compact loop alignment", description: "place small loop functions at 32-byte fetch-block boundaries", architectures: nativeAMD64},
	{name: "interval-scratch-lease", label: "Regional scratch leasing", description: "lend idle RDX to proved straight-line integer local regions", architectures: nativeAMD64},
	{name: "interval-r8-lease", label: "Regional R8 leasing", description: "lend R8 to signal-bounded straight-line integer regions after excluding fixed-register lowerings", architectures: nativeAMD64},
	{name: "interval-i64-weight", label: "Regional i64 weighting", description: "price packed 64-bit local reloads more heavily in regional eviction", architectures: nativeAMD64},
	{name: "interval-next-use", label: "Regional next-use eviction", description: "use a compact local-event tape to evict the farthest local version and elide dead stores", architectures: nativePair},
	{name: "memsize-regional-lease", label: "Regional memory-size leasing", description: "lend the memory-size register to proved straight-line local regions", architectures: nativeAMD64},
	{name: "module-global-regional-lease", label: "Regional module-global leasing", description: "lend an unreferenced module-global register to proved straight-line local regions", architectures: nativeAMD64},
	{name: "loop-memory-base", label: "Loop memory page bases", description: "hoist repeated linear-memory page bases into idle loop-constant registers", architectures: nativeARM64},
	{name: "layout-padding-skip", label: "Skip layout padding", description: "branch over long alignment-preserving padding runs", architectures: nativeARM64},
	{name: "loop-int-const", label: "Loop integer constants", description: "keep costly loop-invariant integer constants in otherwise-idle registers", architectures: nativeARM64},
	{name: "interval-call-regions", label: "Local regions around calls", description: "cache local integer values in bounded regions that canonicalize before calls and fixed-scratch helpers", architectures: nativeARM64},
	{name: "scoped-loop-int-const", label: "Scoped loop integer constants", description: "lease invariant integer constants only inside proven call-free loops, including functions that call elsewhere", architectures: nativeARM64},
	{name: "wide-reuse-pins", label: "Wide repeated local pins", description: "retain bounded integer locals whose loop-weighted reads exceed definitions", architectures: nativeARM64},
	{name: "regional-memory-read", label: "Regional memory address reads", description: "borrow immediate regional memory addresses while pinning their leases against eviction", architectures: nativeARM64},
	{name: "br-table-branch-vector", label: "Branch table vectors", description: "dispatch small unique zero-argument tables directly through executable branches", architectures: nativeARM64},
	{name: "bounded-branch-index", label: "Bounded branch indices", description: "omit branch-vector bounds tests for direct i32 masks and constants proved in range", architectures: nativeARM64},
	{name: "borrowed-div-rem", label: "Read-only division operands", description: "read concrete pinned integer division and remainder operands without copying", architectures: nativePair},
	{name: "bounded-signed-quotient", label: "Bounded signed quotient", description: "skip exact i32 division when a subsequent signed clamp saturates", architectures: nativeARM64},
	{name: "taken-zero-branch", label: "Known zero branch targets", description: "thread direct zero branches through tests implied by their taken edge", architectures: nativeARM64},
	{name: "common-exit-compare", label: "Common-exit compares", description: "combine consecutive integer comparison branches sharing an exit", architectures: nativeARM64},
	{name: "guarded-boolean-xor", label: "Guarded boolean XOR", description: "if-convert pure bounded integer boolean XOR regions after exact clobber proofs", architectures: nativeARM64},
	{name: "cset-lineage", label: "Boolean comparison flags", description: "reuse unchanged flags through exact CSET boolean copies for bounded integer selects", architectures: nativeARM64},
	{name: "integer-select-local-read", label: "Integer select local reads", description: "borrow protected integer operands for materialized local select sinks", architectures: nativeARM64},
	{name: "dominated-indexed-base", label: "Dominated indexed addresses", description: "reuse indexed addresses through proven bounded direct branch regions", architectures: nativeARM64},
	{name: "dominated-indexed-key", label: "Equivalent indexed addresses", description: "merge bounded address facts by their exact register inputs", architectures: nativeARM64},
	{name: "ordinary-shifted-immediate", label: "Shifted integer displacements", description: "use exact shifted immediates for read-only ordinary displacement arithmetic", architectures: nativeARM64},
	{name: "indexed-base-reuse", label: "Adjacent indexed base reuse", description: "reuse an unchanged base-plus-index address across adjacent memory accesses", architectures: nativeARM64},
	{name: "convert-read", label: "Direct conversion reads", description: "read pinned integer conversion inputs without a temporary copy", architectures: nativeARM64},
	{name: "cold-call-local-pins", label: "Cold-call local pins", description: "pin hot locals when every direct local call is outside loops", architectures: nativeARM64},
	{name: "vex-float-mem", label: "VEX memory operands", description: "fold scalar float loads into AVX operations", architectures: nativeAMD64},
	{name: "direct-int-branch-merge", label: "Direct integer branch results", description: "deliver unconditional integer branch results directly to their merge register", architectures: nativeAMD64},
	{name: "wide-local-pins", label: "Bounded wide-function pins", description: "keep up to two locals per register bank in bounded numeric functions with loops", architectures: nativeAMD64},
	{name: "memory-compare-immediate", label: "Integer memory comparisons", description: "compare deferred integer loads with constants using the exact access width", architectures: nativeAMD64},
	{name: "float-store-borrow", label: "Read-only floating-point stores", description: "store pinned scalar floating-point locals and cached constants without scratch copies", architectures: nativeAMD64},
	{name: "float-frame-mem", label: "Float frame operands", description: "fold scalar frame locals and spills into exact-width SSE or AVX arithmetic", architectures: nativeAMD64},
	{name: "multi-bounds-cert", label: "Multiple bounds proofs", description: "retain independent proofs for interleaved arrays", architectures: nativePair},
	{name: "addr-zext-elim", label: "Memory32 address cleanup", description: "skip redundant zero-extension of proven-clean memory32 addresses", architectures: nativeAMD64},
	{name: "canonical-i32", label: "Canonical i32 carriers", description: "reuse canonical regional-local and spill carriers for repeated 32-bit uses", architectures: nativeAMD64},
	{name: "commute-fixed-self-update", label: "Fixed-register self updates", description: "retain proved-safe fixed-register accumulators across commutative update trees", architectures: nativeAMD64},
	{name: "immutable-table", label: "Immutable tables", description: "specialize calls through never-written tables", architectures: nativePair},
	{name: "immutable-table-type", label: "Immutable table types", description: "skip redundant immutable-table type checks", architectures: nativePair},
	{name: "inline-callfree", label: "Call-free inline hints", description: "prioritize call-free functions for inlining", architectures: nativePair},
	{name: "store-forward", label: "Store forwarding", description: "forward straight-line stores into loads", architectures: nativePair},
	{name: "frame-elide", label: "Frame elision", description: "omit frames for small single-result functions", architectures: nativeAMD64},
	{name: "compact-i32-frame", label: "Compact i32 frames", description: "pack i32 locals in straight-line call-free functions", architectures: nativeAMD64},
	{name: "local-slot-order", label: "Symbolic local slot packing", description: "move exact referenced local homes into zero-reference compact slots", architectures: nativeAMD64},
	{name: "commute-self-update", label: "Commute self-updates", description: "make non-fixed destinations accumulate commutative self-update expressions in place", architectures: nativeAMD64},
	{name: "i64-mask32", label: "Low-32 mask lowering", description: "lower i64 low-32 masks to zero-extending 32-bit ANDs", architectures: nativeAMD64},
	{name: "accumulator-immediate", label: "Accumulator immediates", description: "use ModRM-free RAX/EAX imm32 encodings in size objectives", architectures: nativeAMD64},
	{name: "frame-elide-reghomed", label: "Register-homed frames", description: "omit frames when locals remain in registers", architectures: nativeARM64},
	{name: "small-frame", label: "Small frames", description: "use compact stack adjustment forms", architectures: nativeARM64},
	{name: "zero-branch", label: "Zero branches", description: "select direct zero-test branches when flags are not live", architectures: nativeARM64},
	{name: "mul-add-fuse", label: "Multiply-add fusion", description: "fuse multiply-add and multiply-subtract expressions", architectures: nativeARM64},
	{name: "tail-self-loop", label: "tail-self-loop", architectures: nativePair, features: buildfeatures.TailCallBit},
	{name: "tail-loop-inline", label: "tail-loop-inline", architectures: nativePair, features: buildfeatures.TailCallBit},
	{name: "entry-init-elision", label: "Entry initialization elision", description: "skip initialization of locals overwritten before their first read", architectures: nativeARM64},
	{name: "v128-direct-results", label: "v128-direct-results", architectures: nativeARM64, features: buildfeatures.SIMDBit},
	{name: "dead-gc-new", label: "dead-gc-new", architectures: nativeAMD64, features: buildfeatures.GCBit},
	{name: "gc-native-alloc", label: "gc-native-alloc", architectures: nativeAMD64, features: buildfeatures.GCBit},
	{name: "v128-const-cache", label: "Vector constant cache", description: "reserve repeated vector constants and target-specific masks", architectures: nativePair, features: buildfeatures.SIMDBit, featureArch: "arm64"},
	{name: "v128-pins", label: "v128-pins", architectures: nativePair, features: buildfeatures.SIMDBit},
	{name: "reg-abi", label: "Register ABI", description: "use Wago's internal register calling convention", architectures: nativeAll},
	{name: "direct-reference-abi", label: "direct-reference-abi", architectures: nativeAMD64, features: buildfeatures.ReferenceTypesBit},
	{name: "inline", label: "Inlining", description: "inline eligible callees", architectures: nativePair},
	{name: "stack-fence", label: "Stack fence", description: "emit the stack-overflow guard fence", architectures: nativePair},
	{name: "stack-reg", label: "Stack register", description: "keep the guest stack pointer in a register", architectures: nativePair},
}

var (
	catalogOnce sync.Once
	catalog     []Definition
)

func definitions() []Definition {
	catalogOnce.Do(func() { catalog = newCatalog() })
	return catalog
}

func newCatalog() []Definition {
	definitions := make([]Definition, len(catalogEntries))
	for i, entry := range catalogEntries {
		definitions[i] = Definition{
			Name: entry.name, Label: entry.label, Description: entry.description,
			Default: !entry.defaultOff, Experimental: entry.experimental,
			Architectures:    catalogArchitectures[entry.architectures],
			requiredFeatures: entry.features, requiredArch: entry.featureArch,
		}
	}
	addProposalDescriptions(definitions)
	return definitions
}

func (definition Definition) availableInBuild(arch string) bool {
	return definition.requiredArch != "" && definition.requiredArch != arch || definition.requiredFeatures&^buildfeatures.Enabled == 0
}

// omitted reports whether the target recognizes a portable optimization whose
// implementation was excluded from this build.
func omitted(arch, name string) bool {
	if !buildfeatures.Custom {
		return false
	}
	for _, definition := range definitions() {
		if definition.Name == name && supports(definition, arch) {
			return !definition.availableInBuild(arch)
		}
	}
	return false
}

// ForArch returns every optimization registered for arch in catalog order.
func ForArch(arch string) []Definition {
	catalog := definitions()
	result := make([]Definition, 0, len(catalog))
	for _, definition := range catalog {
		if supports(definition, arch) {
			result = append(result, clone(definition))
		}
	}
	return result
}

// All returns every registered optimization once in catalog order.
func All() []Definition {
	catalog := definitions()
	result := make([]Definition, len(catalog))
	for index, definition := range catalog {
		result[index] = clone(definition)
	}
	return result
}

// InfosForArch returns built-in selection values for an architecture.
func InfosForArch(arch string) []Info {
	definitions := ForArch(arch)
	result := make([]Info, len(definitions))
	for index, definition := range definitions {
		if !definition.availableInBuild(arch) {
			definition.Label = definition.Name
			definition.Description = ""
		}
		result[index] = info(definition, definition.Default)
	}
	return result
}

// Infos returns every definition once with its built-in selection value.
func Infos() []Info {
	definitions := All()
	result := make([]Info, len(definitions))
	for index, definition := range definitions {
		result[index] = info(definition, definition.Default)
	}
	return result
}

// Lookup returns a registered optimization for arch.
func Lookup(arch, name string) (Definition, bool) {
	for _, definition := range definitions() {
		if definition.Name == name && supports(definition, arch) {
			return clone(definition), true
		}
	}
	return Definition{}, false
}

// Exists reports whether name is registered for arch without copying the
// public Definition's mutable architecture slice.
func Exists(arch, name string) bool {
	for _, definition := range definitions() {
		if definition.Name == name && supports(definition, arch) {
			return true
		}
	}
	return false
}

func supports(definition Definition, arch string) bool {
	for _, candidate := range definition.Architectures {
		if candidate == arch {
			return true
		}
	}
	return false
}

func clone(definition Definition) Definition {
	definition.Architectures = append([]string(nil), definition.Architectures...)
	return definition
}

func info(definition Definition, on bool) Info {
	return Info{
		Name: definition.Name, Label: definition.Label, Desc: definition.Description,
		On: on, Default: definition.Default, Experimental: definition.Experimental,
	}
}
