package main

import "testing"

func TestGuests(t *testing.T) {
	rt := newRuntime()
	defer rt.Close()
	for name, guest := range map[string]struct {
		source     []byte
		initialize bool
	}{
		"wat":            {watGuest, false},
		"assemblyscript": {assemblyScriptGuest, false},
		"tinygo":         {tinyGoGuest, true},
	} {
		if got := run(rt, guest.source, guest.initialize); got != 42 {
			t.Fatalf("%s answer = %d", name, got)
		}
	}
}
