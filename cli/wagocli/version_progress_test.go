package wagocli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestInstallProgressLineAndTerminalRendering(t *testing.T) {
	var log bytes.Buffer
	p := newInstallProgress(&log)
	p.title("Setting Up")
	p.begin("downloading binary")
	p.percent("downloading binary", 50, 100) // redirected logs do not spam percentages
	p.done("downloaded binary")
	for _, want := range []string{
		"Setting Up\n",
		"… downloading binary\n",
		"✓ downloaded binary\n",
	} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("progress log missing %q:\n%s", want, log.String())
		}
	}

	log.Reset()
	p = &installProgress{out: &log, tty: true, lastPercent: -1}
	p.begin("downloading binary")
	time.Sleep(100 * time.Millisecond) // animation advances independently of work
	p.percent("downloading binary", 50, 100)
	p.done("downloaded binary")
	p.finish("Refreshed Wago canary")
	if text := log.String(); !strings.Contains(text, "⠙") || !strings.Contains(text, " 50%") || !strings.Contains(text, "\r\x1b[2K") || !strings.Contains(text, "✓ downloaded binary") || !strings.HasSuffix(text, "✓ Refreshed Wago canary\n") {
		t.Fatalf("terminal progress = %q", text)
	}
}
