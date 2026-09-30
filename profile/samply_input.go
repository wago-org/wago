package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// JSON unmarshalling normally grows nested slices before the caller can check
// counts. These arrays enforce shared budgets while decoding, not afterward.
type samplyArray[T any] struct {
	values []T
	used   *int
	max    int
	bytes  int64
}

func (a *samplyArray[T]) UnmarshalJSON(raw []byte) error {
	values, err := readArray[T](bytes.NewReader(raw), a.bytes, a.max-*a.used, nil)
	if err != nil {
		return err
	}
	*a.used += len(values)
	a.values = values
	return nil
}

type samplyThread struct {
	Strings []string
	Samples struct {
		Stack  []*int
		Weight []int64
	}
	Stack     struct{ Frame []int }
	Frames    struct{ Function []int }
	Functions struct{ Name []int }
}
type samplyInput struct {
	seen                     bool
	limits                   Limits
	threads                  []samplyThread
	samples, weights, tables int
}

func (s *samplyInput) UnmarshalJSON(raw []byte) error {
	if s.seen {
		return fmt.Errorf("duplicate samply threads field")
	}
	s.seen = true
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return jsonEnd(d)
	}
	if token != json.Delim('[') {
		return fmt.Errorf("expected samply threads array")
	}
	for d.More() {
		if len(s.threads) >= s.limits.Images {
			return fmt.Errorf("samply thread limit exceeded")
		}
		var row struct {
			Strings samplyArray[string] `json:"stringArray"`
			Samples struct {
				Stack  samplyArray[*int]  `json:"stack"`
				Weight samplyArray[int64] `json:"weight"`
			} `json:"samples"`
			Stack struct {
				Frame samplyArray[int] `json:"frame"`
			} `json:"stackTable"`
			Frames struct {
				Function samplyArray[int] `json:"func"`
			} `json:"frameTable"`
			Functions struct {
				Name samplyArray[int] `json:"name"`
			} `json:"funcTable"`
		}
		row.Strings = samplyArray[string]{used: &s.tables, max: s.limits.Metadata, bytes: s.limits.DecodedBytes}
		row.Samples.Stack = samplyArray[*int]{used: &s.samples, max: s.limits.Samples, bytes: s.limits.DecodedBytes}
		row.Samples.Weight = samplyArray[int64]{used: &s.weights, max: s.limits.Samples, bytes: s.limits.DecodedBytes}
		row.Stack.Frame = samplyArray[int]{used: &s.tables, max: s.limits.Metadata, bytes: s.limits.DecodedBytes}
		row.Frames.Function = samplyArray[int]{used: &s.tables, max: s.limits.Metadata, bytes: s.limits.DecodedBytes}
		row.Functions.Name = samplyArray[int]{used: &s.tables, max: s.limits.Metadata, bytes: s.limits.DecodedBytes}
		if err := d.Decode(&row); err != nil {
			return err
		}
		t := samplyThread{Strings: row.Strings.values}
		t.Samples.Stack = row.Samples.Stack.values
		t.Samples.Weight = row.Samples.Weight.values
		t.Stack.Frame = row.Stack.Frame.values
		t.Frames.Function = row.Frames.Function.values
		t.Functions.Name = row.Functions.Name.values
		s.threads = append(s.threads, t)
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	return jsonEnd(d)
}
