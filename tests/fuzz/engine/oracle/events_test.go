package oracle

import (
	"bytes"
	"testing"
)

func TestCanonicalEventJSONAndHash(t *testing.T) {
	events := []Event{
		{"schema", Schema},
		{"input_i32", "00000002", "9f1512ab"},
		{"mark", "000006a4"},
		{"observe_i64", "00000047", "0123456789abcdef"},
		{"outcome", "returned"},
		{"memory", 0, []string{"__fuzz_memory_0"}, 4, "sha256:9f64a747e1b97f131fabb6b447296c9b6f0201e79fb3c5356e6c77e89b6a806a"},
	}
	wantJSON := []byte(`[["schema","starshine.engine-state-events.v1"],["input_i32","00000002","9f1512ab"],["mark","000006a4"],["observe_i64","00000047","0123456789abcdef"],["outcome","returned"],["memory",0,["__fuzz_memory_0"],4,"sha256:9f64a747e1b97f131fabb6b447296c9b6f0201e79fb3c5356e6c77e89b6a806a"]]`)
	gotJSON, err := Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("canonical JSON = %s, want %s", gotJSON, wantJSON)
	}
	if got, want := Hash(gotJSON), "sha256:68aa122e9c5a54daa31fe8b12b9afa91a6a236f353d492a781ed3f82cf98e2f8"; got != want {
		t.Fatalf("hash = %s, want %s", got, want)
	}
}

func TestMixInput64IsStable(t *testing.T) {
	if got, want := MixInput64(0x5eed, 2, I32Salt), uint64(0x6f18f9116c668c0f); got != want {
		t.Fatalf("MixInput64 = %#x, want %#x", got, want)
	}
}
