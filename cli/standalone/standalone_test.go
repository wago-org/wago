package standalone

import (
	"os"
	"testing"
)

func TestRunEmptyStartModule(t *testing.T) {
	if code := Run(emptyStartModule(), nil, "", []string{"hello"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestExecuteRequiresStartExport(t *testing.T) {
	empty := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
	if err := execute(empty, nil, "", nil); err == nil || err.Error() != "module does not export _start" {
		t.Fatalf("execute error = %v", err)
	}
}

func TestExecuteInvokesExportWithTypedArgs(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = original })

	if err := execute(addModule(), nil, "add", []string{"add", "20", "22"}); err != nil {
		t.Fatal(err)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	output := make([]byte, 64)
	n, err := read.Read(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(output[:n]); got != "add(20, 22) = 42\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestExecuteRejectsWrongInvokeArguments(t *testing.T) {
	err := execute(addModule(), nil, "add", []string{"add", "20"})
	if err == nil || err.Error() != "expected 2 arg(s), got 1" {
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

func addModule() []byte {
	return []byte{
		'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 7, 1, 0x60, 2, 0x7f, 0x7f, 1, 0x7f,
		3, 2, 1, 0,
		7, 7, 1, 3, 'a', 'd', 'd', 0, 0,
		10, 9, 1, 7, 0, 0x20, 0, 0x20, 1, 0x6a, 0x0b,
	}
}
