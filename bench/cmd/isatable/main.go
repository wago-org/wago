// Command isatable converts a repeated ISA benchmark run into a complete
// median wago-vs-wazero Markdown table.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var lineRE = regexp.MustCompile(`^Benchmark(WazeroExec|Exec)/(isa_[^\s-]+(?:\.[^\s-]+)?)-\d+\s+\d+\s+([0-9.]+)\s+ns/op`)

type pair struct{ wago, wazero float64 }

func main() {
	in := flag.String("input", "", "go test benchmark output")
	out := flag.String("out", "", "Markdown output (stdout when empty)")
	cpu := flag.String("cpu", "unknown", "CPU label")
	flag.Parse()
	if *in == "" {
		fatal("-input is required")
	}
	f, err := os.Open(*in)
	if err != nil {
		fatal(err.Error())
	}
	defer f.Close()
	samples := map[string]map[string][]float64{"wago": {}, "wazero": {}}
	s := bufio.NewScanner(f)
	for s.Scan() {
		m := lineRE.FindStringSubmatch(s.Text())
		if m == nil {
			continue
		}
		engine := "wago"
		if m[1] == "WazeroExec" {
			engine = "wazero"
		}
		v, _ := strconv.ParseFloat(m[3], 64)
		samples[engine][m[2]] = append(samples[engine][m[2]], v)
	}
	if err := s.Err(); err != nil {
		fatal(err.Error())
	}
	keys := make([]string, 0, len(samples["wago"]))
	rows := map[string]pair{}
	wins := 0
	for k, ws := range samples["wago"] {
		zs := samples["wazero"][k]
		if len(ws) == 0 || len(zs) == 0 {
			continue
		}
		p := pair{median(ws), median(zs)}
		rows[k] = p
		keys = append(keys, k)
		if p.wazero > p.wago {
			wins++
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "# ARM64 ISA comparison: Wago vs wazero\n\n")
	fmt.Fprintf(&b, "CPU: %s. Values are median ns/op from repeated Go benchmark samples. Each ISA export contains a dependent loop; values are per exported call, not per machine instruction. Delta is `wazero / Wago - 1`, so positive means Wago is faster.\n\n", *cpu)
	fmt.Fprintf(&b, "Paired rows: %d. Wago faster: %d. wazero faster: %d.\n\n", len(keys), wins, len(keys)-wins)
	b.WriteString("| ISA benchmark | Wago ns/op | wazero ns/op | Wago delta |\n|---|---:|---:|---:|\n")
	for _, k := range keys {
		p := rows[k]
		fmt.Fprintf(&b, "| `%s` | %.1f | %.1f | %+.1f%% |\n", k, p.wago, p.wazero, (p.wazero/p.wago-1)*100)
	}
	if *out == "" {
		fmt.Print(b.String())
		return
	}
	if err := os.WriteFile(*out, []byte(b.String()), 0o644); err != nil {
		fatal(err.Error())
	}
}

func median(v []float64) float64 {
	v = append([]float64(nil), v...)
	sort.Float64s(v)
	if len(v)%2 != 0 {
		return v[len(v)/2]
	}
	return (v[len(v)/2-1] + v[len(v)/2]) / 2
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "isatable:", msg)
	os.Exit(1)
}
