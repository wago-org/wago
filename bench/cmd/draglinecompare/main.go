// Command draglinecompare runs serialized, alternating Railshot/Dragline ISA
// benchmark rounds and fails when any Dragline median is not lower.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type samples struct {
	railshot []float64
	dragline []float64
	ratio    []float64
}

func main() {
	rounds := flag.Int("rounds", 6, "number of balanced alternating benchmark rounds")
	benchtime := flag.String("benchtime", "500ms", "Go benchmark duration per row")
	filter := flag.String("filter", ".*", "regexp appended below the ISA benchmark root")
	target := flag.String("target", "compat", "Dragline target mode: compat or native")
	tiered := flag.Bool("tiered", false, "measure Dragline after live Railshot-to-Dragline installation")
	outPath := flag.String("out", "", "optional raw-output file")
	flag.Parse()
	if *rounds < 4 || *rounds%2 != 0 {
		fatalf("-rounds must be an even number of at least 4 so both orders have equal weight")
	}
	if *target != "compat" && *target != "native" {
		fatalf("-target must be compat or native")
	}

	bin := filepath.Join(os.TempDir(), "wago-dragline-isa-bench.test")
	build := exec.Command("go", "test", "-c", "-o", bin, ".")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		fatalf("build benchmark binary: %v", err)
	}
	defer os.Remove(bin)
	executableHash, err := hashFile(bin)
	if err != nil {
		fatalf("hash benchmark binary: %v", err)
	}

	all := make(map[string]*samples)
	var raw bytes.Buffer
	root := "BenchmarkRailshotDraglineISAExec"
	if *tiered {
		root = "BenchmarkRailshotTieredDraglineISAExec"
	}
	bench := "^" + root + "/" + *filter + "/(railshot|dragline)$"
	fmt.Fprintf(&raw, "# draglinecompare raw v1\n# target %s\n# rounds %d\n# benchtime %s\n# filter %s\n# tiered %t\n# benchmark_sha256 %x\n", *target, *rounds, *benchtime, *filter, *tiered, executableHash)
	for round := 0; round < *rounds; round++ {
		order := "railshot-first"
		if round%2 != 0 {
			order = "dragline-first"
		}
		cmd := exec.Command(bin, "-test.run=^$", "-test.bench="+bench, "-test.benchtime="+*benchtime, "-test.count=1")
		cmd.Env = append(os.Environ(), "WAGO_DRAGLINE_BENCH_ORDER="+order, "WAGO_DRAGLINE_BENCH_TARGET="+*target)
		output, err := cmd.CombinedOutput()
		fmt.Fprintf(&raw, "# round %d %s\n%s", round+1, order, output)
		if err != nil {
			fatalf("round %d: %v\n%s", round+1, err, output)
		}
		roundSamples := make(map[string]*samples)
		parse(output, root, roundSamples)
		for name, current := range roundSamples {
			if len(current.railshot) != 1 || len(current.dragline) != 1 {
				fatalf("round %d %s has railshot=%d dragline=%d samples", round+1, name, len(current.railshot), len(current.dragline))
			}
			s := all[name]
			if s == nil {
				s = new(samples)
				all[name] = s
			}
			s.railshot = append(s.railshot, current.railshot[0])
			s.dragline = append(s.dragline, current.dragline[0])
			s.ratio = append(s.ratio, current.dragline[0]/current.railshot[0])
		}
		fmt.Fprintf(os.Stderr, "completed round %d/%d (%s)\n", round+1, *rounds, order)
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, raw.Bytes(), 0o644); err != nil {
			fatalf("write raw output: %v", err)
		}
	}

	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		fatalf("benchmark filter %q selected no ISA exports", *filter)
	}
	failures := 0
	fmt.Println("| ISA export | Railshot median ns/op | Dragline median ns/op | Median delta | Paired median delta |")
	fmt.Println("|---|---:|---:|---:|---:|")
	for _, name := range names {
		s := all[name]
		r, d := median(s.railshot), median(s.dragline)
		delta := (d/r - 1) * 100
		pairedDelta := (median(s.ratio) - 1) * 100
		fmt.Printf("| %s | %.0f | %.0f | %+.2f%% | %+.2f%% |\n", name, r, d, delta, pairedDelta)
		if len(s.railshot) != *rounds || len(s.dragline) != *rounds || len(s.ratio) != *rounds || d >= r {
			failures++
		}
	}
	if failures != 0 {
		fatalf("%d/%d ISA exports did not beat Railshot by balanced median", failures, len(names))
	}
}

func hashFile(path string) ([32]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return [32]byte{}, err
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func parse(output []byte, root string, all map[string]*samples) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		prefix := root + "/"
		if len(fields) < 3 || !strings.HasPrefix(fields[0], prefix) {
			continue
		}
		name := strings.TrimPrefix(fields[0], prefix)
		if dash := strings.LastIndexByte(name, '-'); dash >= 0 {
			name = name[:dash]
		}
		slash := strings.LastIndexByte(name, '/')
		if slash < 0 {
			continue
		}
		backend := name[slash+1:]
		name = name[:slash]
		ns, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		s := all[name]
		if s == nil {
			s = new(samples)
			all[name] = s
		}
		switch backend {
		case "railshot":
			s.railshot = append(s.railshot, ns)
		case "dragline":
			s.dragline = append(s.dragline, ns)
		}
	}
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	mid := len(v) / 2
	if len(v)%2 != 0 {
		return v[mid]
	}
	return (v[mid-1] + v[mid]) / 2
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "draglinecompare: "+format+"\n", args...)
	os.Exit(1)
}
