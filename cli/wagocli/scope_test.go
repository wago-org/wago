package wagocli

import "testing"

func TestScopeGlobal(t *testing.T) {
	type in struct{ global, local, manifest bool }
	cases := []struct {
		in      in
		want    bool
		wantErr bool
	}{
		// No flags: cwd manifest selects local, absence falls back to global.
		{in{false, false, true}, false, false},
		{in{false, false, false}, true, false},
		// Explicit flags win over the cwd state.
		{in{true, false, true}, true, false},   // --global in a project dir
		{in{false, true, false}, false, false}, // --local in an empty dir
		{in{true, false, false}, true, false},
		{in{false, true, true}, false, false},
		// Conflicting flags error.
		{in{true, true, false}, false, true},
		{in{true, true, true}, false, true},
	}
	for _, tc := range cases {
		got, err := scopeGlobal(tc.in.global, tc.in.local, tc.in.manifest)
		if (err != nil) != tc.wantErr {
			t.Errorf("scopeGlobal(%+v) err=%v, wantErr=%v", tc.in, err, tc.wantErr)
			continue
		}
		if err == nil && got != tc.want {
			t.Errorf("scopeGlobal(%+v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
