package validate

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
)

func TestModuleBytes(t *testing.T) {
	valid := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
	if err := ModuleBytes(valid); err != nil {
		t.Fatalf("valid empty module: %v", err)
	}
	if err := ModuleBytes([]byte("not wasm")); err == nil || !strings.Contains(err.Error(), "decode:") {
		t.Fatalf("malformed module error = %v", err)
	}
}

func TestCommandConfirmsValidModule(t *testing.T) {
	automation.Reset()
	t.Cleanup(automation.Reset)
	file := t.TempDir() + "/empty.wasm"
	if err := os.WriteFile(file, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = old })
	Command().Run(command.NewContext([]string{file}, nil, nil))
	_ = writer.Close()
	output, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if text := string(output); !strings.Contains(text, "is valid") || !strings.Contains(text, file) {
		t.Fatalf("validation confirmation = %q", text)
	}
}

func TestHelpDocumentsParallelism(t *testing.T) {
	var output strings.Builder
	Command().PrintHelp(&output, "wago validate")
	text := output.String()
	if !strings.Contains(text, "--parallel, -p [workers]") ||
		!strings.Contains(text, "parallel function validation") {
		t.Fatalf("validate help did not document function parallelism:\n%s", text)
	}
}

func TestParallelFlagForms(t *testing.T) {
	cmd := Command()
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "none", args: []string{"module.wasm"}},
		{name: "bare before", args: []string{"-p", "module.wasm"}, want: "auto"},
		{name: "joined before", args: []string{"-p8", "module.wasm"}, want: "8"},
		{name: "separated before", args: []string{"-p", "8", "module.wasm"}, want: "8"},
		{name: "long before", args: []string{"--parallel=4", "module.wasm"}, want: "4"},
		{name: "bare after", args: []string{"module.wasm", "-p"}, want: "auto"},
		{name: "joined after", args: []string{"module.wasm", "-p8"}, want: "8"},
		{name: "separated after", args: []string{"module.wasm", "-p", "8"}, want: "8"},
		{name: "long bare after", args: []string{"module.wasm", "--parallel"}, want: "auto"},
		{name: "long after", args: []string{"module.wasm", "--parallel=4"}, want: "4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, err := cmd.Normalize(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			ctx, err := cmd.Parse("wago validate", args)
			if err != nil {
				t.Fatal(err)
			}
			if got := ctx.Str("parallel"); got != tc.want {
				t.Fatalf("args=%v parallel=%q, want %q", tc.args, got, tc.want)
			}
			if len(ctx.Args) != 1 || ctx.Args[0] != "module.wasm" {
				t.Fatalf("args=%v positionals=%v", tc.args, ctx.Args)
			}
		})
	}

	args, err := cmd.Normalize([]string{"module.wasm", "--", "-p8"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := cmd.Parse("wago validate", args)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Str("parallel") != "" || len(ctx.Args) != 2 || ctx.Args[1] != "-p8" {
		t.Fatalf("terminator did not preserve -p8: parallel=%q args=%v", ctx.Str("parallel"), ctx.Args)
	}
}

func TestModuleBytesCore3(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sections []byte
		invalid  bool
	}{
		{name: "i31 global", sections: []byte{6, 8, 1, 0x6c, 0, 0x41, 0, 0xfb, 0x1c, 0x0b}},
		{name: "prior immutable global", sections: []byte{6, 11, 2, 0x7f, 0, 0x41, 1, 0x0b, 0x7f, 0, 0x23, 0, 0x0b}},
		{name: "multiple memories", sections: []byte{5, 5, 2, 0, 1, 0, 1}},
		{name: "compact mixed imports", sections: []byte{2, 13, 1, 3, 'e', 'n', 'v', 0, 0x7f, 1, 1, 'm', 2, 0, 1}},
		{name: "compact same-kind imports", sections: []byte{2, 13, 1, 3, 'e', 'n', 'v', 0, 0x7e, 2, 1, 1, 'm', 0, 1}},
		{name: "i31 wrong operand", sections: []byte{6, 8, 1, 0x6c, 0, 0x42, 0, 0xfb, 0x1c, 0x0b}, invalid: true},
		{name: "prior mutable global", sections: []byte{6, 11, 2, 0x7f, 1, 0x41, 1, 0x0b, 0x7f, 0, 0x23, 0, 0x0b}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append([]byte{0, 'a', 's', 'm', 1, 0, 0, 0}, tc.sections...)
			for _, workers := range []int{1, 4, 0} {
				err := ModuleBytesWithPolicy(data, workers)
				if tc.invalid {
					if err == nil || !strings.Contains(err.Error(), "validate:") {
						t.Fatalf("workers %d: expected validation error, got %v", workers, err)
					}
				} else if err != nil {
					t.Fatalf("workers %d: %v", workers, err)
				}
			}
		})
	}
}
