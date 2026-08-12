package plugin

import (
	"reflect"
	"strings"
	"testing"
)

func TestArgumentParsing(t *testing.T) {
	if got := strings.Join(SplitCommaList(" a, ,b ,, c "), ","); got != "a,b,c" {
		t.Fatalf("SplitCommaList = %q", got)
	}
}

func TestParseAuthorityScopeOverrides(t *testing.T) {
	raw := `{
		"github.com/acme/plugin": {
			"host.import.define": {"modules": ["clock", "random"]},
			"instance.manage": {"maxInstances": 2, "maxMemoryBytes": 65536}
		}
	}`
	want := AuthorityScopeOverrides{
		"github.com/acme/plugin": {
			"host.import.define": {Modules: []string{"clock", "random"}},
			"instance.manage":    {MaxInstances: 2, MaxMemoryBytes: 65536},
		},
	}
	got, err := ParseAuthorityScopeOverrides(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scope overrides = %#v, want %#v", got, want)
	}
}

func TestParseAuthorityScopeOverridesRejectsAmbiguousJSON(t *testing.T) {
	tests := []string{
		`{}`,
		`{"github.com/acme/plugin":{}}`,
		`{"short/plugin":{"host.import.define":{"modules":["env"]}}}`,
		`{"github.com/acme/plugin":{"host.import.define":{"future":true}}}`,
		`{"github.com/acme/plugin":{"host.import.define":{"modules":["env"]}}} {}`,
		`{"github.com/acme/plugin":{"host.import.define":{"modules":["env"]}},"github.com/acme/plugin":{"instance.manage":{"maxInstances":1,"maxMemoryBytes":1}}}`,
		`{"github.com/acme/plugin":{"host.import.define":{"modules":["env"]},"host.import.define":{"modules":["clock"]}}}`,
		`{"github.com/acme/plugin":{"instance.manage":{"maxInstances":1,"maxInstances":2,"maxMemoryBytes":1}}}`,
	}
	for _, raw := range tests {
		if _, err := ParseAuthorityScopeOverrides(raw); err == nil {
			t.Errorf("ParseAuthorityScopeOverrides accepted %s", raw)
		}
	}
}
