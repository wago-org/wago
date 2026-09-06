package jsonstrict

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

type fieldInner struct {
	N int `json:"n"`
}
type fieldEmbedded struct {
	Item fieldInner `json:"item"`
}
type fieldEmbeddedOther struct {
	Item    map[string]int `json:"item"`
	Padding byte           `json:"-"` // A non-direct interface layout also works with reflect.StructOf.
}

func TestTypedFieldDescriptors(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		value      any
		valid      bool
	}{
		{"embedded struct", `{"item":{"n":1,"N":2}}`, struct{ fieldEmbedded }{}, false},
		{"direct dominates", `{"item":{"n":1,"N":2}}`, struct {
			fieldEmbedded
			Item map[string]int `json:"item"`
		}{}, true},
		{"ambiguous embedded ignored", `{"item":{"n":1,"N":2}}`, reflect.New(reflect.StructOf([]reflect.StructField{
			{Name: "FieldEmbedded", Type: reflect.TypeOf(fieldEmbedded{}), Anonymous: true},
			{Name: "FieldEmbeddedOther", Type: reflect.TypeOf(fieldEmbeddedOther{}), Anonymous: true},
		})).Interface(), true},
		{"raw stays exact", `{"raw":{"n":1,"N":2}}`, struct {
			Raw json.RawMessage `json:"raw"`
		}{}, true},
		{"raw exact duplicate", `{"raw":{"n":1,"n":2}}`, struct {
			Raw json.RawMessage `json:"raw"`
		}{}, false},
		{"known aliases", `{"item":{},"ITEM":{}}`, fieldEmbedded{}, false},
		{"unknown aliases", `{"missing":1,"MISSING":2}`, fieldEmbedded{}, false},
		{"unicode alias", `{"K":1,"K":2}`, struct{ K int }{}, false},
		{"exact type wins", `{"item":{"n":1,"N":2}}`, struct {
			Upper fieldInner     `json:"ITEM"`
			Lower map[string]int `json:"item"`
		}{}, true},
		{"distinct exact aliases still strict", `{"ITEM":{},"item":{}}`, struct {
			Upper fieldInner     `json:"ITEM"`
			Lower map[string]int `json:"item"`
		}{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTypedJSON([]byte(tc.text), tc.value); (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
}

func TestDescriptorCacheIsBounded(t *testing.T) {
	for i := 0; i < maxDescriptorEntries*3; i++ {
		typ := reflect.StructOf([]reflect.StructField{{Name: fmt.Sprintf("Field%d", i), Type: reflect.TypeOf(0)}})
		if err := ValidateTypedJSON([]byte(`{}`), reflect.New(typ).Interface()); err != nil {
			t.Fatal(err)
		}
	}
	descriptorCache.RLock()
	defer descriptorCache.RUnlock()
	if len(descriptorCache.entries) > maxDescriptorEntries || descriptorCache.bytes > maxDescriptorBytes {
		t.Fatalf("cache exceeds bounds: %d entries, %d bytes", len(descriptorCache.entries), descriptorCache.bytes)
	}
}

func BenchmarkValidateTypedJSON(b *testing.B) {
	for _, count := range []int{4, 16, 64, 256} {
		fields := make([]reflect.StructField, count)
		for i := range fields {
			fields[i] = reflect.StructField{Name: fmt.Sprintf("F%03d", i), Type: reflect.TypeOf(0), Tag: reflect.StructTag(fmt.Sprintf(`json:"f%03d"`, i))}
		}
		value := reflect.New(reflect.StructOf(fields)).Interface()
		data, err := json.Marshal(value)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			if err := ValidateTypedJSON(data, value); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := ValidateTypedJSON(data, value); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
