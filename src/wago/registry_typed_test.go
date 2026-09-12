package wago

import "testing"

func TestImportModuleBuilderTypedFunctionsDeclareExactSignatures(t *testing.T) {
	module := &ImportModuleBuilder{module: "env"}
	one := module.I32ToI32Func("one", func(v int32) int32 { return v })
	if one.imp == nil || one.imp.typedI32 == nil || len(one.imp.params) != 1 || one.imp.params[0] != ValI32 || len(one.imp.results) != 1 || one.imp.results[0] != ValI32 {
		t.Fatalf("one-parameter typed declaration = %+v", one.imp)
	}
	two := module.I32I32ToI32Func("two", func(a, b int32) int32 { return a + b })
	if two.imp == nil || two.imp.typedI32x2 == nil || len(two.imp.params) != 2 || two.imp.params[0] != ValI32 || two.imp.params[1] != ValI32 || len(two.imp.results) != 1 || two.imp.results[0] != ValI32 {
		t.Fatalf("two-parameter typed declaration = %+v", two.imp)
	}
}
