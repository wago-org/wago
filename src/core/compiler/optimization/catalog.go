// Package optimization owns the compiler optimization catalog shared by
// backends, the CLI, configuration, and standalone cross-compilation.
package optimization

import (
	"fmt"
	"sync"
)

// Definition describes one user-configurable compiler optimization.
// Register new optimizations here, then bind them to their backend boolean in
// that backend's knobs.go. Every config and CLI surface is derived from this
// catalog.
type Definition struct {
	Name          string
	Label         string
	Description   string
	Default       bool
	Experimental  bool
	Architectures []string
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
}

// Bind creates an ordinary architecture binding.
func Bind(name string, value *bool) BindingSpec { return BindingSpec{Name: name, Value: value} }

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
	word     uint8
	mask     uint64
}

// Enabled reports whether name is enabled in this selection. Unknown names and
// selections from another architecture report false.
func (s Selection) Enabled(name string) bool {
	if s.bindings == nil {
		return false
	}
	index, ok := s.bindings.index[name]
	return ok && s.bits[index/64]&(uint64(1)<<uint(index%64)) != 0
}

// EnabledOption reports whether a pre-resolved option is enabled. An option
// from another architecture is rejected just like an unknown string name.
func (s Selection) EnabledOption(option Option) bool {
	return s.bindings != nil && s.bindings == option.bindings && s.bits[option.word]&option.mask != 0
}

// Valid reports whether the selection was resolved by a Bindings owner.
func (s Selection) Valid() bool { return s.bindings != nil }

// Option resolves name once for use on a backend's hot compile path. It panics
// for an unknown name because backend option inventories are initialization
// invariants already checked by NewBindings.
func (b *Bindings) Option(name string) Option {
	index, ok := b.index[name]
	if !ok {
		panic(fmt.Sprintf("unknown %s optimization %q", b.arch, name))
	}
	return Option{bindings: b, word: uint8(index / 64), mask: uint64(1) << uint(index%64)}
}

// Bindings owns the complete, ordered set of bindings for one architecture.
// NewBindings panics on missing, duplicate, unknown, or nil bindings so an
// advertised optimization cannot reach execution without an implementation.
type Bindings struct {
	mu       sync.Mutex
	arch     string
	entries  []binding
	index    map[string]int
	revision uint64
}

func NewBindings(arch string, specs ...BindingSpec) *Bindings {
	byName := make(map[string]BindingSpec, len(specs))
	for _, spec := range specs {
		if spec.Value == nil {
			panic(fmt.Sprintf("%s optimization binding %q has a nil value", arch, spec.Name))
		}
		if _, exists := byName[spec.Name]; exists {
			panic(fmt.Sprintf("%s optimization binding %q is duplicated", arch, spec.Name))
		}
		if _, ok := Lookup(arch, spec.Name); !ok {
			panic(fmt.Sprintf("%s optimization binding %q is not registered", arch, spec.Name))
		}
		byName[spec.Name] = spec
	}
	definitions := ForArch(arch)
	if len(definitions) > 128 {
		panic(fmt.Sprintf("%s optimization catalog has %d entries, maximum is 128", arch, len(definitions)))
	}
	bindings := &Bindings{
		arch:     arch,
		entries:  make([]binding, 0, len(definitions)),
		index:    make(map[string]int, len(definitions)),
		revision: 1,
	}
	for _, definition := range definitions {
		spec, ok := byName[definition.Name]
		if !ok {
			panic(fmt.Sprintf("%s optimization %q has no backend binding", arch, definition.Name))
		}
		bindings.entries = append(bindings.entries, binding{definition: definition, value: spec.Value, inverted: spec.Inverted})
		bindings.index[definition.Name] = len(bindings.entries) - 1
		delete(byName, definition.Name)
	}
	return bindings
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

// Resolve validates and captures one immutable selection without installing it
// into package globals or retaining the Bindings lock. overrides is expressed
// in public sense (on means enabled). A nil map captures current process
// defaults.
func (b *Bindings) Resolve(overrides map[string]bool) (Selection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var bits [2]uint64
	for index, entry := range b.entries {
		on := *entry.value
		if entry.inverted {
			on = !on
		}
		if on {
			bits[index/64] |= uint64(1) << uint(index%64)
		}
	}
	for name, on := range overrides {
		index, ok := b.index[name]
		if !ok {
			return Selection{}, fmt.Errorf("unknown %s optimization %q", b.arch, name)
		}
		word := index / 64
		mask := uint64(1) << uint(index%64)
		if on {
			bits[word] |= mask
		} else {
			bits[word] &^= mask
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
	index, ok := b.index[name]
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

var catalog = []Definition{
	both("bounds-facts", "Bounds facts", "straight-line bounds-check elision"),
	both("st-flags", "Flags results", "keep comparison results in the flags register"),
	amd64("store8-flags", "Byte-store flags", "sink comparison results directly into byte stores"),
	both("reg-merge", "Register merge", "keep block results in registers across joins"),
	both("tee-sink", "Tee sinking", "sink local.tee expressions into local registers"),
	both("unary-sink", "Unary sinking", "sink unary and conversion expressions in place"),
	arm64("three-op-sink", "Three-operand sinking", "sink binary operations into pinned locals"),
	arm64("olddest-rhs-sink", "Old destination reuse", "reuse an old destination register as the right operand"),
	both("branch-fold", "Branch folding", "fold branch pairs into one conditional branch"),
	arm64("store-load-fwd", "Store/load forwarding", "forward stores into loads after assembly"),
	arm64("uxtw-add", "Extended adds", "fold zero-extension into ADD UXTW"),
	both("value-facts", "Value facts", "propagate bounded upper-zero and boolean provenance"),
	both("call-effect-bounds", "Call effect bounds", "preserve bounds proofs across effect-safe direct calls"),
	arm64("abi-classes", "Internal ABI classes", "classify effect-safe direct-call register contracts"),
	arm64("abi-leaf-fp", "FP leaf ABI", "preserve caller pin banks across bounded FP leaf calls"),
	both("prepared-fp-entry", "Prepared FP/mixed entry", "mark bounded FP and mixed-bank functions for fixed prepared trampolines"),
	both("entry-init-elide", "Entry initialization elision", "skip local initialization when bounded definite assignment makes the initial value unobservable"),
	both("gc-dead-new", "Dropped GC constructors", "retain allocation and traps while omitting unreachable constructor payload population"),
	arm64("gc-fixed-array-len", "Known array lengths", "replace an immediately consumed constructor reference with its statically known length"),
	arm64("gc-const-struct-get", "Known struct fields", "replace an immediately consumed constructor reference with its statically known numeric field"),
	arm64("gc-constructor-cast", "Constructor casts", "elide an adjacent cast of a constructor result to its exact defined type"),
	both("simd-wide-bitmask-consumer", "Wide SIMD bitmask consumers", "reduce wide-lane sign bits directly for adjacent zero tests and population counts"),
	both("entry-arg-pins", "Entry argument pins", "keep entry arguments in incoming registers"),
	arm64("x8-pin", "X8 scratch pin", "pin a scratch value in call-free functions"),
	arm64("deep-fp-pins", "Deep float pins", "pin additional float locals in call-free functions"),
	both("ext-fp-pins", "Extended float pins", "use the larger floating-point register pool"),
	amd64("call-next-use", "Call next-use", "skip dead pinned-local stores before calls"),
	amd64("affine-lea", "Affine LEA", "fold bounded affine index trees into scaled addressing"),
	amd64("tree-order", "Valent tree ordering", "schedule bounded commutative trees by register need"),
	amd64("assoc-tree", "Associative tree cover", "cover high-pressure bounded associative trees with one accumulator"),
	experimentalAMD64("bmi2-rorx", "BMI2 rotates", "use non-destructive immediate rotates on BMI2 hosts"),
	arm64("leaf-scratch-pins", "Leaf scratch pins", "pin scratch values in leaf functions"),
	amd64("vex-float-mem", "VEX memory operands", "fold scalar float loads into AVX operations"),
	amd64("multi-bounds-cert", "Multiple bounds proofs", "retain independent proofs for interleaved arrays"),
	amd64("addr-zext-elim", "Memory32 address cleanup", "skip redundant zero-extension of proven-clean memory32 addresses"),
	both("immutable-table", "Immutable tables", "specialize calls through never-written tables"),
	both("immutable-table-type", "Immutable table types", "skip redundant immutable-table type checks"),
	both("inline-callfree", "Call-free inline hints", "prioritize call-free functions for inlining"),
	both("store-forward", "Store forwarding", "forward straight-line stores into loads"),
	amd64("frame-elide", "Frame elision", "omit frames for small single-result functions"),
	amd64("compact-i32-frame", "Compact i32 frames", "pack i32 locals in straight-line call-free functions"),
	amd64("local-slot-order", "Symbolic local slot packing", "move exact referenced local homes into zero-reference compact slots"),
	amd64("tee-spill-elide", "Reuse tee spill homes", "reuse a local.tee frame slot when spilling its still-live scalar result"),
	amd64("commute-self-update", "Commute self-updates", "make non-fixed destinations accumulate commutative self-update expressions in place"),
	amd64("i64-mask32", "Low-32 mask lowering", "lower i64 low-32 masks to zero-extending 32-bit ANDs"),
	amd64("accumulator-immediate", "Accumulator immediates", "use ModRM-free RAX/EAX imm32 encodings in size objectives"),
	arm64("frame-elide-reghomed", "Register-homed frames", "omit frames when locals remain in registers"),
	arm64("small-frame", "Small frames", "use compact stack adjustment forms"),
	both("v128-const-cache", "Vector constant cache", "reserve vector registers for repeated constants"),
	both("v128-pins", "Vector pins", "pin hot vector locals in registers"),
	both("v128-sink", "Vector sinking", "sink vector operations into pinned locals"),
	arm64("shuffle-half-zip", "Halfword shuffle ZIP", "select native halfword ZIP instructions for exact shuffle masks"),
	both("reg-abi", "Register ABI", "use Wago's internal register calling convention"),
	both("inline", "Inlining", "inline eligible callees"),
	experimentalBoth("inline-loop-callees", "Loop-call inlining", "inline callees invoked from inside loops"),
	both("loop-precheck", "Loop prechecks", "hoist invariant bounds checks before loops"),
	experimentalArm64("loop-region-pins", "Loop-region pins", "pin loop-carried values across loop regions"),
	experimentalArm64("immutable-poly-fastpath", "Polymorphic table fast path", "specialize polymorphic immutable-table calls"),
	experimentalArm64("legacy-fp-pins", "Legacy float pins", "use the legacy floating-point pin allocator"),
	experimentalArm64("legacy-gp-pins", "Legacy integer pins", "use the legacy integer pin allocator"),
	both("stack-fence", "Stack fence", "emit the stack-overflow guard fence"),
	both("stack-reg", "Stack register", "keep the guest stack pointer in a register"),
}

func both(name, label, description string) Definition {
	return Definition{Name: name, Label: label, Description: description, Default: true, Architectures: []string{"amd64", "arm64"}}
}

func amd64(name, label, description string) Definition {
	return Definition{Name: name, Label: label, Description: description, Default: true, Architectures: []string{"amd64"}}
}

func arm64(name, label, description string) Definition {
	return Definition{Name: name, Label: label, Description: description, Default: true, Architectures: []string{"arm64"}}
}

func experimentalBoth(name, label, description string) Definition {
	return Definition{Name: name, Label: label, Description: description, Experimental: true, Architectures: []string{"amd64", "arm64"}}
}

func experimentalArm64(name, label, description string) Definition {
	return Definition{Name: name, Label: label, Description: description, Experimental: true, Architectures: []string{"arm64"}}
}

func experimentalAMD64(name, label, description string) Definition {
	return Definition{Name: name, Label: label, Description: description, Experimental: true, Architectures: []string{"amd64"}}
}

// ForArch returns every optimization registered for arch in catalog order.
func ForArch(arch string) []Definition {
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
	for _, definition := range catalog {
		if definition.Name == name && supports(definition, arch) {
			return clone(definition), true
		}
	}
	return Definition{}, false
}

// Exists reports whether name is registered for arch without copying the
// public Definition's mutable architecture slice.
func Exists(arch, name string) bool {
	for _, definition := range catalog {
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
