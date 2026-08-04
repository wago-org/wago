// Package optimization owns the compiler optimization catalog shared by
// backends, the CLI, configuration, and standalone cross-compilation.
package optimization

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

var catalog = []Definition{
	both("bounds-facts", "Bounds facts", "straight-line bounds-check elision"),
	both("st-flags", "Flags results", "keep comparison results in the flags register"),
	both("reg-merge", "Register merge", "keep block results in registers across joins"),
	both("tee-sink", "Tee sinking", "sink local.tee expressions into local registers"),
	both("unary-sink", "Unary sinking", "sink unary and conversion expressions in place"),
	arm64("three-op-sink", "Three-operand sinking", "sink binary operations into pinned locals"),
	arm64("olddest-rhs-sink", "Old destination reuse", "reuse an old destination register as the right operand"),
	both("branch-fold", "Branch folding", "fold branch pairs into one conditional branch"),
	arm64("store-load-fwd", "Store/load forwarding", "forward stores into loads after assembly"),
	arm64("uxtw-add", "Extended adds", "fold zero-extension into ADD UXTW"),
	both("entry-arg-pins", "Entry argument pins", "keep entry arguments in incoming registers"),
	arm64("x8-pin", "X8 scratch pin", "pin a scratch value in call-free functions"),
	arm64("deep-fp-pins", "Deep float pins", "pin additional float locals in call-free functions"),
	both("ext-fp-pins", "Extended float pins", "use the larger floating-point register pool"),
	arm64("leaf-scratch-pins", "Leaf scratch pins", "pin scratch values in leaf functions"),
	amd64("vex-float-mem", "VEX memory operands", "fold scalar float loads into AVX operations"),
	amd64("multi-bounds-cert", "Multiple bounds proofs", "retain independent proofs for interleaved arrays"),
	both("immutable-table", "Immutable tables", "specialize calls through never-written tables"),
	both("immutable-table-type", "Immutable table types", "skip redundant immutable-table type checks"),
	both("inline-callfree", "Call-free inline hints", "prioritize call-free functions for inlining"),
	both("store-forward", "Store forwarding", "forward straight-line stores into loads"),
	amd64("frame-elide", "Frame elision", "omit frames for small single-result functions"),
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

// Lookup returns a registered optimization for arch.
func Lookup(arch, name string) (Definition, bool) {
	for _, definition := range catalog {
		if definition.Name == name && supports(definition, arch) {
			return clone(definition), true
		}
	}
	return Definition{}, false
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
