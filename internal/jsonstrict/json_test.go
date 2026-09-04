package jsonstrict

import (
	"encoding/json"
	"testing"
)

func TestTypedUniqueKeys(t *testing.T) {
	type entry struct {
		Grants []string        `json:"grants"`
		Config json.RawMessage `json:"config"`
	}
	type document struct {
		Plugins map[string]entry `json:"plugins"`
	}
	for _, tc := range []struct {
		data  string
		valid bool
	}{
		{`{"plugins":{},"plugins":{}}`, false},
		{`{"plugins":{},"Plugins":{}}`, false},
		{`{"plugins":{"x":{"grants":[],"Grants":[]}}}`, false},
		{`{"plugins":{"x":{"config":{"a":1,"a":2}}}}`, false},
		{`{"plugins":{"X":{},"x":{"config":{"a":1,"A":2}}}}`, true},
	} {
		if err := ValidateTypedJSON([]byte(tc.data), document{}); (err == nil) != tc.valid {
			t.Errorf("%s: valid=%v, error=%v", tc.data, tc.valid, err)
		}
	}
}
