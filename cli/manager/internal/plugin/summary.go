package plugin

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wago-org/wago/cli/internal/ui"
)

type SummaryPackage struct {
	Module  string
	Version string
}

func DisplayVersion(version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" || strings.HasPrefix(version, "0.0.0-") {
		return "0.0.0"
	}
	return version
}

func PrintSummary(output io.Writer, packages []SummaryPackage, elapsed time.Duration) {
	for _, pkg := range packages {
		name := strings.TrimPrefix(pkg.Module, "github.com/")
		fmt.Fprintf(output, "%s %s@%s\n", ui.Cyan("+"), name, pkg.Version)
	}
	fmt.Fprintf(output, "\n%d package%s installed [%s]\n",
		len(packages), plural(len(packages)), duration(elapsed))
}

func duration(elapsed time.Duration) string {
	if elapsed < time.Second {
		return fmt.Sprintf("%.1fms", float64(elapsed)/float64(time.Millisecond))
	}
	return fmt.Sprintf("%.1fs", elapsed.Seconds())
}
