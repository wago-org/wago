package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func readSnapshot(path string) (Snapshot, error) {
	var s Snapshot
	f, err := os.Open(path)
	if err != nil {
		return s, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxInputBytes+1))
	if err != nil {
		return s, err
	}
	if len(b) > maxInputBytes {
		return s, fmt.Errorf("input byte budget")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&s); err != nil {
		return s, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return s, fmt.Errorf("trailing JSON")
	}
	return s, nil
}

var compiledRevision = "unqualified-build"

func main() {
	if len(os.Args) == 4 && os.Args[1] == "capture" {
		if err := captureFile(os.Args[2], os.Args[3]); err != nil {
			fail(err)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "compare" {
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: native-compare [compare] before.json after.json | capture input.wasm output.json")
		os.Exit(2)
	}
	a, err := readSnapshot(os.Args[1])
	if err != nil {
		fail(err)
	}
	b, err := readSnapshot(os.Args[2])
	if err != nil {
		fail(err)
	}
	r, err := Compare(a, b)
	if err != nil {
		fail(err)
	}
	if err = json.NewEncoder(os.Stdout).Encode(r); err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "supplied regions: compared=%d supported=%d unknown=%d changes=%d complete=%v limit=%v\n", r.Compared, r.Known, r.Unknown, len(r.Changes), r.Complete, r.Limit)
	if r.BeforeCapture != nil && r.AfterCapture != nil {
		fmt.Fprintf(os.Stderr, "image bytes outside supplied regions: before=%d after=%d\n", r.BeforeCapture.UnmappedBytes, r.AfterCapture.UnmappedBytes)
	}
	if !r.Complete {
		os.Exit(3)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(2) }
