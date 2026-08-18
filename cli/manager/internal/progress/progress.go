// Package progress renders concise, live status for manager transactions.
package progress

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/wago-org/wago/cli/internal/ui"
)

var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// installProgress keeps one live terminal status line. Its animation runs on a
// ticker, independently of blocking network and checksum work. Redirected
// output remains durable and line-oriented.
type Progress struct {
	out io.Writer
	tty bool

	mu          sync.Mutex
	active      bool
	text        string
	frame       int
	lastPercent int
	stop        chan struct{}
	stopped     chan struct{}
}

func NewProgress(out io.Writer) *Progress {
	p := &Progress{out: out, lastPercent: -1}
	if f, ok := out.(*os.File); ok {
		if fi, err := f.Stat(); err == nil {
			p.tty = fi.Mode()&os.ModeCharDevice != 0
		}
	}
	return p
}

func (p *Progress) Title(title string) {
	fmt.Fprintf(p.out, "%s\n", ui.Bold(title))
}

func (p *Progress) Begin(step string) {
	p.stopAnimation()
	if !p.tty {
		fmt.Fprintf(p.out, "%s %s\n", ui.Dim("…"), step)
		return
	}
	p.mu.Lock()
	p.active = true
	p.text = step
	p.frame = 0
	p.lastPercent = -1
	stop := make(chan struct{})
	stopped := make(chan struct{})
	p.stop, p.stopped = stop, stopped
	p.renderSpinnerLocked()
	p.mu.Unlock()
	go p.animate(stop, stopped)
}

func (p *Progress) animate(stop <-chan struct{}, stopped chan<- struct{}) {
	ticker := time.NewTicker(80 * time.Millisecond)
	defer func() {
		ticker.Stop()
		close(stopped)
	}()
	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			p.frame = (p.frame + 1) % len(spinnerFrames)
			p.renderSpinnerLocked()
			p.mu.Unlock()
		case <-stop:
			return
		}
	}
}

func (p *Progress) Percent(step string, current, total int64) {
	if !p.tty || total <= 0 {
		return
	}
	percent := int(current * 100 / total)
	if percent > 100 {
		percent = 100
	}
	p.mu.Lock()
	if percent != p.lastPercent {
		p.lastPercent = percent
		p.text = fmt.Sprintf("%s %3d%%", step, percent)
		p.renderSpinnerLocked()
	}
	p.mu.Unlock()
}

// done replaces the live line with a completed step. The next begin call
// overwrites it, so terminal installs retain only one status line.
func (p *Progress) Done(step string) {
	if !p.tty {
		fmt.Fprintf(p.out, "%s %s\n", ui.Cyan("✓"), step)
		return
	}
	p.stopAnimation()
	p.mu.Lock()
	fmt.Fprintf(p.out, "\r\x1b[2K%s %s", ui.Cyan("✓"), step)
	p.active = true
	p.mu.Unlock()
}

// finish replaces the live line with the final concise result and advances.
func (p *Progress) Finish(step string) {
	if !p.tty {
		fmt.Fprintf(p.out, "%s %s\n", ui.Cyan("✓"), step)
		return
	}
	p.stopAnimation()
	p.mu.Lock()
	fmt.Fprintf(p.out, "\r\x1b[2K%s %s\n", ui.Cyan("✓"), step)
	p.active = false
	p.mu.Unlock()
}

func (p *Progress) Fail(step string) {
	if !p.tty {
		fmt.Fprintf(p.out, "%s %s\n", ui.Red("✗"), step)
		return
	}
	p.stopAnimation()
	p.mu.Lock()
	fmt.Fprintf(p.out, "\r\x1b[2K%s %s\n", ui.Red("✗"), step)
	p.active = false
	p.mu.Unlock()
}

// Clear removes an active terminal status without leaving a completed line.
// It is used immediately before an interactive control takes over the same
// terminal area. Redirected output remains durable and is intentionally not
// rewritten.
func (p *Progress) Clear() {
	if !p.tty {
		return
	}
	p.stopAnimation()
	p.mu.Lock()
	if p.active {
		fmt.Fprint(p.out, "\r\x1b[2K")
		p.active = false
	}
	p.mu.Unlock()
}

func (p *Progress) renderSpinnerLocked() {
	fmt.Fprintf(p.out, "\r\x1b[2K%s %s", ui.Dim(spinnerFrames[p.frame]), p.text)
}

func (p *Progress) stopAnimation() {
	p.mu.Lock()
	stop, stopped := p.stop, p.stopped
	p.stop, p.stopped = nil, nil
	p.mu.Unlock()
	if stop != nil {
		close(stop)
		<-stopped
	}
}

func (p *Progress) Writer() io.Writer {
	return p.out
}

func (p *Progress) DisableAnimation() {
	p.tty = false
}
