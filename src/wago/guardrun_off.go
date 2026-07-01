//go:build !wago_guardpage

package wago

import (
	"fmt"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// newGuardedJobMemory is unavailable without the wago_guardpage build; the config
// layer rejects signals-based bounds checks before reaching here, so this only
// guards against a deserialized signals-based module.
func newGuardedJobMemory(int, int) (*wruntime.JobMemory, error) {
	return nil, fmt.Errorf("signals-based bounds checks require a binary built with -tags wago_guardpage")
}
