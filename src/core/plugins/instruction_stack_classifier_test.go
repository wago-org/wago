package plugins

import "testing"

func classifierInstructionSpec(shape string) InstructionSpec {
	return InstructionSpec{Module: "classifier", Name: shape, Input: []int32{32}, Output: []int32{32},
		Handler: func(_ InstructionContext, args []Bits) ([]Bits, error) { return args, nil },
		Lower: func(c LoweringContext) error {
			x := c.Input(0)
			switch shape {
			case "normal":
				x = c.Add(x, c.Const(32, 1))
			case "shared", "unreachable":
				v := c.Const(32, 1)
				for range 12 {
					v = c.Add(v, v)
				}
				if shape == "shared" {
					x = c.Add(x, v)
				}
			case "deep":
				for range 128 {
					x = c.Not(x)
				}
			}
			c.Output(0, x)
			return nil
		}}
}

func TestInstructionStackClassifier(t *testing.T) {
	for _, shape := range []string{"normal", "shared", "deep", "unreachable"} {
		t.Run(shape, func(t *testing.T) {
			d, err := Prepare(classifierInstructionSpec(shape))
			if err != nil {
				t.Fatal(err)
			}
			native, ok := d.Native()
			if !ok || len(native.Nodes) == 0 {
				t.Fatal("classifier must retain the native recipe")
			}
			want := shape != "shared"
			if native.StackCompatible != want {
				t.Fatalf("stack-compatible=%t,want%t", native.StackCompatible, want)
			}
			if native.ResultWidth != 32 || native.Output != d.recipe.outputs[0] || len(native.Nodes) != len(d.recipe.nodes) {
				t.Fatal("classifier changed recipe metadata")
			}
		})
	}
}

func BenchmarkInstructionPreparation(b *testing.B) {
	for _, shape := range []string{"normal", "shared", "deep", "unreachable"} {
		b.Run(shape, func(b *testing.B) {
			spec := classifierInstructionSpec(shape)
			b.ReportAllocs()
			var nodes int
			for range b.N {
				d, err := Prepare(spec)
				if err != nil {
					b.Fatal(err)
				}
				native, ok := d.Native()
				if !ok {
					b.Fatal("lost recipe")
				}
				nodes = len(native.Nodes)
			}
			b.ReportMetric(float64(nodes), "source-nodes")
		})
	}
}

func BenchmarkInstructionNativeClassification(b *testing.B) {
	for _, shape := range []string{"normal", "shared", "deep", "unreachable"} {
		b.Run(shape, func(b *testing.B) {
			d, err := Prepare(classifierInstructionSpec(shape))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			var nodes int
			for range b.N {
				native, ok := d.Native()
				if !ok {
					b.Fatal("lost recipe")
				}
				nodes = len(native.Nodes)
			}
			b.ReportMetric(float64(nodes), "source-nodes")
		})
	}
}
