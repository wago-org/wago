package project

import (
	"strings"
	"testing"
)

func TestProjectRejectsDuplicateJSON(t *testing.T) {
	if _, err := decodeManifest([]byte(`{"plugins":{},"plugins":{}}`), t.TempDir()); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("manifest duplicate error = %v", err)
	}
	for _, data := range []string{
		`{"formatVersion":1,"plugins":{},"Plugins":{}}`,
		`{"formatVersion":1,"plugins":{"x":{"grants":[],"Grants":[]}}}`,
		`{"formatVersion":1,"plugins":{"x":{"config":{"flag":false,"flag":true}}}}`,
	} {
		if _, err := DecodeLock([]byte(data)); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("lock duplicate error = %v", err)
		}
	}
}
