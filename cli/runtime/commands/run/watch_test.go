package run

import (
	"reflect"
	"testing"
)

func TestWithoutWatchFlagsPreservesGuestArguments(t *testing.T) {
	input := []string{"run", "--watch", "--watch-interval", "1s", "module.wasm", "--watch", "guest"}
	want := []string{"run", "module.wasm", "--watch", "guest"}
	if got := withoutWatchFlags(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("withoutWatchFlags = %#v, want %#v", got, want)
	}
}
