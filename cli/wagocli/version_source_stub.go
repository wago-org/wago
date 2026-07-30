//go:build wago_lean

package wagocli

import (
	"fmt"

	"github.com/wago-org/wago/internal/wagopaths"
)

var (
	buildRunnerSource = func(string, wagopaths.Profile, wagopaths.Build, string, *installProgress) error {
		return fmt.Errorf("source builds are unavailable in the lean runtime")
	}
	buildManagerSource = func(string, string, *installProgress) error {
		return fmt.Errorf("source builds are unavailable in the lean runtime")
	}
)

func syncInstalledSource(string, string, *installProgress) error {
	return fmt.Errorf("source updates are unavailable in the lean runtime")
}
