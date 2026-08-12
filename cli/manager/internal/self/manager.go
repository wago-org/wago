// Package self owns manager updates and uninstall cleanup.
package self

import (
	"context"
	"io"

	"github.com/wago-org/wago/internal/wagopaths"
)

func ExecutablePath() string {
	return selfExecutablePath()
}

func Update(current, executable string, force bool) {
	selfUpdate(current, executable, force)
}

func UpdateContext(ctx context.Context, current, executable string, force bool) {
	selfUpdateContext(ctx, current, executable, force)
}

func RequestedMode(value string, yes bool) (Mode, bool) {
	return requestedSelfUninstallMode(value, yes)
}

func Uninstall(dirs wagopaths.Dirs, executable string, mode Mode, yes bool, in io.Reader, out io.Writer) {
	selfUninstall(dirs, executable, mode, yes, in, out)
}
