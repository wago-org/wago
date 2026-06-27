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
	if svg, ok := realworldChart(run); ok {
		must(writeFile(filepath.Join(dir, "realworld.svg"), svg))
	}
	if svg, ok := compileEnginesChart(run); ok {
		must(writeFile(filepath.Join(dir, "compile-engines.svg"), svg))
	}
	if svg, ok := execEnginesChart(run); ok {
		must(writeFile(filepath.Join(dir, "exec-engines.svg"), svg))
	}
}

type legItem struct {
	label string
	color string
	op    float64
}

func legend(b *strings.Builder, items []legItem) {
	lx := float64(padL)
	for _, it := range items {
		fmt.Fprintf(b, `<rect x="%.1f" y="34" width="10" height="10" fill="%s" fill-opacity="%.2f"/>`+"\n", lx, it.color, it.op)
		txt(b, lx+14, 43, "leg", "start", it.label)
		lx += 22 + float64(len(it.label))*6.4
	}
}

// compileEnginesChart compares per-module compile time across wago, wazero and
// WARP. wago shows CompileFull where it can compile, else its Validate time
// (dimmed) — so the modules the backend can't compile yet still appear with the
// stage they reach. Modules are ordered by wasm size.
func compileEnginesChart(run Run) (string, bool) {
	type row struct {
		name  string
		bytes int64
	}
	var rows []row
	for name, info := range run.Modules {
		if _, ok := run.Metrics["WazeroCompile/"+name]; ok {
			rows = append(rows, row{name, info.Bytes})
		}
	}
	if len(rows) == 0 {
		return "", false
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].bytes < rows[j].bytes })

	wago := func(name string) (ns float64, ok, validateOnly bool) {
		if m, ok := run.Metrics["CompileFull/"+name]; ok {
			return m.Ns, true, false
		}
		if m, ok := run.Metrics["Validate/"+name]; ok {
			return m.Ns, true, true
		}
		return 0, false, false
	}

	var vals []float64
	for _, r := range rows {
		if v, ok, _ := wago(r.name); ok {
			vals = append(vals, v)
		}
		for _, k := range []string{"WazeroCompile/", "WarpCompile/"} {
			if m, ok := run.Metrics[k+r.name]; ok {
				vals = append(vals, m.Ns)
			}
		}
	}
	lo, hi := bounds(vals)
	cwago, cwazero, cwarp := palette[0], palette[3], palette[1]

	h := 480
	top, bottom := float64(padT+20), float64(h-padB)
	var b strings.Builder
	header(&b, svgW, h, "compile time: wago vs wazero vs WARP (ns/op, log scale)")
	legend(&b, []legItem{{"wago", cwago, 1}, {"wago (validate only)", cwago, 0.4}, {"wazero", cwazero, 1}, {"WARP", cwarp, 1}})
	gridLog(&b, lo, hi, top, bottom)

	gw := float64(svgW-padL-padR) / float64(len(rows))
	bw := gw * 0.8 / 3
	drawbar := func(gx float64, slot int, ns, op float64, col string) {
		x := gx + gw*0.1 + float64(slot)*bw
		y := logY(ns, lo, hi, top, bottom)
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" fill-opacity="%.2f"/>`+"\n",
			x, y, bw*0.9, bottom-y, col, op)
	}
	for gi, r := range rows {
		gx := float64(padL) + float64(gi)*gw
		if v, ok, vo := wago(r.name); ok {
			op := 1.0
			if vo {
				op = 0.4
			}
			drawbar(gx, 0, v, op, cwago)
		}
		if m, ok := run.Metrics["WazeroCompile/"+r.name]; ok {
			drawbar(gx, 1, m.Ns, 1, cwazero)
		}
		if m, ok := run.Metrics["WarpCompile/"+r.name]; ok {
			drawbar(gx, 2, m.Ns, 1, cwarp)
		}
		cx := gx + gw/2
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" class="cat" text-anchor="end" transform="rotate(-35 %.1f %.1f)">%s</text>`+"\n",
			cx, bottom+14, cx, bottom+14, esc(r.name))
	}
	axis(&b, bottom)
	b.WriteString("</svg>")
	return b.String(), true
}

// execEnginesChart compares per-export execution time, wago vs wazero, on the
// real workloads (same args as the suite's Exec stage).
func execEnginesChart(run Run) (string, bool) {
	var names []string
	for k := range run.Metrics {
		if s, key := stageOf(k); s == "Exec" {
			if _, ok := run.Metrics["WazeroExec/"+key]; ok {
				names = append(names, key)
			}
		}
	}
	if len(names) == 0 {
		return "", false
	}
	sort.Strings(names)
	var vals []float64
	for _, n := range names {
		vals = append(vals, run.Metrics["Exec/"+n].Ns, run.Metrics["WazeroExec/"+n].Ns)
	}
	lo, hi := bounds(vals)
	cwago, cwazero := palette[0], palette[3]

	h := 450
	top, bottom := float64(padT+20), float64(h-padB)
	var b strings.Builder
	header(&b, svgW, h, "exec time: wago vs wazero (ns/op, log scale, lower is faster)")
	legend(&b, []legItem{{"wago", cwago, 1}, {"wazero", cwazero, 1}})
	gridLog(&b, lo, hi, top, bottom)

	gw := float64(svgW-padL-padR) / float64(len(names))
	bw := gw * 0.8 / 2
	for gi, n := range names {
		gx := float64(padL) + float64(gi)*gw
		draw := func(slot int, ns float64, col string) {
			x := gx + gw*0.1 + float64(slot)*bw
			y := logY(ns, lo, hi, top, bottom)
			fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`+"\n", x, y, bw*0.9, bottom-y, col)
		}
		draw(0, run.Metrics["Exec/"+n].Ns, cwago)
		draw(1, run.Metrics["WazeroExec/"+n].Ns, cwazero)
		cx := gx + gw/2
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" class="cat" text-anchor="end" transform="rotate(-35 %.1f %.1f)">%s</text>`+"\n",
			cx, bottom+14, cx, bottom+14, esc(n))
	}
	axis(&b, bottom)
	b.WriteString("</svg>")
	return b.String(), true
}

// realCategories are the corpus categories considered "real-world" (real
// programs / third-party binaries) versus the synthetic micro/scale modules.
var realCategories = map[string]bool{"compute": true, "real": true, "real-large": true}

// realworldChart compares the real-world corpus modules side by side: one group
// per module (ordered by wasm size), one colored bar per pipeline stage it
// supports, on a shared log axis. Stage bars keep a fixed slot so a missing
// stage (e.g. the big binaries the backend can't compile) reads as a gap.
func realworldChart(run Run) (string, bool) {
	type mod struct {
		name  string
		bytes int64
	}
	var mods []mod
	for name, info := range run.Modules {
		if realCategories[info.Category] {
			if _, ok := run.Metrics["Decode/"+name]; ok {
				mods = append(mods, mod{name, info.Bytes})
			}
		}
	}
	if len(mods) == 0 {
		return "", false
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].bytes < mods[j].bytes })
	stages := []string{"Decode", "Validate", "Compile", "CompileFull", "Instantiate"}

	var vals []float64
	for _, md := range mods {
		for _, s := range stages {
			if m, ok := run.Metrics[s+"/"+md.name]; ok {
				vals = append(vals, m.Ns)
			}
		}
	}
	lo, hi := bounds(vals)

	h := 470
	top, bottom := float64(padT+20), float64(h-padB)
	var b strings.Builder
	header(&b, svgW, h, "real-world corpus — pipeline cost by module (ns/op, log scale)")

	// stage legend across the top
	lx := float64(padL)
	for si, s := range stages {
		fmt.Fprintf(&b, `<rect x="%.1f" y="34" width="10" height="10" fill="%s"/>`+"\n", lx, palette[si%len(palette)])
		txt(&b, lx+14, 43, "leg", "start", s)
		lx += 24 + float64(len(s))*7
	}
	gridLog(&b, lo, hi, top, bottom)

	gw := float64(svgW-padL-padR) / float64(len(mods))
	bw := gw * 0.8 / float64(len(stages))
	for gi, md := range mods {
		gx := float64(padL) + float64(gi)*gw
		for si, s := range stages {
			m, ok := run.Metrics[s+"/"+md.name]
			if !ok {
				continue
			}
			x := gx + gw*0.1 + float64(si)*bw
			y := logY(m.Ns, lo, hi, top, bottom)
			fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`+"\n",
				x, y, bw*0.9, bottom-y, palette[si%len(palette)])
		}
		cx := gx + gw/2
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" class="cat" text-anchor="end" transform="rotate(-35 %.1f %.1f)">%s</text>`+"\n",
			cx, bottom+14, cx, bottom+14, esc(md.name+" ("+sizeLabel(md.bytes)+")"))
	}
	axis(&b, bottom)
	b.WriteString("</svg>")
	return b.String(), true
}

func sizeLabel(b int64) string {
	switch {
	case b >= 1<<20:
		return trim(float64(b)/(1<<20)) + "MB"
	case b >= 1<<10:
		return trim(float64(b)/(1<<10)) + "KB"
	default:
		return fmt.Sprintf("%dB", b)
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
