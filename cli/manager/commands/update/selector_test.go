package update

import (
	"strings"
	"testing"
)

func TestComponentPickerSelectsEverythingByDefault(t *testing.T) {
	picker := ComponentPicker()
	if got := strings.Join(picker.Chosen(), ","); got != "Manager,Runtime,Plugins" {
		t.Fatalf("default selection = %q", got)
	}
	frame := picker.Frame()
	for _, want := range []string{"Choose what to update", "Manager", "Runtime", "Plugins", "space toggle", "a toggle all", "enter/→ update"} {
		if !strings.Contains(frame, want) {
			t.Errorf("picker does not contain %q:\n%s", want, frame)
		}
	}
}
