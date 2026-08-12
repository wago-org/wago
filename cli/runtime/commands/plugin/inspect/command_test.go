package inspect

import "testing"

func TestPackageArgumentIsOptionalForInteractiveSelection(t *testing.T) {
	if got := Command().Args; got != "[plugin-id]" {
		t.Fatalf("inspect args = %q", got)
	}
}
