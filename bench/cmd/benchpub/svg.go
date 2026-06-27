package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// palette is a set of distinguishable line/bar colors, cycled by series index.
var palette = []string{
	"#4e79a7", "#f28e2b", "#59a14f", "#e15759", "#76b7b2", "#edc948",
	"#b07aa1", "#ff9da7", "#9c755f", "#bab0ac", "#1b9e77", "#d95f02",
}

func renderCharts(outDir string, run Run, hist History) {
	dir := filepath.Join(outDir, "charts")
	for _, stage := range stageOrder {
		if bars := stageEntries(run, stage); len(bars) > 0 {
			must(writeFile(filepath.Join(dir, "latency-"+lc(stage)+".svg"), barChart(stage, bars)))
		}
		if svg, ok := trendChart(stage, hist); ok {
			must(writeFile(filepath.Join(dir, "trend-"+lc(stage)+".svg"), svg))
		}
	}
}

type entry struct {
	key string
	ns  float64
}

func stageEntries(run Run, stage string) []entry {
	var es []entry
	for name, m := range run.Metrics {
		if s, k := stageOf(name); s == stage {
			es = append(es, entry{k, m.Ns})
		}
	}
	sort.Slice(es, func(i, j int) bool { return es[i].key < es[j].key })
	return es
}

// --- geometry / scale ---

const (
	svgW = 900
	padL = 70
	padR = 24
	padT = 48
	padB = 96
)

// logY maps a value in [lo,hi] to a y pixel in [top, bottom] on a log axis.
func logY(v, lo, hi float64, top, bottom float64) float64 {
	if v < lo {
		v = lo
	}
	lg := math.Log10(v/lo) / math.Log10(hi/lo)
	return bottom - lg*(bottom-top)
}

func bounds(vals []float64) (lo, hi float64) {
	lo, hi = math.MaxFloat64, 0
	for _, v := range vals {
		if v > 0 && v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	if lo == math.MaxFloat64 || lo <= 0 {
		lo = 1
	}
	// pad to surrounding powers of 10 for clean gridlines
	lo = math.Pow(10, math.Floor(math.Log10(lo)))
	hi = math.Pow(10, math.Ceil(math.Log10(hi*1.0001)))
	if hi <= lo {
		hi = lo * 10
	}
	return lo, hi
}

// --- bar chart: latency per module for one stage (latest run) ---

func barChart(stage string, es []entry) string {
	h := 420
	top, bottom := float64(padT), float64(h-padB)
	vals := make([]float64, len(es))
	for i, e := range es {
		vals[i] = e.ns
	}
	lo, hi := bounds(vals)

	var b strings.Builder
	header(&b, svgW, h, fmt.Sprintf("wago %s — ns/op by module (log scale, lower is faster)", stage))
	gridLog(&b, lo, hi, top, bottom)

	cw := float64(svgW-padL-padR) / float64(len(es))
	bw := cw * 0.6
	for i, e := range es {
		x := float64(padL) + float64(i)*cw + (cw-bw)/2
		y := logY(e.ns, lo, hi, top, bottom)
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`+"\n",
			x, y, bw, bottom-y, palette[i%len(palette)])
		txt(&b, x+bw/2, y-4, "val", "middle", nsLabel(e.ns))
		// rotated module label under the axis
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" class="cat" text-anchor="end" transform="rotate(-35 %.1f %.1f)">%s</text>`+"\n",
			x+bw/2, bottom+14, x+bw/2, bottom+14, esc(e.key))
	}
	axis(&b, bottom)
	b.WriteString("</svg>")
	return b.String()
}

// --- trend chart: ns/op per module across versions for one stage ---

func trendChart(stage string, hist History) (string, bool) {
	runs := hist.Runs
	if len(runs) == 0 {
		return "", false
	}
	// collect module keys present in this stage, and all values for bounds
	keySet := map[string]bool{}
	var vals []float64
	for _, r := range runs {
		for name, m := range r.Metrics {
			if s, k := stageOf(name); s == stage {
				keySet[k] = true
				vals = append(vals, m.Ns)
			}
		}
	}
	if len(keySet) == 0 {
		return "", false
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lo, hi := bounds(vals)

	h := 460
	top, bottom := float64(padT), float64(h-padB)
	var b strings.Builder
	header(&b, svgW, h, fmt.Sprintf("wago %s — ns/op over versions (log scale)", stage))
	gridLog(&b, lo, hi, top, bottom)

	plotR := float64(svgW - padR - 150) // leave room for legend
	xOf := func(i int) float64 {
		if len(runs) == 1 {
			return (float64(padL) + plotR) / 2
		}
		return float64(padL) + float64(i)/float64(len(runs)-1)*(plotR-float64(padL))
	}

	for ki, k := range keys {
		col := palette[ki%len(palette)]
		var pts []string
		for i, r := range runs {
			m, ok := r.Metrics[stage+"/"+k]
			if !ok {
				continue
			}
			x, y := xOf(i), logY(m.Ns, lo, hi, top, bottom)
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", x, y))
			fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="2.5" fill="%s"/>`+"\n", x, y, col)
		}
		if len(pts) > 1 {
			fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="1.5"/>`+"\n",
				strings.Join(pts, " "), col)
		}
		ly := float64(padT) + float64(ki)*16
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="10" height="10" fill="%s"/>`+"\n", plotR+18, ly, col)
		txt(&b, plotR+32, ly+9, "leg", "start", k)
	}
	// x labels: version per run (sparse to avoid crowding)
	step := 1 + len(runs)/8
	for i, r := range runs {
		if i%step != 0 && i != len(runs)-1 {
			continue
		}
		x := xOf(i)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" class="cat" text-anchor="end" transform="rotate(-35 %.1f %.1f)">%s</text>`+"\n",
			x, bottom+14, x, bottom+14, esc(shortVer(r.Version)))
	}
	axis(&b, bottom)
	b.WriteString("</svg>")
	return b.String(), true
}

// --- svg primitives ---

func header(b *strings.Builder, w, h int, title string) {
	fmt.Fprintf(b, `<svg width="%d" height="%d" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg" font-family="ui-sans-serif,system-ui,sans-serif">`+"\n", w, h, w, h)
	b.WriteString(`<style>.val{font-size:10px;fill:#444}.cat{font-size:11px;fill:#333}.leg{font-size:11px;fill:#333}.ax{font-size:10px;fill:#888}.grid{stroke:#eee}.axis{stroke:#bbb}</style>` + "\n")
	fmt.Fprintf(b, `<rect width="%d" height="%d" fill="#fff"/>`+"\n", w, h)
	fmt.Fprintf(b, `<text x="%d" y="26" font-size="15" font-weight="600" fill="#222">%s</text>`+"\n", padL, esc(title))
}

func gridLog(b *strings.Builder, lo, hi, top, bottom float64) {
	for v := lo; v <= hi+1e-9; v *= 10 {
		y := logY(v, lo, hi, top, bottom)
		fmt.Fprintf(b, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" class="grid"/>`+"\n", padL, y, svgW-padR, y)
		fmt.Fprintf(b, `<text x="%d" y="%.1f" class="ax" text-anchor="end">%s</text>`+"\n", padL-6, y+3, nsLabel(v))
	}
}

func axis(b *strings.Builder, bottom float64) {
	fmt.Fprintf(b, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" class="axis"/>`+"\n", padL, bottom, svgW-padR, bottom)
}

func txt(b *strings.Builder, x, y float64, cls, anchor, s string) {
	fmt.Fprintf(b, `<text x="%.1f" y="%.1f" class="%s" text-anchor="%s">%s</text>`+"\n", x, y, cls, anchor, esc(s))
}

func nsLabel(v float64) string {
	switch {
	case v >= 1e9:
		return trim(v/1e9) + "s"
	case v >= 1e6:
		return trim(v/1e6) + "ms"
	case v >= 1e3:
		return trim(v/1e3) + "µs"
	default:
		return trim(v) + "ns"
	}
}

func trim(v float64) string {
	s := fmt.Sprintf("%.1f", v)
	s = strings.TrimSuffix(s, ".0")
	return s
}

func shortVer(v string) string {
	if len(v) > 16 {
		return v[len(v)-16:]
	}
	return v
}

func lc(s string) string { return strings.ToLower(s) }

func esc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
