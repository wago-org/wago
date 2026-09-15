package wago

import "testing"

func TestImportModuleBuilderTypedFunctionsDeclareExactSignatures(t *testing.T) {
	module := &ImportModuleBuilder{module: "env"}
	none := module.Func("none", func() {})
	if none.imp == nil || none.imp.fn == nil || len(none.imp.params) != 0 || len(none.imp.results) != 0 {
		t.Fatalf("no-parameter typed declaration = %+v", none.imp)
	}
	sink := module.Func("sink", func(int32) {})
	if sink.imp == nil || sink.imp.fn == nil || len(sink.imp.params) != 1 || sink.imp.params[0] != ValI32 || len(sink.imp.results) != 0 {
		t.Fatalf("one-parameter void typed declaration = %+v", sink.imp)
	}
	one := module.Func("one", func(v int32) int32 { return v })
	if one.imp == nil || one.imp.fn == nil || len(one.imp.params) != 1 || one.imp.params[0] != ValI32 || len(one.imp.results) != 1 || one.imp.results[0] != ValI32 {
		t.Fatalf("one-parameter typed declaration = %+v", one.imp)
	}
	two := module.Func("two", func(a, b int32) int32 { return a + b })
	if two.imp == nil || two.imp.fn == nil || len(two.imp.params) != 2 || two.imp.params[0] != ValI32 || two.imp.params[1] != ValI32 || len(two.imp.results) != 1 || two.imp.results[0] != ValI32 {
		t.Fatalf("two-parameter typed declaration = %+v", two.imp)
	}
	twoSink := module.Func("two-sink", func(int32, int32) {})
	if twoSink.imp == nil || twoSink.imp.fn == nil || len(twoSink.imp.params) != 2 || len(twoSink.imp.results) != 0 {
		t.Fatalf("two-parameter void typed declaration = %+v", twoSink.imp)
	}
	pair := module.Func("pair", func(v int32) (int32, int32) { return v, v })
	if pair.imp == nil || pair.imp.fn == nil || len(pair.imp.params) != 1 || len(pair.imp.results) != 2 || pair.imp.results[0] != ValI32 || pair.imp.results[1] != ValI32 {
		t.Fatalf("one-parameter pair typed declaration = %+v", pair.imp)
	}
	twoPair := module.Func("two-pair", func(a, b int32) (int32, int32) { return a, b })
	if twoPair.imp == nil || twoPair.imp.fn == nil || len(twoPair.imp.params) != 2 || len(twoPair.imp.results) != 2 {
		t.Fatalf("two-parameter pair typed declaration = %+v", twoPair.imp)
	}
	event := module.I32Event("event", func(int32) {})
	if event.imp == nil || event.imp.eventI32 == nil || len(event.imp.params) != 1 || event.imp.params[0] != ValI32 || len(event.imp.results) != 0 {
		t.Fatalf("deferred event declaration = %+v", event.imp)
	}
}
