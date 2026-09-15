//go:build windows

package wagobench

import (
	"fmt"
	"io"

	"github.com/wago-org/wago"
)

func commandRuntimeImports(m corpusModule, _ []byte, _, _ io.Writer) (wago.Imports, error) {
	if m.Command.Runtime == "core" {
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported command runtime %q on windows", m.Command.Runtime)
}
