package exports

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/wago-org/wago"
)

func TestBuildReportIncludesEveryExportKindInNameOrder(t *testing.T) {
	metadata := wago.ModuleMetadata{
		Functions: []wago.FunctionMetadata{{Index: 4, Params: []wago.ValType{wago.ValI32}, Results: []wago.ValType{wago.ValI64}, Exports: []string{"run", "alias"}}},
		Globals:   []wago.GlobalMetadata{{Index: 2, Type: wago.ValI64, Mutable: true, Exports: []string{"counter"}}},
		Tables:    []wago.TableMetadata{{Index: 1, Type: wago.ValFuncRef, Min: 2, Max: 8, HasMax: true, Exports: []string{"callbacks"}}},
		Memories:  []wago.MemoryMetadata{{Index: 0, Min: 1, Addr64: true, Shared: true, Exports: []string{"memory"}}},
		Tags:      []wago.TagMetadata{{Index: 3, TypeIndex: 7, Params: []wago.ValType{wago.ValI32}, Exports: []string{"failure"}}},
	}

	reports := BuildReport(metadata)
	names := make([]string, len(reports))
	for index, report := range reports {
		names[index] = report.Name
	}
	if want := []string{"alias", "callbacks", "counter", "failure", "memory", "run"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("export names = %v, want %v", names, want)
	}
	if got := reports[0]; got.Kind != "func" || got.Index != 4 || !reflect.DeepEqual(got.Params, []string{"i32"}) || !reflect.DeepEqual(got.Results, []string{"i64"}) {
		t.Fatalf("function report = %#v", got)
	}
	if got := reports[1]; got.Kind != "table" || got.Min == nil || *got.Min != 2 || got.Max == nil || *got.Max != 8 || got.Type != "funcref" {
		t.Fatalf("table report = %#v", got)
	}
	if got := reports[2]; got.Mutable == nil || !*got.Mutable {
		t.Fatalf("global report = %#v", got)
	}
	if got := reports[3]; got.TypeIndex == nil || *got.TypeIndex != 7 {
		t.Fatalf("tag report = %#v", got)
	}
	if got := reports[4]; got.Min == nil || *got.Min != 1 || got.Max != nil || got.Addr64 == nil || !*got.Addr64 || got.Shared == nil || !*got.Shared {
		t.Fatalf("memory report = %#v", got)
	}
}

func TestReportJSONPreservesZeroAndFalseExportProperties(t *testing.T) {
	reports := BuildReport(wago.ModuleMetadata{
		Globals:  []wago.GlobalMetadata{{Type: wago.ValI32, Exports: []string{"g"}}},
		Tables:   []wago.TableMetadata{{Type: wago.ValFuncRef, Exports: []string{"t"}}},
		Memories: []wago.MemoryMetadata{{Exports: []string{"m"}}},
	})
	payload, err := json.Marshal(reports)
	if err != nil {
		t.Fatal(err)
	}
	const want = `[{"name":"g","kind":"global","index":0,"type":"i32","mutable":false},{"name":"m","kind":"memory","index":0,"min":0,"addr64":false,"shared":false},{"name":"t","kind":"table","index":0,"type":"funcref","min":0,"addr64":false}]`
	if string(payload) != want {
		t.Fatalf("JSON = %s\nwant = %s", payload, want)
	}
}

func TestExportDetails(t *testing.T) {
	mutable := true
	min, max := uint64(1), uint64(4)
	addr64, shared := true, true
	for _, test := range []struct {
		report Report
		want   string
	}{
		{Report{Kind: "func", Params: []string{"i32"}, Results: []string{"i64"}}, "(i32) -> i64"},
		{Report{Kind: "global", Type: "i64", Mutable: &mutable}, "i64 mutable"},
		{Report{Kind: "table", Type: "funcref", Min: &min, Max: &max, Addr64: &addr64}, "funcref min=1 max=4 table64"},
		{Report{Kind: "memory", Min: &min, Addr64: &addr64, Shared: &shared}, "min=1 memory64 shared"},
		{Report{Kind: "tag", Params: []string{"i32"}}, "(i32)"},
	} {
		if got := detail(test.report); got != test.want {
			t.Errorf("detail(%#v) = %q, want %q", test.report, got, test.want)
		}
	}
}
