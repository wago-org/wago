package wagocli

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

var installSpinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// installProgress keeps one live terminal status line. Its animation runs on a
// ticker, independently of blocking network and checksum work. Redirected
// output remains durable and line-oriented.
type installProgress struct {
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

func newInstallProgress(out io.Writer) *installProgress {
	p := &installProgress{out: out, lastPercent: -1}
	if f, ok := out.(*os.File); ok {
		if fi, err := f.Stat(); err == nil {
			p.tty = fi.Mode()&os.ModeCharDevice != 0
		}
	}
	return p
}

func (p *installProgress) title(title string) {
	fmt.Fprintf(p.out, "%s\n", bold(title))
}

func (p *installProgress) begin(step string) {
	p.stopAnimation()
	if !p.tty {
		fmt.Fprintf(p.out, "%s %s\n", dim("…"), step)
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

func (p *installProgress) animate(stop <-chan struct{}, stopped chan<- struct{}) {
	ticker := time.NewTicker(80 * time.Millisecond)
	defer func() {
		ticker.Stop()
		close(stopped)
	}()
	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			p.frame = (p.frame + 1) % len(installSpinnerFrames)
			p.renderSpinnerLocked()
			p.mu.Unlock()
		case <-stop:
			return
		}
	}
}

func (p *installProgress) percent(step string, current, total int64) {
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
func (p *installProgress) done(step string) {
	if !p.tty {
		fmt.Fprintf(p.out, "%s %s\n", cyan("✓"), step)
		return
	}
	p.stopAnimation()
	p.mu.Lock()
	fmt.Fprintf(p.out, "\r\x1b[2K%s %s", cyan("✓"), step)
	p.active = true
	p.mu.Unlock()
}

// finish replaces the live line with the final concise result and advances.
func (p *installProgress) finish(step string) {
	if !p.tty {
		fmt.Fprintf(p.out, "%s %s\n", cyan("✓"), step)
		return
	}
	p.stopAnimation()
	p.mu.Lock()
	fmt.Fprintf(p.out, "\r\x1b[2K%s %s\n", cyan("✓"), step)
	p.active = false
	p.mu.Unlock()
}

func (p *installProgress) fail(step string) {
	if !p.tty {
		fmt.Fprintf(p.out, "%s %s\n", red("✗"), step)
		return
	}
	p.stopAnimation()
	p.mu.Lock()
	fmt.Fprintf(p.out, "\r\x1b[2K%s %s\n", red("✗"), step)
	p.active = false
	p.mu.Unlock()
}

func (p *installProgress) renderSpinnerLocked() {
	fmt.Fprintf(p.out, "\r\x1b[2K%s %s", dim(installSpinnerFrames[p.frame]), p.text)
}

func (p *installProgress) stopAnimation() {
	p.mu.Lock()
	stop, stopped := p.stop, p.stopped
	p.stop, p.stopped = nil, nil
	p.mu.Unlock()
	if stop != nil {
		close(stop)
		<-stopped
	}
}
