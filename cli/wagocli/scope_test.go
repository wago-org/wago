package wagocli

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
		got, err := scopeGlobal(tc.in.global, tc.in.local)
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
	for _, name := range []string{pluginScopeGlobalEnv, pluginScopeLocalEnv, pluginScopeBareEnv} {
		t.Setenv(name, "")
	}
	if err := selectPluginScope(false, true, false); err != nil {
		t.Fatal(err)
	}
	if !truthyEnv(pluginScopeLocalEnv) || truthyEnv(pluginScopeGlobalEnv) || truthyEnv(pluginScopeBareEnv) {
		t.Fatal("local scope did not clear other selections")
	}
	if err := selectPluginScope(true, false, false); err != nil {
		t.Fatal(err)
	}
	if !truthyEnv(pluginScopeGlobalEnv) || truthyEnv(pluginScopeLocalEnv) || truthyEnv(pluginScopeBareEnv) {
		t.Fatal("global scope did not clear other selections")
	}
	if err := selectPluginScope(true, true, false); err == nil {
		t.Fatal("conflicting plugin scopes were accepted")
	}
	if got := os.Getenv(pluginScopeGlobalEnv); got != "1" {
		t.Fatalf("invalid selection changed scope: %q", got)
	}
}
