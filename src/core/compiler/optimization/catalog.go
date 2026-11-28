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

// All entries share these arrays. Public definitions clone the architecture list.
var (
	amd64Architectures = [...]string{"amd64"}
	arm64Architectures = [...]string{"arm64"}
	pairArchitectures  = [...]string{"amd64", "arm64"}
	allArchitectures   = [...]string{"amd64", "arm64", "riscv64"}
	catalogOnce        sync.Once
)

// Keep the literal static so the compiler can store it as data.
var catalog = []Definition{
	{Name: "bounds-facts", Label: "Bounds facts", Description: "straight-line bounds-check elision", Architectures: pairArchitectures[:], Default: true},
	{Name: "simd-superopt", Label: "simd-superopt", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.SIMDBit, Default: true},
	{Name: "simd-reduction-borrow", Label: "simd-reduction-borrow", Architectures: amd64Architectures[:], requiredFeatures: buildfeatures.SIMDBit, Default: true},
	{Name: "interval-region-pins", Label: "Interval-region pins", Description: "reuse registers across bounded straight-line local lifetimes", Architectures: pairArchitectures[:], Default: true},
	{Name: "interval-control", Label: "Regional control local cache", Description: "reuse up to two integer local registers within call and control separated stretches", Architectures: amd64Architectures[:], Default: true},
	{Name: "signed-quotient-direct", Label: "Direct signed constant quotient", Description: "keep signed i32 constant quotients in their result register and combine exact product shifts and sign correction", Architectures: arm64Architectures[:], Default: true},
	{Name: "magic-div", Label: "Magic division", Description: "lower constant integer division through multiply-high sequences", Architectures: pairArchitectures[:], Default: true},
	{Name: "shared-trap-body", Label: "Shared trap bodies", Description: "share repeated cold trap bodies in size-oriented code", Architectures: pairArchitectures[:], Default: true},
	{Name: "shared-adapters", Label: "Shared adapters", Description: "share byte-identical host adapters in size-oriented code", Architectures: pairArchitectures[:], Default: true},
	{Name: "st-flags", Label: "Flags results", Description: "keep comparison results in the flags register", Architectures: pairArchitectures[:], Default: true},
	{Name: "store8-flags", Label: "Byte-store flags", Description: "sink comparison results directly into byte stores", Architectures: amd64Architectures[:], Default: true},
	{Name: "reg-merge", Label: "Register merge", Description: "keep block results in registers across joins", Architectures: pairArchitectures[:], Default: true},
	{Name: "tee-sink", Label: "Tee sinking", Description: "sink local.tee expressions into local registers", Architectures: pairArchitectures[:], Default: true},
	{Name: "unary-sink", Label: "Unary sinking", Description: "sink unary and conversion expressions in place", Architectures: pairArchitectures[:], Default: true},
	{Name: "counted-loop-body-align", Label: "Counted loop body alignment", Description: "align the hot target after a countdown loop zero-trip test", Architectures: arm64Architectures[:], Default: true},
	{Name: "register-call-borrowed-args", Label: "Borrowed register-call arguments", Description: "stage numeric pinned local arguments and immediate arithmetic without temporary copies", Architectures: arm64Architectures[:], Default: true},
	{Name: "three-op-sink", Label: "Three-operand sinking", Description: "sink binary operations into pinned locals", Architectures: arm64Architectures[:], Default: true},
	{Name: "olddest-rhs-sink", Label: "Old destination reuse", Description: "reuse an old destination register as the right operand", Architectures: arm64Architectures[:], Default: true},
	{Name: "branch-fold", Label: "Branch folding", Description: "fold branch pairs into one conditional branch", Architectures: pairArchitectures[:], Default: true},
	{Name: "conditional-bounds-fallthrough", Label: "Conditional fallthrough bounds certificates", Description: "retain bounds proofs for unchanged locals on conditional fallthrough paths", Architectures: arm64Architectures[:], Default: true},
	{Name: "repeated-native-address", Label: "Repeated native address reuse", Description: "reuse identical adjacent native memory-address calculations while preserving access order", Architectures: arm64Architectures[:], Default: true},
	{Name: "repeated-store-region", Label: "Repeated store region reuse", Description: "remove repeated proven-identical unshared-memory stores while preserving effects", Architectures: arm64Architectures[:], Default: true},
	{Name: "arithmetic-tee-branch", Label: "Arithmetic tee backedge flags", Description: "branch on an assigned arithmetic result using its proven surviving zero flag", Architectures: arm64Architectures[:], Default: true},
	{Name: "native-loop-const", Label: "Native loop constant leases", Description: "lease a register with proved dead exits for repeated loop materializations", Architectures: arm64Architectures[:], Default: true},
	{Name: "affine-bounds-loop", Label: "Guarded affine scalar memory loops", Description: "check bounded affine streams once while preserving the original arithmetic and checked fallback", Architectures: arm64Architectures[:], Default: true},
	{Name: "select-subtract", Label: "Conditional integer subtraction", Description: "share subtraction flags with a select of zero or the subtrahend", Architectures: arm64Architectures[:], Default: true},
	{Name: "float-memory-borrow", Label: "Borrow float memory operands", Description: "store read-only float operands directly and load directly into pinned float destinations", Architectures: arm64Architectures[:], Default: true},
	{Name: "byte-store-pair", Label: "Adjacent byte stores", Description: "combine two byte stores only when the complete access is proven within the memory minimum", Architectures: arm64Architectures[:], Default: true},
	{Name: "store-load-fwd", Label: "Store/load forwarding", Description: "forward native frame stores into later scalar loads", Architectures: arm64Architectures[:], Default: true},
	{Name: "memcopy-tail4", Label: "memcopy-tail4", Architectures: arm64Architectures[:], requiredFeatures: buildfeatures.BulkMemoryOperationsBit, Default: true},
	{Name: "memcopy-qpairs", Label: "memcopy-qpairs", Architectures: arm64Architectures[:], requiredFeatures: buildfeatures.BulkMemoryOperationsBit, Default: true},
	{Name: "uxtw-add", Label: "Extended adds", Description: "fold zero-extension into ADD UXTW", Architectures: arm64Architectures[:], Default: true},
	{Name: "select-group-guard", Label: "Select group guard", Description: "skip pure update groups when their unchanged mask has no selected bits", Architectures: arm64Architectures[:], Default: true},
	{Name: "dot-loop-vector", Label: "Scalar loop vectorization", Description: "vectorize proven integer reductions, min-plus updates and independent floating-point maps with guarded scalar fallback", Architectures: arm64Architectures[:], Default: true},
	{Name: "regional-condition-fusion", Label: "Regional condition fusion", Description: "keep pure branch conditions deferred while writing regional locals to their canonical homes", Architectures: arm64Architectures[:], Default: true},
	{Name: "pure-reduce-vector", Label: "Pure integer reduction vectorization", Description: "vectorize independent i32 expressions accumulated into i64 sums with exact scalar tails", Architectures: arm64Architectures[:], Default: true},
	{Name: "select-sink-pure-guard", Label: "Pure integer select update guard", Description: "skip bounded costly pure work when an integer select keeps its pinned local", Architectures: arm64Architectures[:], Default: true},
	{Name: "streaming-i32-reduction", Label: "Streaming i32 product reductions", Description: "reassociate proven wrapping product sums into a concrete streaming accumulator", Architectures: arm64Architectures[:], Default: true},
	{Name: "single-negative-move32", Label: "Single-word negative i32 constants", Description: "use one zero-extending W-MOVN for negative constants representable by its 16-bit immediate", Architectures: arm64Architectures[:], Default: true},
	{Name: "select-source-read", Label: "Read-only select sink sources", Description: "borrow protected integer select sources and share large constants with their comparison", Architectures: arm64Architectures[:], Default: true},
	{Name: "crowded-products", Label: "Crowded integer product scheduling", Description: "finish bounded pure products after pending effects to reduce live operand factors", Architectures: arm64Architectures[:], Default: true},
	{Name: "guarded-test-ccmp", Label: "Guarded bit test conditional compare", Description: "combine adjacent short-circuit bit tests when both continuations overwrite flags", Architectures: arm64Architectures[:], Default: true},
	{Name: "inverted-logical", Label: "Inverted logical operands", Description: "fold integer bitwise complements into logical register operations", Architectures: arm64Architectures[:], Default: true},
	{Name: "shifted-register-alu", Label: "Shifted-register ALU", Description: "fold constant shifts into integer ALU operands", Architectures: arm64Architectures[:], Default: true},
	{Name: "fp-immediate-const", Label: "Float immediates", Description: "materialize exact AArch64 floating-point immediates directly", Architectures: arm64Architectures[:], Default: true},
	{Name: "scoped-loop-float-const", Label: "Scoped loop float constants", Description: "cache scalar float constants inside bounded call-free loops", Architectures: arm64Architectures[:], Default: true},
	{Name: "fp-literal-pool", Label: "Float literal pool", Description: "load non-immediate scalar float constants from a deduplicated PC-relative pool", Architectures: arm64Architectures[:], Default: true},
	{Name: "value-facts", Label: "Value facts", Description: "propagate bounded upper-zero and boolean provenance", Architectures: pairArchitectures[:], Default: true},
	{Name: "load-pair", Label: "Adjacent load pairs", Description: "combine adjacent full-width scalar loads from one local address into LDP", Architectures: arm64Architectures[:], Default: true},
	{Name: "entry-param-pairs", Label: "Entry parameter pairs", Description: "pair adjacent serialized wrapper parameter homes in function prologues", Architectures: arm64Architectures[:], Default: true},
	{Name: "entry-zero-pairs", Label: "Entry zero pairs", Description: "pair adjacent declared-local zero stores in function prologues", Architectures: arm64Architectures[:], Default: true},
	{Name: "entry-arg-pins", Label: "Entry argument pins", Description: "keep entry arguments in incoming registers", Architectures: pairArchitectures[:], Default: true},
	{Name: "x8-pin", Label: "X8 scratch pin", Description: "pin a scratch value in call-free functions", Architectures: arm64Architectures[:], Default: true},
	{Name: "ext-fp-pins", Label: "Extended float pins", Description: "use the larger floating-point register pool", Architectures: pairArchitectures[:], Default: true},
	{Name: "merge-next-use", Label: "Merge next-use", Description: "keep dead forward-merge locals lazy with bounded post-merge lookahead", Architectures: arm64Architectures[:], Default: true},
	{Name: "callfree-loop-cold-exit", Label: "Call-free loop cold exits", Description: "move local reconciliation from a call-free loop's hot conditional fall-through to its taken exit edge", Architectures: pairArchitectures[:], Default: true},
	{Name: "weighted-scalar-merge", Label: "Weighted scalar merges", Description: "reserve the canonical merge register for loop-hot scalar result joins", Architectures: arm64Architectures[:], Default: true},
	{Name: "tree-order", Label: "Valent tree ordering", Description: "schedule bounded commutative trees by register need", Architectures: amd64Architectures[:], Default: true},
	{Name: "assoc-tree", Label: "Associative tree cover", Description: "cover high-pressure bounded associative trees with one accumulator", Architectures: amd64Architectures[:], Default: true},
	{Name: "bmi2-rorx", Label: "BMI2 rotates", Description: "use non-destructive immediate rotates on BMI2 hosts", Architectures: amd64Architectures[:], Experimental: true},
	{Name: "avx512-ternary", Label: "avx512-ternary", Architectures: amd64Architectures[:], requiredFeatures: buildfeatures.SIMDBit, Default: true},
	{Name: "avx512-vrotate", Label: "avx512-vrotate", Architectures: amd64Architectures[:], requiredFeatures: buildfeatures.SIMDBit, Default: true},
	{Name: "callfree-loop-reg-state", Label: "Call-free loop register state", Description: "keep written pinned locals register-resident across simple call-free loop backedges", Architectures: amd64Architectures[:], Default: true},
	{Name: "leaf-scratch-pins", Label: "Leaf scratch pins", Description: "pin scratch values in leaf functions", Architectures: arm64Architectures[:], Default: true},
	{Name: "leaf-scratch-memsize", Label: "Leaf scratch memory size", Description: "cache memory size in backend scratch for straight-line regional leaves", Architectures: arm64Architectures[:], Default: true},
	{Name: "loop-trap-cell", Label: "Loop trap cell", Description: "cache the stable cancellation cell across call-free loops", Architectures: arm64Architectures[:], Default: true},
	{Name: "atomic-notify-empty", Label: "atomic-notify-empty", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.ThreadsBit, Default: true},
	{Name: "atomic-wait-zero-timeout", Label: "atomic-wait-zero-timeout", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.ThreadsBit, Default: true},
	{Name: "shared-memory-base", Label: "shared-memory-base", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.ThreadsBit, Default: true},
	{Name: "atomic-lse", Label: "atomic-lse", Architectures: arm64Architectures[:], requiredFeatures: buildfeatures.ThreadsBit, Default: true},
	{Name: "atomic-static-bounds", Label: "atomic-static-bounds", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.ThreadsBit, Default: true},
	{Name: "counted-loop-latch", Label: "Counted loop latch", Description: "fold exact top-tested i32 countdown loops into an architecture-safe latch", Architectures: pairArchitectures[:], Default: true},
	{Name: "straight-line-block", Label: "Straight-line blocks", Description: "retain operand residency through bounded fallthrough blocks and fully spliced private callees", Architectures: pairArchitectures[:], Default: true},
	{Name: "loop-local-params", Label: "Loop parameter locals", Description: "transfer bounded integer loop parameters directly into pinned local homes", Architectures: pairArchitectures[:], Default: true},
	{Name: "scalar-loop-unroll", Label: "Scalar loop unrolling", Description: "replicate bounded register-only integer loops and streamline short pinned-local loop backedges", Architectures: pairArchitectures[:], Default: true},
	{Name: "cold-tuple-loop-exit", Label: "Cold integer tuple loop exits", Description: "defer local-only tuple exit arms so loop continuations fall through", Architectures: arm64Architectures[:], Default: true},
	{Name: "guarded-div-rem", Label: "Guarded integer division", Description: "reuse an immediately preceding local zero guard for one integer division or remainder", Architectures: arm64Architectures[:], Default: true},
	{Name: "tuple-if-select", Label: "Integer pair selection", Description: "select pinned integer pair operands without a structured merge", Architectures: pairArchitectures[:], Default: true},
	{Name: "pure-integer-if-select", Label: "Pure integer if selection", Description: "select bounded nontrapping integer expressions without a structured merge", Architectures: pairArchitectures[:], Default: true},
	{Name: "inline-integer-tuples", Label: "Integer tuple inlining", Description: "inline bounded numeric tuple leaves with structured control and private local homes", Architectures: arm64Architectures[:], Default: true},
	{Name: "bulk-fill-vector", Label: "bulk-fill-vector", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.BulkMemoryOperationsBit, Default: true},
	{Name: "bulk-copy-vector", Label: "bulk-copy-vector", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.BulkMemoryOperationsBit, Default: true},
	{Name: "bulk-memory-loops", Label: "bulk-memory-loops", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.BulkMemoryOperationsBit, Default: true},
	{Name: "bulk-fill-copy-forward", Label: "bulk-fill-copy-forward", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.BulkMemoryOperationsBit, Default: true},
	{Name: "byte-reduce-loop", Label: "Read-only byte reductions", Description: "guard complete unsigned byte-reduction ranges and lower modular byte sums in native groups", Architectures: pairArchitectures[:], Default: true},
	{Name: "simd-store-load-address", Label: "simd-store-load-address", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.SIMDBit, Default: true},
	{Name: "simd-load-extract", Label: "simd-load-extract", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.SIMDBit, Default: true},
	{Name: "memory-min-bounds", Label: "Declared minimum memory bounds", Description: "prove bounded integer address expressions fit the selected memory minimum", Architectures: pairArchitectures[:], Default: true},
	{Name: "linear-integer-tree", Label: "Repeated integer linear terms", Description: "collect repeated pure local terms in bounded wrapping i32 expressions", Architectures: pairArchitectures[:], Default: true},
	{Name: "linear-sum-loop", Label: "Linear sum loops", Description: "hoist bounds and split exact i64 memory reductions across four accumulators", Architectures: pairArchitectures[:], Default: true},
	{Name: "prepared-direct-entry", Label: "Direct prepared entry", Description: "enter compiler-proved integer functions through the register ABI", Architectures: allArchitectures[:], Default: true},
	{Name: "prepared-light-entry", Label: "Light prepared entry", Description: "use caller-clobber proofs to select a smaller native entry thunk", Architectures: arm64Architectures[:], Default: true},
	{Name: "prepared-bounded-entry", Label: "Bounded prepared entry", Description: "keep compiler-proven bounded native invocations on the Go scheduler", Architectures: allArchitectures[:], Default: true},
	{Name: "native-leaf-host", Label: "Native scalar host leaves", Description: "lower proven pure Go scalar callbacks into native import trampolines", Architectures: allArchitectures[:], Default: true},
	{Name: "direct-go-host-import", Label: "Direct Go host imports", Description: "bypass cross-instance marshaling for bound Go imports in bounded numeric functions", Architectures: pairArchitectures[:], Default: true},
	{Name: "wide-loop-int-const", Label: "Wide loop integer constants", Description: "keep repeatedly materialized non-imm32 i64 loop constants in otherwise-idle registers", Architectures: amd64Architectures[:], Default: true},
	{Name: "compact-loop-align32", Label: "Compact loop alignment", Description: "place small loop functions at 32-byte fetch-block boundaries", Architectures: amd64Architectures[:], Default: true},
	{Name: "interval-scratch-lease", Label: "Regional scratch leasing", Description: "lend idle RDX to proved straight-line integer local regions", Architectures: amd64Architectures[:], Default: true},
	{Name: "interval-r8-lease", Label: "Regional R8 leasing", Description: "lend R8 to signal-bounded straight-line integer regions after excluding fixed-register lowerings", Architectures: amd64Architectures[:], Default: true},
	{Name: "interval-i64-weight", Label: "Regional i64 weighting", Description: "price packed 64-bit local reloads more heavily in regional eviction", Architectures: amd64Architectures[:], Default: true},
	{Name: "interval-next-use", Label: "Regional next-use eviction", Description: "use a compact local-event tape to evict the farthest local version and elide dead stores", Architectures: pairArchitectures[:], Default: true},
	{Name: "memsize-regional-lease", Label: "Regional memory-size leasing", Description: "lend the memory-size register to proved straight-line local regions", Architectures: amd64Architectures[:], Default: true},
	{Name: "module-global-regional-lease", Label: "Regional module-global leasing", Description: "lend an unreferenced module-global register to proved straight-line local regions", Architectures: amd64Architectures[:], Default: true},
	{Name: "loop-memory-base", Label: "Loop memory page bases", Description: "hoist repeated linear-memory page bases into idle loop-constant registers", Architectures: arm64Architectures[:], Default: true},
	{Name: "layout-padding-skip", Label: "Skip layout padding", Description: "branch over long alignment-preserving padding runs", Architectures: arm64Architectures[:], Default: true},
	{Name: "loop-int-const", Label: "Loop integer constants", Description: "keep costly loop-invariant integer constants in otherwise-idle registers", Architectures: arm64Architectures[:], Default: true},
	{Name: "interval-call-regions", Label: "Local regions around calls", Description: "cache local integer values in bounded regions that canonicalize before calls and fixed-scratch helpers", Architectures: arm64Architectures[:], Default: true},
	{Name: "scoped-loop-int-const", Label: "Scoped loop integer constants", Description: "lease invariant integer constants only inside proven call-free loops, including functions that call elsewhere", Architectures: arm64Architectures[:], Default: true},
	{Name: "wide-reuse-pins", Label: "Wide repeated local pins", Description: "retain bounded integer locals whose loop-weighted reads exceed definitions", Architectures: arm64Architectures[:], Default: true},
	{Name: "regional-memory-read", Label: "Regional memory address reads", Description: "borrow immediate regional memory addresses while pinning their leases against eviction", Architectures: arm64Architectures[:], Default: true},
	{Name: "br-table-branch-vector", Label: "Branch table vectors", Description: "dispatch small unique zero-argument tables directly through executable branches", Architectures: arm64Architectures[:], Default: true},
	{Name: "bounded-branch-index", Label: "Bounded branch indices", Description: "omit branch-vector bounds tests for direct i32 masks and constants proved in range", Architectures: arm64Architectures[:], Default: true},
	{Name: "borrowed-div-rem", Label: "Read-only division operands", Description: "read concrete pinned integer division and remainder operands without copying", Architectures: pairArchitectures[:], Default: true},
	{Name: "bounded-signed-quotient", Label: "Bounded signed quotient", Description: "skip exact i32 division when a subsequent signed clamp saturates", Architectures: arm64Architectures[:], Default: true},
	{Name: "taken-zero-branch", Label: "Known zero branch targets", Description: "thread direct zero branches through tests implied by their taken edge", Architectures: arm64Architectures[:], Default: true},
	{Name: "common-exit-compare", Label: "Common-exit compares", Description: "combine consecutive integer comparison branches sharing an exit", Architectures: arm64Architectures[:], Default: true},
	{Name: "guarded-boolean-xor", Label: "Guarded boolean XOR", Description: "if-convert pure bounded integer boolean XOR regions after exact clobber proofs", Architectures: arm64Architectures[:], Default: true},
	{Name: "cset-lineage", Label: "Boolean comparison flags", Description: "reuse unchanged flags through exact CSET boolean copies for bounded integer selects", Architectures: arm64Architectures[:], Default: true},
	{Name: "integer-select-local-read", Label: "Integer select local reads", Description: "borrow protected integer operands for materialized local select sinks", Architectures: arm64Architectures[:], Default: true},
	{Name: "dominated-indexed-base", Label: "Dominated indexed addresses", Description: "reuse indexed addresses through proven bounded direct branch regions", Architectures: arm64Architectures[:], Default: true},
	{Name: "dominated-indexed-key", Label: "Equivalent indexed addresses", Description: "merge bounded address facts by their exact register inputs", Architectures: arm64Architectures[:], Default: true},
	{Name: "ordinary-shifted-immediate", Label: "Shifted integer displacements", Description: "use exact shifted immediates for read-only ordinary displacement arithmetic", Architectures: arm64Architectures[:], Default: true},
	{Name: "indexed-base-reuse", Label: "Adjacent indexed base reuse", Description: "reuse an unchanged base-plus-index address across adjacent memory accesses", Architectures: arm64Architectures[:], Default: true},
	{Name: "convert-read", Label: "Direct conversion reads", Description: "read pinned integer conversion inputs without a temporary copy", Architectures: arm64Architectures[:], Default: true},
	{Name: "cold-call-local-pins", Label: "Cold-call local pins", Description: "pin hot locals when every direct local call is outside loops", Architectures: arm64Architectures[:], Default: true},
	{Name: "vex-float-mem", Label: "VEX memory operands", Description: "fold scalar float loads into AVX operations", Architectures: amd64Architectures[:], Default: true},
	{Name: "direct-int-branch-merge", Label: "Direct integer branch results", Description: "deliver unconditional integer branch results directly to their merge register", Architectures: amd64Architectures[:], Default: true},
	{Name: "wide-local-pins", Label: "Bounded wide-function pins", Description: "keep up to two locals per register bank in bounded numeric functions with loops", Architectures: amd64Architectures[:], Default: true},
	{Name: "memory-compare-immediate", Label: "Integer memory comparisons", Description: "compare deferred integer loads with constants using the exact access width", Architectures: amd64Architectures[:], Default: true},
	{Name: "float-store-borrow", Label: "Read-only floating-point stores", Description: "store pinned scalar floating-point locals and cached constants without scratch copies", Architectures: amd64Architectures[:], Default: true},
	{Name: "float-frame-mem", Label: "Float frame operands", Description: "fold scalar frame locals and spills into exact-width SSE or AVX arithmetic", Architectures: amd64Architectures[:], Default: true},
	{Name: "multi-bounds-cert", Label: "Multiple bounds proofs", Description: "retain independent proofs for interleaved arrays", Architectures: pairArchitectures[:], Default: true},
	{Name: "addr-zext-elim", Label: "Memory32 address cleanup", Description: "skip redundant zero-extension of proven-clean memory32 addresses", Architectures: amd64Architectures[:], Default: true},
	{Name: "canonical-i32", Label: "Canonical i32 carriers", Description: "reuse canonical regional-local and spill carriers for repeated 32-bit uses", Architectures: amd64Architectures[:], Default: true},
	{Name: "commute-fixed-self-update", Label: "Fixed-register self updates", Description: "retain proved-safe fixed-register accumulators across commutative update trees", Architectures: amd64Architectures[:], Default: true},
	{Name: "immutable-table", Label: "Immutable tables", Description: "specialize calls through never-written tables", Architectures: pairArchitectures[:], Default: true},
	{Name: "immutable-table-type", Label: "Immutable table types", Description: "skip redundant immutable-table type checks", Architectures: pairArchitectures[:], Default: true},
	{Name: "inline-callfree", Label: "Call-free inline hints", Description: "prioritize call-free functions for inlining", Architectures: pairArchitectures[:], Default: true},
	{Name: "store-forward", Label: "Store forwarding", Description: "forward straight-line stores into loads", Architectures: pairArchitectures[:], Default: true},
	{Name: "frame-elide", Label: "Frame elision", Description: "omit frames for small single-result functions", Architectures: amd64Architectures[:], Default: true},
	{Name: "compact-i32-frame", Label: "Compact i32 frames", Description: "pack i32 locals in straight-line call-free functions", Architectures: amd64Architectures[:], Default: true},
	{Name: "local-slot-order", Label: "Symbolic local slot packing", Description: "move exact referenced local homes into zero-reference compact slots", Architectures: amd64Architectures[:], Default: true},
	{Name: "commute-self-update", Label: "Commute self-updates", Description: "make non-fixed destinations accumulate commutative self-update expressions in place", Architectures: amd64Architectures[:], Default: true},
	{Name: "i64-mask32", Label: "Low-32 mask lowering", Description: "lower i64 low-32 masks to zero-extending 32-bit ANDs", Architectures: amd64Architectures[:], Default: true},
	{Name: "accumulator-immediate", Label: "Accumulator immediates", Description: "use ModRM-free RAX/EAX imm32 encodings in size objectives", Architectures: amd64Architectures[:], Default: true},
	{Name: "frame-elide-reghomed", Label: "Register-homed frames", Description: "omit frames when locals remain in registers", Architectures: arm64Architectures[:], Default: true},
	{Name: "small-frame", Label: "Small frames", Description: "use compact stack adjustment forms", Architectures: arm64Architectures[:], Default: true},
	{Name: "zero-branch", Label: "Zero branches", Description: "select direct zero-test branches when flags are not live", Architectures: arm64Architectures[:], Default: true},
	{Name: "mul-add-fuse", Label: "Multiply-add fusion", Description: "fuse multiply-add and multiply-subtract expressions", Architectures: arm64Architectures[:], Default: true},
	{Name: "tail-self-loop", Label: "tail-self-loop", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.TailCallBit, Default: true},
	{Name: "tail-loop-inline", Label: "tail-loop-inline", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.TailCallBit, Default: true},
	{Name: "entry-init-elision", Label: "Entry initialization elision", Description: "skip initialization of locals overwritten before their first read", Architectures: arm64Architectures[:], Default: true},
	{Name: "v128-direct-results", Label: "v128-direct-results", Architectures: arm64Architectures[:], requiredFeatures: buildfeatures.SIMDBit, Default: true},
	{Name: "dead-gc-new", Label: "dead-gc-new", Architectures: amd64Architectures[:], requiredFeatures: buildfeatures.GCBit, Default: true},
	{Name: "gc-native-alloc", Label: "gc-native-alloc", Architectures: amd64Architectures[:], requiredFeatures: buildfeatures.GCBit, Default: true},
	{Name: "v128-const-cache", Label: "Vector constant cache", Description: "reserve repeated vector constants and target-specific masks", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.SIMDBit, requiredArch: "arm64", Default: true},
	{Name: "v128-pins", Label: "v128-pins", Architectures: pairArchitectures[:], requiredFeatures: buildfeatures.SIMDBit, Default: true},
	{Name: "reg-abi", Label: "Register ABI", Description: "use Wago's internal register calling convention", Architectures: allArchitectures[:], Default: true},
	{Name: "direct-reference-abi", Label: "direct-reference-abi", Architectures: amd64Architectures[:], requiredFeatures: buildfeatures.ReferenceTypesBit, Default: true},
	{Name: "inline", Label: "Inlining", Description: "inline eligible callees", Architectures: pairArchitectures[:], Default: true},
	{Name: "stack-fence", Label: "Stack fence", Description: "emit the stack-overflow guard fence", Architectures: pairArchitectures[:], Default: true},
	{Name: "stack-reg", Label: "Stack register", Description: "keep the guest stack pointer in a register", Architectures: pairArchitectures[:], Default: true},
}

func definitions() []Definition {
	catalogOnce.Do(func() { addProposalDescriptions(catalog) })
	return catalog
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
