package project

import (
	"os"
	"testing"
)

func TestScopeGlobal(t *testing.T) {
	type in struct{ global, local bool }
	cases := []struct {
		in      in
		want    bool
		wantErr bool
	}{
		{in{false, false}, false, false},
		{in{true, false}, true, false},
		{in{false, true}, false, false},
		// Conflicting flags error.
		{in{true, true}, false, true},
	}
	for _, tc := range cases {
		got, err := MutationGlobal(tc.in.global, tc.in.local)
		if (err != nil) != tc.wantErr {
			t.Errorf("scopeGlobal(%+v) err=%v, wantErr=%v", tc.in, err, tc.wantErr)
			continue
		}
		if err == nil && got != tc.want {
			t.Errorf("scopeGlobal(%+v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSelectPluginScope(t *testing.T) {
	for _, name := range []string{GlobalEnv, LocalEnv, BareEnv} {
		t.Setenv(name, "")
	}
	if err := SelectScope(false, true, false); err != nil {
		t.Fatal(err)
	}
	if !Truthy(LocalEnv) || Truthy(GlobalEnv) || Truthy(BareEnv) {
		t.Fatal("local scope did not clear other selections")
	}
	if err := SelectScope(true, false, false); err != nil {
		t.Fatal(err)
	}
	if !Truthy(GlobalEnv) || Truthy(LocalEnv) || Truthy(BareEnv) {
		t.Fatal("global scope did not clear other selections")
	}
	if err := SelectScope(true, true, false); err == nil {
		t.Fatal("conflicting plugin scopes were accepted")
	}
	if got := os.Getenv(GlobalEnv); got != "1" {
		t.Fatalf("invalid selection changed scope: %q", got)
	}
}

func TestTruthy(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{{"1", true}, {"TRUE", true}, {"yes", true}, {"on", true}, {"0", false}, {"", false}} {
		t.Setenv("WAGO_TEST_TRUTHY", test.value)
		if got := Truthy("WAGO_TEST_TRUTHY"); got != test.want {
			t.Fatalf("Truthy(%q) = %v", test.value, got)
		}
	}
}
