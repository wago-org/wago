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
	bits     uint64
}

// Option is a pre-resolved flag owned by one architecture's Bindings. Backends
// resolve these once during package initialization so hot lowering decisions
// need only a pointer comparison and bit test, never a string/map lookup.
type Option struct {
	bindings *Bindings
	mask     uint64
}

// Enabled reports whether name is enabled in this selection. Unknown names and
// selections from another architecture report false.
func (s Selection) Enabled(name string) bool {
	if s.bindings == nil {
		return false
	}
	index, ok := s.bindings.index[name]
	return ok && s.bits&(uint64(1)<<index) != 0
}

// EnabledOption reports whether a pre-resolved option is enabled. An option
// from another architecture is rejected just like an unknown string name.
func (s Selection) EnabledOption(option Option) bool {
	return s.bindings != nil && s.bindings == option.bindings && s.bits&option.mask != 0
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
	return Option{bindings: b, mask: uint64(1) << index}
}

// Bindings owns the complete, ordered set of bindings for one architecture.
// NewBindings panics on missing, duplicate, unknown, or nil bindings so an
// advertised optimization cannot reach execution without an implementation.
type Bindings struct {
	mu       sync.Mutex
	arch     string
	entries  []binding
	index    map[string]int
	before   []bool
	changed  []int
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
	bindings := &Bindings{
		arch:     arch,
		entries:  make([]binding, 0, len(definitions)),
		index:    make(map[string]int, len(definitions)),
		before:   make([]bool, len(definitions)),
		changed:  make([]int, len(definitions)),
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

// ResolveSnapshot validates and captures one immutable selection without
// installing it into package globals or retaining the Bindings lock. overrides
// is expressed in public sense (on means enabled). A nil map captures current
// process defaults. Snapshot and deltas are accepted for API compatibility; the
// complete overrides map remains authoritative and makes resolution bounded by
// the small optimization catalog.
func (b *Bindings) ResolveSnapshot(overrides map[string]bool, _ Snapshot, _ map[string]bool) (Selection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) > 64 {
		return Selection{}, fmt.Errorf("%s optimization catalog has %d entries, maximum is 64", b.arch, len(b.entries))
	}
	var bits uint64
	for index, entry := range b.entries {
		on := *entry.value
		if entry.inverted {
			on = !on
		}
		if on {
			bits |= uint64(1) << index
		}
	}
	for name, on := range overrides {
		index, ok := b.index[name]
		if !ok {
			return Selection{}, fmt.Errorf("unknown %s optimization %q", b.arch, name)
		}
		mask := uint64(1) << index
		if on {
			bits |= mask
		} else {
			bits &^= mask
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
			index := b.index[name]
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
		index, ok := b.index[name]
		if !ok {
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
		index, ok := b.index[name]
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
	arm64("load-pair", "Adjacent load pairs", "combine adjacent full-width scalar loads from one local address into LDP"),
	arm64("entry-param-pairs", "Entry parameter pairs", "pair adjacent serialized wrapper parameter homes in function prologues"),
	arm64("entry-zero-pairs", "Entry zero pairs", "pair adjacent declared-local zero stores in function prologues"),
	both("entry-arg-pins", "Entry argument pins", "keep entry arguments in incoming registers"),
	arm64("x8-pin", "X8 scratch pin", "pin a scratch value in call-free functions"),
	arm64("deep-fp-pins", "Deep float pins", "pin additional float locals in call-free functions"),
	both("ext-fp-pins", "Extended float pins", "use the larger floating-point register pool"),
	amd64("call-next-use", "Call next-use", "skip dead pinned-local stores before calls"),
	arm64("merge-next-use", "Merge next-use", "keep dead forward-merge locals lazy with bounded post-merge lookahead"),
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
