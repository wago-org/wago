package inspect

import "testing"

func TestPackageArgumentIsOptionalForInteractiveSelection(t *testing.T) {
	if got := Command().Args; got != "[name]" {
		t.Fatalf("inspect args = %q", got)
	}
}
