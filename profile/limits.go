package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/wago-org/wago/internal/jitprofile"
)

// Limits bound offline input and retained report structures independently of the
// runtime journal. They are not an estimate of the process's total heap usage.
type Limits struct {
	FileBytes, DecodedBytes, CodeBytes              int64
	Events, Images, Samples, Metadata, Rows, HotPCs int
}

func DefaultLimits() Limits {
	return Limits{FileBytes: 128 << 20, DecodedBytes: 128 << 20, CodeBytes: 64 << 20, Events: 65536, Images: 8192, Samples: 1_000_000, Metadata: 1_000_000, Rows: 100_000, HotPCs: 250_000}
}
func (l Limits) validate() error {
	if l.FileBytes <= 0 || l.DecodedBytes <= 0 || l.CodeBytes <= 0 || l.Events <= 0 || l.Images <= 0 || l.Samples <= 0 || l.Metadata <= 0 || l.Rows <= 0 || l.HotPCs <= 0 {
		return fmt.Errorf("all profile resource limits must be positive")
	}
	return nil
}

type budgetReader struct {
	r    io.Reader
	left int64
}

func (r *budgetReader) Read(p []byte) (int, error) {
	if r.left < 0 {
		return 0, fmt.Errorf("invalid byte limit")
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.left == 0 {
		var b [1]byte
		n, err := r.r.Read(b[:])
		if n > 0 {
			return 0, fmt.Errorf("profile decoded byte limit exceeded")
		}
		return 0, err
	}
	if int64(len(p)) > r.left {
		p = p[:r.left]
	}
	n, err := r.r.Read(p)
	r.left -= int64(n)
	return n, err
}

// LimitedReader rejects overflow instead of presenting a truncated input as EOF.
func LimitedReader(r io.Reader, bytes int64) io.Reader { return &budgetReader{r: r, left: bytes} }
func jsonEnd(d *json.Decoder) error {
	var extra any
	err := d.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("trailing JSON value")
	}
	return err
}
func DecodeJSON(r io.Reader, v any, bytes int64) error {
	if bytes <= 0 {
		return fmt.Errorf("invalid JSON byte limit")
	}
	d := json.NewDecoder(LimitedReader(r, bytes))
	if err := d.Decode(v); err != nil {
		return err
	}
	return jsonEnd(d)
}

// OpenInput enforces stored-file size before decoding, and bounds subsequent reads
// even if the file grows after the size check. Non-regular inputs are rejected.
func OpenInput(path string, bytes int64) (*os.File, io.Reader, error) {
	linked, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !linked.Mode().IsRegular() || linked.Size() > bytes || bytes <= 0 {
		return nil, nil, fmt.Errorf("profile file is not regular or exceeds byte limit: %s", path)
	}
	f, err := openInput(path)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !st.Mode().IsRegular() || !os.SameFile(linked, st) || st.Size() > bytes || bytes <= 0 {
		f.Close()
		return nil, nil, fmt.Errorf("profile file is not regular or exceeds byte limit: %s", path)
	}
	return f, LimitedReader(f, bytes), nil
}
func ReadJSONFile(path string, v any, bytes int64) error {
	f, r, err := OpenInput(path, bytes)
	if err != nil {
		return err
	}
	defer f.Close()
	return DecodeJSON(r, v, bytes)
}
func readArray[T any](r io.Reader, maxBytes int64, maxCount int, check func(T) error, rawCheck ...func([]byte) error) ([]T, error) {
	d := json.NewDecoder(LimitedReader(r, maxBytes))
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, jsonEnd(d)
	}
	if token != json.Delim('[') {
		return nil, fmt.Errorf("expected profile JSON array")
	}
	var out []T
	for d.More() {
		if len(out) >= maxCount {
			return nil, fmt.Errorf("profile record limit %d exceeded", maxCount)
		}
		var v T
		if len(rawCheck) > 0 {
			var raw json.RawMessage
			if err := d.Decode(&raw); err != nil {
				return nil, err
			}
			for _, check := range rawCheck {
				if err := check(raw); err != nil {
					return nil, err
				}
			}
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, err
			}
		} else if err := d.Decode(&v); err != nil {
			return nil, err
		}
		if check != nil {
			if err := check(v); err != nil {
				return nil, err
			}
		}
		out = append(out, v)
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if err := jsonEnd(d); err != nil {
		return nil, err
	}
	return out, nil
}
func ReadSamplesJSON(r io.Reader, l Limits) ([]Sample, error) {
	if err := l.validate(); err != nil {
		return nil, err
	}
	return readArray[Sample](r, l.DecodedBytes, l.Samples, nil)
}

type imageBudget struct {
	images, metadata int
	code             int64
}

func (b *imageBudget) add(e jitprofile.Event, l Limits) error {
	if im := e.Image; im != nil {
		b.images++
		b.code += int64(len(im.Code))
		for _, f := range im.Functions {
			b.metadata += len(f.Calls) + len(f.Decisions)
		}
		b.metadata += len(im.Regions) + len(im.Functions) + len(im.Sources) + len(im.InlineFrames) + len(im.CodeSites) + len(im.Unwind)
		if b.images > l.Images || b.code > l.CodeBytes || b.metadata > l.Metadata {
			return fmt.Errorf("profile image, metadata, or retained-code limit exceeded")
		}
		// Bound the expansion of inline ancestry for every hot PC.
		depths := make([]int, len(im.InlineFrames))
		for i, f := range im.InlineFrames {
			if f.Parent > uint32(i) {
				return fmt.Errorf("invalid inline parent")
			}
			d := 1
			if f.Parent > 0 {
				d += depths[f.Parent-1]
			}
			if d > 128 {
				return fmt.Errorf("profile inline depth limit exceeded")
			}
			depths[i] = d
		}
	}
	return nil
}
func ValidateEvents(events []jitprofile.Event, l Limits) error {
	if err := l.validate(); err != nil {
		return err
	}
	if len(events) > l.Events {
		return fmt.Errorf("profile event limit exceeded")
	}
	var b imageBudget
	for _, e := range events {
		if err := b.add(e, l); err != nil {
			return err
		}
	}
	return nil
}
func ReadEventsJSON(r io.Reader, l Limits) ([]jitprofile.Event, error) {
	if err := l.validate(); err != nil {
		return nil, err
	}
	var b imageBudget
	entries := 0
	return readArray[jitprofile.Event](r, l.DecodedBytes, l.Events, func(e jitprofile.Event) error { return b.add(e, l) }, func(raw []byte) error { return checkStructure(raw, &entries, l.Metadata) })
}

// CheckInput validates a raw file before handing it to an external reader.
func CheckInput(path string) error {
	f, _, err := OpenInput(path, DefaultLimits().FileBytes)
	if err != nil {
		return err
	}
	return f.Close()
}

// Check structural complexity before unmarshalling an image's nested arrays and
// maps. The byte ceiling alone would permit millions of empty function records.
func checkStructure(raw []byte, entries *int, limit int) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	depth := 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if delim, ok := token.(json.Delim); ok {
			if delim == '[' || delim == '{' {
				*entries++
				if *entries > limit {
					return fmt.Errorf("profile JSON structural-entry limit exceeded")
				}
				depth++
				if depth > 128 {
					return fmt.Errorf("profile JSON nesting limit exceeded")
				}
			} else {
				depth--
			}
			continue
		}
		*entries++
		if *entries > limit {
			return fmt.Errorf("profile JSON structural-entry limit exceeded")
		}
	}
}
