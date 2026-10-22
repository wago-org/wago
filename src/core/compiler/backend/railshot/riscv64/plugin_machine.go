//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	plugincodegen "github.com/wago-org/wago/codegen/riscv64"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/plugins"
	"sort"
)

type pluginInstructionDirectory struct {
	instructions map[uint32]railcore.CustomInstruction
	classes      map[plugins.CustomType]swarClass
	types        []plugins.CustomType
}

func pluginInstructions(instructions map[uint32]railcore.CustomInstruction) *pluginInstructionDirectory {
	if len(instructions) == 0 {
		return nil
	}
	d := &pluginInstructionDirectory{instructions: instructions}
	hasCustom := false
	for _, ins := range instructions {
		hasCustom = hasCustom || ins.CustomOutput != nil
		for _, typ := range ins.CustomInputs {
			hasCustom = hasCustom || !typ.IsZero()
		}
	}
	if !hasCustom {
		return d
	}
	d.classes = make(map[plugins.CustomType]swarClass)
	indices := make([]int, 0, len(instructions))
	for index := range instructions {
		indices = append(indices, int(index))
	}
	sort.Ints(indices)
	for _, index := range indices {
		ins := instructions[uint32(index)]
		values := append([]plugins.CustomType(nil), ins.CustomInputs...)
		if ins.CustomOutput != nil {
			values = append(values, *ins.CustomOutput)
		}
		for _, typ := range values {
			if typ.IsZero() {
				continue
			}
			if _, exists := d.classes[typ]; !exists {
				d.classes[typ] = swarCustomBase + swarClass(len(d.types))
				d.types = append(d.types, typ)
			}
		}
	}
	return d
}

// X5/X28/X29 are reserved here for checked address construction. The Wasm
// integer walker keeps its other values in canonical homes at every call.
var pluginRegisters = [...]enc.Reg{enc.X6, enc.X7, enc.X9, enc.X10, enc.X11, enc.X12, enc.X13, enc.X14, enc.X15, enc.X16, enc.X17, enc.X18, enc.X19, enc.X20, enc.X21, enc.X22, enc.X23}

type pluginRV64Context struct {
	f             *integerEmitter
	instruction   railcore.CustomInstruction
	inputs        []int32
	outputOffset  int32
	owned         uint32
	output        []enc.Reg
	outputSet     bool
	outputCells   []bool
	outputAddress bool
	active        bool
	err           error
}

func (c *pluginRV64Context) check() error {
	if !c.active {
		return fmt.Errorf("rv64: plugin context is no longer active")
	}
	return c.err
}
func (c *pluginRV64Context) Encoder() *enc.Asm {
	if c.check() != nil {
		panic("rv64: plugin context is no longer active")
	}
	return &c.f.a
}
func (c *pluginRV64Context) AllocGP(exclude ...enc.Reg) enc.Reg {
	if err := c.check(); err != nil {
		panic(err)
	}
	for _, r := range pluginRegisters {
		if c.owned&(1<<r) != 0 {
			continue
		}
		skip := false
		for _, x := range exclude {
			skip = skip || x == r
		}
		if !skip {
			c.owned |= 1 << r
			return r
		}
	}
	c.err = fmt.Errorf("rv64: plugin temporary register capacity exceeded")
	return enc.Zero
}
func (c *pluginRV64Context) ReserveGP(r enc.Reg) error {
	if err := c.check(); err != nil {
		return err
	}
	for _, allowed := range pluginRegisters {
		if r == allowed {
			c.owned |= 1 << r
			return nil
		}
	}
	return fmt.Errorf("rv64: plugin cannot reserve runtime register %d", r)
}
func (c *pluginRV64Context) Release(r enc.Reg) { c.ReleaseGP(r) }
func (c *pluginRV64Context) ReleaseGP(r enc.Reg) {
	if !c.active {
		return
	}
	for _, out := range c.output {
		if out == r {
			return
		}
	}
	c.owned &^= 1 << r
}
func (c *pluginRV64Context) InputI32(i int) (enc.Reg, error) {
	if err := c.check(); err != nil {
		return 0, err
	}
	if i < 0 || i >= len(c.inputs) || len(c.instruction.CustomInputs) != 0 && !c.instruction.CustomInputs[i].IsZero() {
		return 0, fmt.Errorf("rv64: plugin input %d is not a scalar input", i)
	}
	if c.inputs[i] == c.outputOffset && (c.outputAddress || len(c.outputCells) != 0 && c.outputCells[0]) {
		return 0, fmt.Errorf("rv64: plugin scalar input was overwritten by its custom output")
	}
	r := c.AllocGP()
	if c.err != nil {
		return 0, c.err
	}
	c.f.Load(uint8(r), c.inputs[i], false)
	width := c.instruction.InputWidths[i]
	if width < 32 {
		shift := uint8(64 - width)
		c.f.a.Slli(r, r, shift)
		c.f.a.Srli(r, r, shift)
	}
	return r, nil
}
func (c *pluginRV64Context) InputCustom(i int) ([]enc.Reg, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	if i < 0 || i >= len(c.inputs) || len(c.instruction.CustomInputs) == 0 || c.instruction.CustomInputs[i].IsZero() {
		return nil, fmt.Errorf("rv64: plugin input %d is not a custom input", i)
	}
	regs := make([]enc.Reg, int(c.instruction.CustomInputs[i].Size()/8))
	for j := range regs {
		var err error
		regs[j], err = c.InputCustomCell(i, j)
		if err != nil {
			return nil, err
		}
	}
	return regs, nil
}
func (c *pluginRV64Context) OutputI32(r enc.Reg) error {
	if err := c.check(); err != nil {
		return err
	}
	if c.instruction.CustomOutput != nil {
		return fmt.Errorf("rv64: custom output cannot receive an i32 register")
	}
	return c.setOutput([]enc.Reg{r})
}
func (c *pluginRV64Context) OutputCustom(regs ...enc.Reg) error {
	if err := c.check(); err != nil {
		return err
	}
	if c.instruction.CustomOutput == nil || len(regs) != int(c.instruction.CustomOutput.Size()/8) {
		return fmt.Errorf("rv64: plugin custom output requires its complete owned RV64 cells")
	}
	return c.setOutput(regs)
}
func (c *pluginRV64Context) setOutput(regs []enc.Reg) error {
	if c.outputSet {
		return fmt.Errorf("rv64: plugin output already assigned")
	}
	var seen uint32
	for _, r := range regs {
		if r >= 32 || c.owned&(1<<r) == 0 || seen&(1<<r) != 0 {
			return fmt.Errorf("rv64: plugin output register %d is not uniquely owned", r)
		}
		seen |= 1 << r
	}
	c.outputSet = true
	c.output = append([]enc.Reg(nil), regs...)
	return nil
}
func (c *pluginRV64Context) MemoryBase() enc.Reg {
	if err := c.check(); err != nil {
		panic(err)
	}
	if !c.f.mode.customMemory {
		c.err = fmt.Errorf("rv64: plugin memory base requires linear memory")
		return enc.Zero
	}
	r := c.AllocGP()
	c.f.productionMemoryBaseAt(0, r)
	return r
}
func (c *pluginRV64Context) CheckedMemory(input int, offset uint32, size int) (enc.Reg, enc.Reg, int32, error) {
	if err := c.check(); err != nil {
		return 0, 0, 0, err
	}
	if !c.f.mode.customMemory || input < 0 || input >= len(c.inputs) || len(c.instruction.CustomInputs) != 0 && !c.instruction.CustomInputs[input].IsZero() {
		return 0, 0, 0, fmt.Errorf("rv64: plugin memory input is not a scalar address")
	}
	if size != 1 && size != 2 && size != 4 && size != 8 && size != 16 {
		return 0, 0, 0, fmt.Errorf("rv64: invalid plugin memory size %d", size)
	}
	if c.inputs[input] == c.outputOffset && (c.outputAddress || len(c.outputCells) != 0 && c.outputCells[0]) {
		return 0, 0, 0, fmt.Errorf("rv64: plugin memory input was overwritten by its custom output")
	}
	c.f.Load(5, c.inputs[input], false)
	c.f.swarMemoryAddress(uint64(offset), uint8(size))
	if c.f.err != nil {
		return 0, 0, 0, c.f.err
	}
	r := c.AllocGP()
	if c.err != nil {
		return 0, 0, 0, c.err
	}
	c.f.a.Addi(r, enc.X5, 0)
	return enc.Zero, r, 0, nil
}

// ValidateCustomInstructions checks the exact native inline directory before a
// source root planner omits its calls from native return-PC maps.
func ValidateCustomInstructions(m *wasm.Module, instructions map[uint32]railcore.CustomInstruction) error {
	return admitCustomInstructions(m, instructions)
}

func admitCustomInstructions(m *wasm.Module, instructions map[uint32]railcore.CustomInstruction) error {
	if len(instructions) != 0 && (m == nil || len(instructions) > MaxHostFunctionImports || m.ImportedFuncCount() > MaxHostFunctionImports) {
		return unsupported("RV64 plugin import directory bound")
	}
	for index, instruction := range instructions {
		sig, ok := m.FuncSignature(index)
		if !ok || index >= uint32(m.ImportedFuncCount()) || sig == nil || len(instruction.InputWidths) != len(sig.Params) || len(sig.Params) > maxSWARLocals || len(sig.Results) > 1 {
			return unsupported("RV64 plugin import signature")
		}
		lowering, ok := instruction.Codegen.(*plugincodegen.Lowering)
		if !ok || lowering == nil {
			return unsupported("RV64 plugin native lowering target")
		}
		if lowering.Compatibility == plugincodegen.CompatibilityManaged {
			if lowering.Managed == nil || lowering.Emit != nil {
				return unsupported("RV64 managed plugin callback")
			}
		} else if lowering.Compatibility == plugincodegen.CompatibilityFullAccess {
			if lowering.Emit == nil || lowering.Managed != nil {
				return unsupported("RV64 full-access plugin callback")
			}
		} else {
			return unsupported("RV64 plugin compatibility contract")
		}
		if len(instruction.CustomInputs) != 0 && len(instruction.CustomInputs) != len(sig.Params) {
			return unsupported("RV64 plugin custom input directory")
		}
		for i, typ := range sig.Params {
			custom := len(instruction.CustomInputs) != 0 && !instruction.CustomInputs[i].IsZero()
			if custom {
				if !pluginCustomCarrier(instruction.CustomInputs[i], typ) || instruction.InputWidths[i] != instruction.CustomInputs[i].Bits() {
					return unsupported("RV64 plugin custom carrier")
				}
			} else if !wasm.EqualValType(typ, wasm.I32) || instruction.InputWidths[i] < 1 || instruction.InputWidths[i] > 32 {
				return unsupported("RV64 plugin scalar input carrier")
			}
		}
		if len(sig.Results) == 1 {
			if instruction.CustomOutput != nil {
				if !pluginCustomCarrier(*instruction.CustomOutput, sig.Results[0]) || instruction.ResultWidth != instruction.CustomOutput.Bits() {
					return unsupported("RV64 plugin custom output carrier")
				}
			} else if !wasm.EqualValType(sig.Results[0], wasm.I32) || instruction.ResultWidth < 1 || instruction.ResultWidth > 32 {
				return unsupported("RV64 plugin scalar output carrier")
			}
		} else if instruction.CustomOutput != nil || instruction.ResultWidth != 0 {
			return unsupported("RV64 plugin void output declaration")
		}
	}
	return nil
}
func (f *integerEmitter) emitPluginCall(index uint32, sig *wasm.CompType, base int, spill func(int) int32) bool {
	if f.mode.customInstructions == nil {
		return false
	}
	instruction, ok := f.mode.customInstructions.instructions[index]
	if !ok {
		return false
	}
	lowering, ok := instruction.Codegen.(*plugincodegen.Lowering)
	if !ok || lowering == nil {
		f.err = unsupported("RV64 plugin lowering target")
		return true
	}
	ctx := &pluginRV64Context{f: f, instruction: instruction, inputs: make([]int32, len(sig.Params)), outputOffset: spill(base), active: true}
	for i := range ctx.inputs {
		ctx.inputs[i] = spill(base + i)
	}
	var err error
	func() {
		defer func() { ctx.active = false }()
		if lowering.Compatibility == plugincodegen.CompatibilityManaged {
			err = lowering.Managed((*managedPluginRV64Context)(ctx))
		} else {
			err = lowering.Emit(ctx)
		}
	}()
	if err == nil {
		err = ctx.err
	}
	if err == nil && len(sig.Results) == 1 && !ctx.outputSet {
		err = fmt.Errorf("rv64: plugin lowering did not set its output")
	}
	if err == nil && len(sig.Results) == 0 && ctx.outputSet {
		err = fmt.Errorf("rv64: void plugin lowering set an output")
	}
	if err == nil && len(ctx.outputCells) != 0 {
		for _, set := range ctx.outputCells {
			if !set {
				err = fmt.Errorf("rv64: plugin custom output has unset cells")
				break
			}
		}
	}
	if err != nil {
		f.err = err
		return true
	}
	for i, r := range ctx.output {
		wide := instruction.CustomOutput != nil
		if !wide {
			width := instruction.ResultWidth
			if width < 32 {
				shift := uint8(64 - width)
				f.a.Slli(r, r, shift)
				f.a.Srli(r, r, shift)
			} else {
				f.a.ZeroExtend32(r, r)
			}
		}
		f.Store(ctx.outputOffset+int32(i*8), uint8(r), true)
	}
	return true
}
func (f *integerEmitter) tryPluginCall(index uint32, sig functionSpec, base int) bool {
	return f.emitPluginCall(index, sig.signature, base, f.SpillOffset)
}

// A managed context has no raw-encoder or register-reservation methods.
type managedPluginRV64Context pluginRV64Context

func (c *managedPluginRV64Context) InputI32(i int) (enc.Reg, error) {
	return (*pluginRV64Context)(c).InputI32(i)
}
func (c *managedPluginRV64Context) InputCustom(i int) ([]enc.Reg, error) {
	return (*pluginRV64Context)(c).InputCustom(i)
}
func (c *managedPluginRV64Context) Release(r enc.Reg)   { (*pluginRV64Context)(c).Release(r) }
func (c *managedPluginRV64Context) ReleaseGP(r enc.Reg) { (*pluginRV64Context)(c).ReleaseGP(r) }
func (c *managedPluginRV64Context) OutputI32(r enc.Reg) error {
	return (*pluginRV64Context)(c).OutputI32(r)
}
func (c *managedPluginRV64Context) OutputCustom(r ...enc.Reg) error {
	return (*pluginRV64Context)(c).OutputCustom(r...)
}

// Custom IDs never share a class with their erased numeric/reference carrier.

func (d *pluginInstructionDirectory) customType(class swarClass) (plugins.CustomType, bool) {
	if d == nil || class < swarCustomBase || int(class-swarCustomBase) >= len(d.types) {
		return plugins.CustomType{}, false
	}
	return d.types[int(class-swarCustomBase)], true
}
func pluginCarrierType(typ plugins.CustomType) wasm.ValType {
	switch typ.Carrier() {
	case plugins.WasmI64:
		return wasm.I64
	case plugins.WasmF32:
		return wasm.F32
	case plugins.WasmF64:
		return wasm.F64
	case plugins.WasmV128:
		return wasm.V128
	case plugins.WasmFuncRef:
		return wasm.FuncRef
	case plugins.WasmExternRef:
		return wasm.ExternRef
	default:
		return wasm.I32
	}
}
func pluginCustomCarrier(custom plugins.CustomType, physical wasm.ValType) bool {
	return custom.Valid() && custom.Size()%16 == 0 && wasm.EqualValType(pluginCarrierType(custom), physical)
}
func (d *pluginInstructionDirectory) hasCustom() bool { return d != nil && len(d.types) != 0 }
func (d *pluginInstructionDirectory) physicalClass(class swarClass) swarClass {
	typ, ok := d.customType(class)
	if !ok {
		return class
	}
	if typ.Carrier() == plugins.WasmFuncRef || typ.Carrier() == plugins.WasmExternRef {
		return swarFuncRef
	}
	return swarClassOf(pluginCarrierType(typ))
}
func (d *pluginInstructionDirectory) physicalSignature(sig *wasm.CompType) bool {
	if !d.hasCustom() || sig == nil || sig.Kind != wasm.CompFunc || len(sig.Params) > maxSWARLocals || len(sig.Results) > maxSWARLocals {
		return false
	}
	for _, list := range [][]wasm.ValType{sig.Params, sig.Results} {
		for _, typ := range list {
			if swarClassOf(typ) != swarInvalid {
				continue
			}
			known := false
			for _, custom := range d.types {
				known = known || wasm.EqualValType(typ, pluginCarrierType(custom))
			}
			if !known {
				return false
			}
		}
	}
	return true
}

// Budget admission needs only the erased widths. Actual signatures and logical
// operands are checked against the registered directory in the typed walker.
func pluginBudgetModule(m *wasm.Module, d *pluginInstructionDirectory) *wasm.Module {
	if !d.hasCustom() {
		return m
	}
	clone := *m
	clone.Types = append([]wasm.RecType(nil), m.Types...)
	for i := range clone.Types {
		clone.Types[i].SubTypes = append([]wasm.SubType(nil), m.Types[i].SubTypes...)
		for j := range clone.Types[i].SubTypes {
			comp := &clone.Types[i].SubTypes[j].Comp
			if !d.physicalSignature(comp) {
				continue
			}
			comp.Params = append([]wasm.ValType(nil), comp.Params...)
			comp.Results = append([]wasm.ValType(nil), comp.Results...)
			for _, list := range [][]wasm.ValType{comp.Params, comp.Results} {
				for k, typ := range list {
					if swarClassOf(typ) == swarInvalid {
						list[k] = wasm.I64
					}
				}
			}
		}
	}
	return &clone
}

func (c *pluginRV64Context) customInputSize(i int) (int32, error) {
	if err := c.check(); err != nil {
		return 0, err
	}
	if i < 0 || i >= len(c.inputs) || len(c.instruction.CustomInputs) == 0 || c.instruction.CustomInputs[i].IsZero() {
		return 0, fmt.Errorf("rv64: plugin input %d is not a custom input", i)
	}
	return c.instruction.CustomInputs[i].Size(), nil
}
func (c *pluginRV64Context) InputCustomCell(i, cell int) (enc.Reg, error) {
	size, err := c.customInputSize(i)
	if err != nil {
		return 0, err
	}
	if cell < 0 || int64(cell) >= int64(size/8) {
		return 0, fmt.Errorf("rv64: plugin custom input cell %d is out of range", cell)
	}
	if c.inputs[i] == c.outputOffset && (c.outputAddress || len(c.outputCells) != 0 && cell < len(c.outputCells) && c.outputCells[cell]) {
		return 0, fmt.Errorf("rv64: plugin custom input cell was overwritten by its output")
	}
	r := c.AllocGP()
	if c.err != nil {
		return 0, c.err
	}
	c.f.Load(uint8(r), c.inputs[i]+int32(cell*8), true)
	return r, nil
}
func (c *pluginRV64Context) OutputCustomCell(cell int, r enc.Reg) error {
	if err := c.check(); err != nil {
		return err
	}
	if c.instruction.CustomOutput == nil || cell < 0 || int64(cell) >= int64(c.instruction.CustomOutput.Size()/8) {
		return fmt.Errorf("rv64: plugin custom output cell %d is out of range", cell)
	}
	if c.outputSet && len(c.outputCells) == 0 {
		return fmt.Errorf("rv64: plugin output already assigned")
	}
	if r >= 32 || c.owned&(1<<r) == 0 {
		return fmt.Errorf("rv64: plugin custom output cell register is not owned")
	}
	if c.outputCells == nil {
		c.outputCells = make([]bool, c.instruction.CustomOutput.Size()/8)
	}
	if c.outputCells[cell] {
		return fmt.Errorf("rv64: plugin custom output cell %d already assigned", cell)
	}
	c.f.Store(c.outputOffset+int32(cell*8), uint8(r), true)
	c.outputCells[cell], c.outputSet = true, true
	return nil
}
func (c *pluginRV64Context) customAddress(offset int32) (enc.Reg, error) {
	r := c.AllocGP()
	if c.err != nil {
		return 0, c.err
	}
	if offset >= -2048 && offset <= 2047 {
		c.f.a.Addi(r, enc.SP, offset)
	} else {
		c.f.a.Constant64(r, int64(offset))
		c.f.a.Add(r, r, enc.SP)
	}
	return r, nil
}
func (c *pluginRV64Context) InputCustomAddress(i int) (enc.Reg, error) {
	if _, err := c.customInputSize(i); err != nil {
		return 0, err
	}
	return c.customAddress(c.inputs[i])
}
func (c *pluginRV64Context) OutputCustomAddress() (enc.Reg, error) {
	if err := c.check(); err != nil {
		return 0, err
	}
	if c.instruction.CustomOutput == nil {
		return 0, fmt.Errorf("rv64: plugin has no custom output")
	}
	if c.outputSet {
		return 0, fmt.Errorf("rv64: plugin output already assigned")
	}
	r, err := c.customAddress(c.outputOffset)
	if err != nil {
		return 0, err
	}
	c.outputSet, c.outputAddress = true, true
	return r, nil
}
func (c *managedPluginRV64Context) InputCustomCell(i, cell int) (enc.Reg, error) {
	return (*pluginRV64Context)(c).InputCustomCell(i, cell)
}
func (c *managedPluginRV64Context) OutputCustomCell(cell int, r enc.Reg) error {
	return (*pluginRV64Context)(c).OutputCustomCell(cell, r)
}
