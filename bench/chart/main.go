// Command chart runs the wago-vs-wazero benchmarks (or reads saved `go test
// -bench` output) and renders SVG bar charts — a zero-dependency, pure-Go take on
// json-as's hand-built SVG charts.
//
//	cd bench && go run ./chart                 # run benches, write charts/*.svg
//	cd bench && go run ./chart -in results.txt # chart saved output
//	cd bench && go run ./chart -benchtime 1s -out charts
package main

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Metric pairs a benchmark's wago and wazero results.
type Metric struct {
	Key          string // base name, e.g. "Compile"
	Label        string // display label
	WagoNs       float64
	WazeroNs     float64
	WagoAllocs   float64
	WazeroAllocs float64
}

// labels maps benchmark base names to friendly labels, in chart order.
var order = []struct{ key, label string }{
	{"Compile", "compile"},
	{"Instantiate", "instantiate"},
	{"ExecCallOverhead", "call overhead"},
	{"ExecFibLoop", "fib loop"},
	{"ExecFibRec", "fib recursion"},
}

func main() {
	in := flag.String("in", "", "read `go test -bench` output from this file instead of running it")
	out := flag.String("out", "charts", "output directory for SVGs")
	benchtime := flag.String("benchtime", "300ms", "benchtime passed to go test")
	flag.Parse()

	var raw []byte
	if *in != "" {
		b, err := os.ReadFile(*in)
		must(err)
		raw = b
	} else {
		fmt.Fprintln(os.Stderr, "running benchmarks (go test -bench)…")
		cmd := exec.Command("go", "test", "-run", "^$", "-bench", ".", "-benchmem", "-benchtime", *benchtime, ".")
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = os.Stderr
		must(cmd.Run())
		raw = buf.Bytes()
	}

	metrics := parse(string(raw))
	if len(metrics) == 0 {
		fmt.Fprintln(os.Stderr, "no benchmark lines parsed")
		os.Exit(1)
	}
	must(os.MkdirAll(*out, 0o755))
	must(os.WriteFile(*out+"/speedup.svg", []byte(speedupChart(metrics)), 0o644))
	must(os.WriteFile(*out+"/latency.svg", []byte(latencyChart(metrics)), 0o644))
	fmt.Printf("wrote %s/speedup.svg and %s/latency.svg\n", *out, *out)
}

var benchLine = regexp.MustCompile(`^Benchmark(\w+?)_(wago|wazero)(?:-\d+)?\s+\d+\s+([\d.]+)\s+ns/op(?:.*?\s(\d+)\s+allocs/op)?`)

func parse(text string) []Metric {
	byKey := map[string]*Metric{}
	for _, line := range strings.Split(text, "\n") {
		m := benchLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		key, impl := m[1], m[2]
		ns, _ := strconv.ParseFloat(m[3], 64)
		allocs := 0.0
		if m[4] != "" {
			allocs, _ = strconv.ParseFloat(m[4], 64)
		}
		met := byKey[key]
		if met == nil {
			met = &Metric{Key: key, Label: key}
			byKey[key] = met
		}
		if impl == "wago" {
			met.WagoNs, met.WagoAllocs = ns, allocs
		} else {
			met.WazeroNs, met.WazeroAllocs = ns, allocs
		}
	}
	// order by the known list, then any extras alphabetically
	var out []Metric
	seen := map[string]bool{}
	for _, o := range order {
		if m, ok := byKey[o.key]; ok && m.WagoNs > 0 && m.WazeroNs > 0 {
			m.Label = o.label
			out = append(out, *m)
			seen[o.key] = true
		}
	}
	var extra []string
	for k := range byKey {
		if !seen[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		if m := byKey[k]; m.WagoNs > 0 && m.WazeroNs > 0 {
			out = append(out, *m)
		}
	}
	return out
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// --- SVG chart rendering (style ported from json-as) ---

const (
	cWago    = "#3b82f6" // blue
	cWazero  = "#f59e0b" // amber
	cWin     = "#10b981" // green: wago faster
	cLose    = "#ef4444" // red: wago slower
	cText    = "#1f2937"
	cTick    = "#6b7280"
	cGrid    = "#e5e7eb"
	cAxis    = "#9ca3af"
	svgWidth = 900
)

func svgHeader(w, h int, title string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<svg width="%d" height="%d" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg">
<defs><style>
  .title { font: bold 20px sans-serif; fill: %s; }
  .axis-label { font: 14px sans-serif; fill: #374151; }
  .tick { font: 12px sans-serif; fill: %s; }
  .grid { stroke: %s; stroke-dasharray: 3,3; }
  .parity { stroke: #475569; stroke-width: 1.5; stroke-dasharray: 5,4; }
  .axis { stroke: %s; stroke-width: 1.5; }
  .bar-label { font: bold 11px sans-serif; fill: #374151; text-anchor: middle; }
  .legend-text { font: 13px sans-serif; fill: #374151; }
</style></defs>
<rect width="%d" height="%d" fill="#fff"/>
<text x="%d" y="34" text-anchor="middle" class="title">%s</text>
`, w, h, w, h, cText, cTick, cGrid, cAxis, w, h, w/2, title)
}

func bar(x, y, w, h float64, fill string) string {
	return fmt.Sprintf("<rect x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"%.1f\" fill=\"%s\" rx=\"4\"/>\n", x, y, w, h, fill)
}

func text(x, y float64, cls, anchor, s string) string {
	return fmt.Sprintf("<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"%s\" class=\"%s\">%s</text>\n", x, y, anchor, cls, s)
}

// speedupChart: one bar per benchmark = wazeroNs/wagoNs (log scale). Green above
// parity (wago faster), red below.
func speedupChart(ms []Metric) string {
	const h = 470
	padL, padR, padT, padB := 70.0, 40.0, 70.0, 70.0
	cw := svgWidth - padL - padR
	ch := h - padT - padB

	lo, hi := 0.5, 2.0
	for _, m := range ms {
		if s := m.WazeroNs / m.WagoNs; s > hi {
			hi = s
		} else if s < lo {
			lo = s
		}
	}
	hi *= 1.3
	yOf := func(v float64) float64 {
		f := (math.Log10(v) - math.Log10(lo)) / (math.Log10(hi) - math.Log10(lo))
		return padT + ch - f*ch
	}

	var b strings.Builder
	b.WriteString(svgHeader(svgWidth, h, "wago speedup vs wazero  (higher = wago faster, log scale)"))
	for _, gv := range []float64{0.5, 1, 2, 5, 10, 20, 50} {
		if gv < lo || gv > hi {
			continue
		}
		y := yOf(gv)
		cls := "grid"
		if gv == 1 {
			cls = "parity"
		}
		b.WriteString(fmt.Sprintf("<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" class=\"%s\"/>\n", padL, y, svgWidth-padR, y, cls))
		b.WriteString(text(padL-10, y+4, "tick", "end", fmt.Sprintf("%g×", gv)))
	}
	groupW := cw / float64(len(ms))
	barW := groupW * 0.5
	for i, m := range ms {
		s := m.WazeroNs / m.WagoNs
		x := padL + groupW*(float64(i)+0.5) - barW/2
		y := yOf(s)
		base := yOf(lo)
		fill := cWin
		if s < 1 {
			fill = cLose
		}
		b.WriteString(bar(x, y, barW, base-y, fill))
		b.WriteString(text(x+barW/2, y-7, "bar-label", "middle", fmt.Sprintf("%.1f×", s)))
		b.WriteString(text(padL+groupW*(float64(i)+0.5), h-padB+22, "tick", "middle", m.Label))
	}
	b.WriteString(fmt.Sprintf("<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" class=\"axis\"/>\n", padL, padT, padL, h-padB))
	b.WriteString(fmt.Sprintf("<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" class=\"axis\"/>\n", padL, h-padB, svgWidth-padR, h-padB))
	b.WriteString("</svg>")
	return b.String()
}

// latencyChart: grouped bars (wago + wazero) of ns/op, log scale.
func latencyChart(ms []Metric) string {
	const h = 480
	padL, padR, padT, padB := 80.0, 150.0, 70.0, 70.0
	cw := svgWidth - padL - padR
	ch := h - padT - padB

	hi := 1.0
	for _, m := range ms {
		hi = math.Max(hi, math.Max(m.WagoNs, m.WazeroNs))
	}
	loE, hiE := 0.0, math.Ceil(math.Log10(hi))
	yOf := func(v float64) float64 {
		if v < 1 {
			v = 1
		}
		f := (math.Log10(v) - loE) / (hiE - loE)
		return padT + ch - f*ch
	}

	var b strings.Builder
	b.WriteString(svgHeader(svgWidth, h, "latency: ns per operation  (log scale, lower = faster)"))
	for e := 0; float64(e) <= hiE; e++ {
		v := math.Pow(10, float64(e))
		y := yOf(v)
		b.WriteString(fmt.Sprintf("<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" class=\"grid\"/>\n", padL, y, svgWidth-padR, y))
		b.WriteString(text(padL-10, y+4, "tick", "end", nsLabel(v)))
	}
	groupW := cw / float64(len(ms))
	barW := groupW * 0.30
	base := yOf(1)
	for i, m := range ms {
		gx := padL + groupW*float64(i)
		for j, pair := range []struct {
			ns   float64
			fill string
		}{{m.WagoNs, cWago}, {m.WazeroNs, cWazero}} {
			x := gx + groupW*0.5 - barW + float64(j)*barW
			y := yOf(pair.ns)
			b.WriteString(bar(x, y, barW*0.92, base-y, pair.fill))
			b.WriteString(text(x+barW*0.46, y-5, "bar-label", "middle", nsLabel(pair.ns)))
		}
		b.WriteString(text(gx+groupW*0.5, h-padB+22, "tick", "middle", m.Label))
	}
	b.WriteString(fmt.Sprintf("<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" class=\"axis\"/>\n", padL, padT, padL, h-padB))
	b.WriteString(fmt.Sprintf("<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" class=\"axis\"/>\n", padL, h-padB, svgWidth-padR, h-padB))
	// legend
	lx := float64(svgWidth) - padR + 24
	for i, leg := range []struct{ name, fill string }{{"wago", cWago}, {"wazero", cWazero}} {
		ly := padT + float64(i)*28
		b.WriteString(bar(lx, ly-12, 18, 18, leg.fill))
		b.WriteString(text(lx+26, ly+2, "legend-text", "start", leg.name))
	}
	b.WriteString("</svg>")
	return b.String()
}

func nsLabel(v float64) string {
	switch {
	case v >= 1e6:
		return trim(v/1e6) + " ms"
	case v >= 1e3:
		return trim(v/1e3) + " µs"
	default:
		return trim(v) + " ns"
	}
}

func trim(v float64) string {
	if v >= 100 || v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}
