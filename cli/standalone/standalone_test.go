package standalone

import "testing"

func TestRunEmptyStartModule(t *testing.T) {
	if code := Run(emptyStartModule(), nil, []string{"hello"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestExecuteRequiresStartExport(t *testing.T) {
	empty := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
	if err := execute(empty, nil, nil); err == nil || err.Error() != "module does not export _start" {
		t.Fatalf("execute error = %v", err)
	}
}

func emptyStartModule() []byte {
	return []byte{
		'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0,
		10, 4, 1, 2, 0, 0x0b,
	}
}
