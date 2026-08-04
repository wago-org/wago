package main

import "testing"

func TestParseRunAcceptsOptionalProcessorSuffix(t *testing.T) {
	const input = `goos: linux
goarch: amd64
cpu: test cpu
BenchmarkDecode/tiny          10  100 ns/op  20 B/op  3 allocs/op
BenchmarkDecode/tiny          10  120 ns/op  24 B/op  5 allocs/op
BenchmarkExec/tiny.add-16     20   30 ns/op   0 B/op  0 allocs/op
`
	run := parseRun(input)
	if run.Goos != "linux" || run.Goarch != "amd64" || run.CPU != "test cpu" {
		t.Fatalf("headers = %q/%q/%q", run.Goos, run.Goarch, run.CPU)
	}
	decode, ok := run.Metrics["Decode/tiny"]
	if !ok {
		t.Fatal("missing suffix-free Decode/tiny metric")
	}
	if decode.Ns != 110 || decode.Bytes != 22 || decode.Allocs != 4 {
		t.Fatalf("Decode/tiny = %+v", decode)
	}
	exec, ok := run.Metrics["Exec/tiny.add"]
	if !ok {
		t.Fatal("missing suffixed Exec/tiny.add metric")
	}
	if exec.Ns != 30 || exec.Bytes != 0 || exec.Allocs != 0 {
		t.Fatalf("Exec/tiny.add = %+v", exec)
	}
}

func TestParseRunDropsContainerWithChildrenWithoutProcessorSuffix(t *testing.T) {
	const input = `BenchmarkDecode       1  1 ns/op  0 B/op  0 allocs/op
BenchmarkDecode/tiny  1  2 ns/op  3 B/op  4 allocs/op
`
	run := parseRun(input)
	if _, ok := run.Metrics["Decode"]; ok {
		t.Fatal("container metric was retained")
	}
	if got, ok := run.Metrics["Decode/tiny"]; !ok || got.Ns != 2 {
		t.Fatalf("Decode/tiny = %+v, present=%v", got, ok)
	}
}
