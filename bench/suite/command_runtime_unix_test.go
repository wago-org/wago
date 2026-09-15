//go:build !windows

package wagobench

import (
	"bytes"
	"fmt"
	"io"

	"github.com/wago-org/wago"
	"github.com/wago-org/wasi/p1"
)

func commandRuntimeImports(m corpusModule, stdin []byte, stdout, stderr io.Writer) (wago.Imports, error) {
	switch m.Command.Runtime {
	case "core":
		return nil, nil
	case "wasi":
		cfg := p1.Config{
			Args: commandArgs(m), Stdin: bytes.NewReader(stdin),
			Stdout: stdout, Stderr: stderr,
			Now: func() int64 { return 0 },
		}
		if dir := commandPreopen(m); dir != "" {
			cfg.Preopens = map[string]string{"/": dir}
		}
		return p1.Imports(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported command runtime %q", m.Command.Runtime)
	}
}
