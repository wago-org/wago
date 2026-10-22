//go:build linux && riscv64

package runtime

import (
	"fmt"

	"github.com/wago-org/wago/internal/runtimebridge"
)

func (p *PreparedHostScalarCall) CallBoundedHelper(_ runtimebridge.HostScalarCallAccess, _, _ uint32, _ HostCall) error {
	return fmt.Errorf("jit: bounded helper calls are not supported on riscv64")
}
