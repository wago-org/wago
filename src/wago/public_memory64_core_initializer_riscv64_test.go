//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import "testing"

func TestRV64PublicMemory64CoreInitializerFeatureWitness(t *testing.T) {
	for _, tt := range []struct {
		name     string
		body     []byte
		features CoreFeatures
		want     ValType
		limit    int
		valid    bool
	}{
		{"i64-literal", []byte{0x42, 0, 0x0b}, 0, ValI64, 2, true},
		{"i32-literal", []byte{0x41, 0, 0x0b}, 0, ValI32, 2, true},
		{"imported-global", []byte{0x23, 0, 0x0b}, 0, ValI64, 2, true},
		{"wrong-width", []byte{0x41, 0, 0x0b}, 0, ValI64, 2, false},
		{"arithmetic-needs-feature", []byte{0x42, 1, 0x42, 2, 0x7c, 0x0b}, 0, ValI64, 2, false},
		{"arithmetic-with-feature", []byte{0x42, 1, 0x42, 2, 0x7c, 0x0b}, CoreFeatureExtendedConst, ValI64, 2, true},
		{"local-needs-feature", []byte{0x23, 1, 0x0b}, 0, ValI64, 2, false},
		{"local-needs-scope-feature", []byte{0x23, 1, 0x0b}, CoreFeatureExtendedConst, ValI64, 2, false},
		{"local-with-feature", []byte{0x23, 1, 0x0b}, CoreFeatureExtendedConstExpressions, ValI64, 2, true},
		{"forward-local", []byte{0x23, 1, 0x0b}, CoreFeatureExtendedConstExpressions, ValI64, 1, false},
		{"truncated", []byte{0x42, 0x80}, 0, ValI64, 2, false},
		{"trailing", []byte{0x42, 0, 0x0b, 0x0b}, 0, ValI64, 2, false},
		{"extra-value", []byte{0x42, 0, 0x42, 0, 0x0b}, 0, ValI64, 2, false},
		{"float", []byte{0x42, 0, 0x0b}, 0, ValF64, 2, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiled{
				requiredFeatures: CoreFeatureMemory64 | tt.features,
				Globals:          []GlobalDef{{Type: ValI64}, {Type: ValI64}},
				GlobalImports:    []GlobalImportDef{{Type: ValI64}},
			}
			if err := rv64ValidateIntegerInitializer(c, tt.body, tt.want, tt.limit); (err == nil) != tt.valid {
				t.Fatalf("initializer admission = %v; valid=%t", err, tt.valid)
			}
			c.GlobalImports[0].Mutable = true
			if err := rv64ValidateIntegerInitializer(c, []byte{0x23, 0, 0x0b}, ValI64, 2); err == nil {
				t.Fatal("mutable imported offset admitted")
			}
		})
	}
}
