package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestInstallProgressLineAndTerminalRendering(t *testing.T) {
	var log bytes.Buffer
	p := NewProgress(&log)
	p.Title("Setting Up")
	p.Begin("downloading binary")
	p.Percent("downloading binary", 50, 100) // redirected logs do not spam percentages
	p.Done("downloaded binary")
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
	p = &Progress{out: &log, tty: true, lastPercent: -1}
	p.Begin("downloading binary")
	time.Sleep(100 * time.Millisecond) // animation advances independently of work
	p.Percent("downloading binary", 50, 100)
	p.Done("downloaded binary")
	p.Finish("Refreshed Wago canary")
	if text := log.String(); !strings.Contains(text, "⠙") || !strings.Contains(text, " 50%") || !strings.Contains(text, "\r\x1b[2K") || !strings.Contains(text, "✓ downloaded binary") || !strings.HasSuffix(text, "✓ Refreshed Wago canary\n") {
		t.Fatalf("terminal progress = %q", text)
	}
}
