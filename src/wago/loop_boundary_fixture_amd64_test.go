//go:build amd64 && (linux || darwin || windows) && !tinygo

package wago

import (
	"fmt"
	"math"
	"strings"
)

// loopBoundaryFixture keeps the producer shape separate from its use count
// and live carriers, so source placement and register pressure vary independently.
type loopBoundaryFixture struct {
	Name                                                          string
	Uses, Pressure                                                int
	Alias, Conditional, Local, Inside, Constant, RegisterPressure bool
}

var loopBoundaryFixtures = []loopBoundaryFixture{
	{Name: "stack", Uses: 1}, {Name: "reuse", Uses: 3},
	{Name: "alias", Uses: 3, Alias: true}, {Name: "pressure", Uses: 1, Pressure: 16},
	{Name: "conditional", Uses: 1, Conditional: true}, {Name: "local", Uses: 1, Local: true},
	{Name: "inside-control", Uses: 1, Inside: true},
	{Name: "constant", Uses: 1, Constant: true},
	{Name: "pressure-register", Uses: 1, Pressure: 16, RegisterPressure: true},
}

func (f loopBoundaryFixture) WAT() string {
	var b strings.Builder
	b.WriteString(`(module (func (export "run") (param $a f64) (param $b f64) (param $n i32) (param $enabled i32) (result f64)
 (local $value f64) (local $alias f64) (local $sum f64) (local $i i32)
 `)
	for i := 1; i <= f.Pressure; i++ {
		fmt.Fprintf(&b, "local.get $a f64.const %d f64.add i64.reinterpret_f64\n", i)
		if !f.RegisterPressure {
			b.WriteString("f64.reinterpret_i64\n")
		}
	}
	if f.Conditional {
		b.WriteString("local.get $enabled if (result f64)\n")
	}
	if f.Local {
		b.WriteString("local.get $a local.get $b f64.mul local.set $value\n")
	}
	b.WriteString("block $exit (result f64)\n")
	if !f.Local {
		if f.Inside {
			b.WriteString("f64.const 0\n")
		} else {
			b.WriteString("local.get $a local.get $b f64.mul\n")
		}
		if f.Alias {
			b.WriteString("local.tee $alias\n")
		}
		b.WriteString("loop $again (param f64) (result f64) local.set $value\n")
	} else {
		b.WriteString("loop $again\n")
	}
	b.WriteString("local.get $i local.get $n i32.ge_u if local.get $sum br $exit end\n")
	if f.Inside {
		b.WriteString("local.get $a local.get $b f64.mul local.set $value\n")
	}
	for i := 0; i < f.Uses; i++ {
		value := "$value"
		if f.Alias && i%2 == 1 {
			value = "$alias"
		}
		fmt.Fprintf(&b, "local.get $sum local.get %s f64.add local.set $sum\n", value)
	}
	b.WriteString("local.get $i i32.const 1 i32.add local.set $i\n")
	if !f.Local {
		b.WriteString("local.get $value\n")
	}
	b.WriteString("br $again end unreachable end\n")
	if f.Conditional {
		b.WriteString("else f64.const 0 end\n")
	}
	for i := 0; i < f.Pressure; i++ {
		if f.RegisterPressure {
			b.WriteString("local.set $sum f64.reinterpret_i64 local.get $sum\n")
		}
		b.WriteString("f64.add\n")
	}
	b.WriteString("))")
	wat := b.String()
	if f.Constant {
		wat = strings.ReplaceAll(wat, "local.get $a local.get $b f64.mul", "f64.const 1.25 f64.const 2 f64.mul")
	}
	return wat
}

func (f loopBoundaryFixture) Want(a, b float64, n uint32, enabled bool) uint64 {
	var sum float64
	product := a * b
	if f.Constant {
		product = 2.5
	}
	if enabled || !f.Conditional {
		for i := uint32(0); i < n; i++ {
			for j := 0; j < f.Uses; j++ {
				sum += product
			}
		}
	}
	for i := f.Pressure; i > 0; i-- {
		sum = (a + float64(i)) + sum
	}
	return math.Float64bits(sum)
}

func loopBoundaryEqual(a, b uint64) bool {
	return a == b || math.IsNaN(math.Float64frombits(a)) && math.IsNaN(math.Float64frombits(b))
}
