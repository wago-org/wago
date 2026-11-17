// Command probe measures a single fresh-process public-API compilation.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	wago "github.com/wago-org/wago"
)

func main() {
	empty := flag.Bool("empty", false, "measure process floor without reading or compiling")
	profile := flag.String("cpuprofile", "", "optional diagnostic CPU profile; never use for acceptance timing")
	flag.Parse()
	if *empty {
		rss := peakRSS()
		fmt.Printf("{\"empty\":true,\"rss_kib\":%d}\n", rss)
		return
	}
	if flag.NArg() != 1 {
		panic("expected one Wasm path")
	}
	data, err := os.ReadFile(flag.Arg(0))
	check(err)
	hash := sha256.Sum256(data)
	if *profile != "" {
		f, e := os.Create(*profile)
		check(e)
		check(pprof.StartCPUProfile(f))
		defer f.Close()
		defer pprof.StopCPUProfile()
	}
	start := time.Now()
	compiled, err := wago.Compile(wago.NewRuntimeConfig().WithFunctionWorkers(1), data)
	elapsed := time.Since(start)
	check(err)
	rss := peakRSS()
	result := struct {
		RSSKiB     int64  `json:"rss_kib"`
		SHA256     string `json:"sha256"`
		Go         string `json:"go"`
		InputBytes int    `json:"input_bytes"`
		CompileNS  int64  `json:"compile_ns"`
		CodeBytes  int    `json:"code_bytes"`
	}{rss, hex.EncodeToString(hash[:]), runtime.Version(), len(data), elapsed.Nanoseconds(), compiled.CodeSize()}
	check(json.NewEncoder(os.Stdout).Encode(result))
	check(compiled.Close())
	runtime.KeepAlive(data)
}
func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// VmHWM belongs to this exec image. RUSAGE_SELF Maxrss can include the
// launching parent's pre-exec high-water and overstate a small compiler.
func peakRSS() int64 {
	b, err := os.ReadFile("/proc/self/status")
	check(err)
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "VmHWM:" {
			n, e := strconv.ParseInt(fields[1], 10, 64)
			check(e)
			return n
		}
	}
	panic("missing Linux VmHWM")
}
