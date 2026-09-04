package jsonstrict

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode"
)

// ValidateUniqueJSON validates one JSON value and rejects exactly repeated
// object members. JSON map keys remain case-sensitive.
func ValidateUniqueJSON(data []byte) error {
	return validateUniqueJSON(data, false, nil, nil, false, Limits{})
}

// ValidateUniqueFoldedJSON additionally treats case-folded object member names
// as duplicates, matching encoding/json's struct-field lookup. Values of fields
// in exactSubtrees use ordinary case-sensitive JSON object semantics.
func ValidateUniqueFoldedJSON(data []byte, exactSubtrees ...string) error {
	var exact map[string]struct{}
	if len(exactSubtrees) > 0 {
		exact = make(map[string]struct{}, len(exactSubtrees))
	}
	for _, field := range exactSubtrees {
		exact[foldJSONName(field)] = struct{}{}
	}
	return validateUniqueJSON(data, true, exact, nil, false, Limits{})
}

// ValidateTypedJSON follows encoding/json struct matching while preserving map
// and RawMessage key semantics, including arbitrary plugin configuration.
func ValidateTypedJSON(data []byte, value any) error {
	return validateUniqueJSON(data, false, nil, reflect.TypeOf(value), true, Limits{})
}

func jsonType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeOf(json.RawMessage{}) {
		return nil
	}
	return t
}

func jsonFieldType(t reflect.Type, key string) reflect.Type {
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Map {
		return t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		if foldJSONName(name) == foldJSONName(key) {
			return f.Type
		}
	}
	return nil
}

func validateUniqueJSON(data []byte, foldNames bool, exactSubtrees map[string]struct{}, rootType reflect.Type, typed bool, limits Limits) error {
	values := 0
	chargeValue := func() error {
		values++
		if limits.MaxValues > 0 && values > limits.MaxValues {
			return errors.New("JSON collection count limit exceeded")
		}
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var frames []uniqueJSONFrame
	rootStarted := false
	rootComplete := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if !rootComplete {
				return errors.New("JSON response is incomplete")
			}
			return nil
		}
		if err != nil {
			return errors.New("JSON response is invalid")
		}
		if rootComplete {
			return errors.New("JSON response contains multiple values")
		}

		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				if err := chargeValue(); err != nil {
					return err
				}
				if limits.MaxDepth > 0 && len(frames) >= limits.MaxDepth {
					return errors.New("JSON depth limit exceeded")
				}
				if err := beginUniqueJSONValue(frames, &rootStarted); err != nil {
					return err
				}
				childFoldNames := foldNames
				if len(frames) > 0 {
					childFoldNames = frames[len(frames)-1].valueFoldNames
				}
				childType := rootType
				if len(frames) > 0 {
					childType = frames[len(frames)-1].valueType
				}
				childType = jsonType(childType)
				valueType := childType
				if childType != nil && (childType.Kind() == reflect.Slice || childType.Kind() == reflect.Array || childType.Kind() == reflect.Map) {
					valueType = childType.Elem()
				}
				if typed {
					childFoldNames = childType != nil && childType.Kind() == reflect.Struct
				}
				frames = append(frames, uniqueJSONFrame{
					object: delimiter == '{', wantKey: delimiter == '{',
					typ: childType, valueType: valueType,
					foldNames: childFoldNames, valueFoldNames: childFoldNames,
				})
			case '}', ']':
				if len(frames) == 0 {
					return errors.New("JSON response contains an unexpected closing delimiter")
				}
				frame := frames[len(frames)-1]
				if frame.object != (delimiter == '}') || frame.object && !frame.wantKey {
					return errors.New("JSON response contains an invalid closing delimiter")
				}
				frames = frames[:len(frames)-1]
				completeUniqueJSONValue(frames, &rootComplete)
			default:
				return errors.New("JSON response contains an unexpected delimiter")
			}
			continue
		}

		if len(frames) > 0 && frames[len(frames)-1].object && frames[len(frames)-1].wantKey {
			key, ok := token.(string)
			if !ok {
				return errors.New("JSON response contains a non-string object key")
			}
			frame := &frames[len(frames)-1]
			if frame.members == nil {
				frame.members = map[string]struct{}{}
			}
			canonicalKey := key
			if frame.foldNames {
				canonicalKey = foldJSONName(key)
			}
			if _, exists := frame.members[canonicalKey]; exists {
				return errors.New("JSON response contains a duplicate object field")
			}
			frame.members[canonicalKey] = struct{}{}
			frame.valueFoldNames = frame.foldNames
			if _, exact := exactSubtrees[foldJSONName(key)]; exact {
				frame.valueFoldNames = false
			}
			if typed {
				frame.valueType = jsonFieldType(frame.typ, key)
			}
			frame.wantKey = false
			continue
		}

		if err := chargeValue(); err != nil {
			return err
		}
		if err := beginUniqueJSONValue(frames, &rootStarted); err != nil {
			return err
		}
		completeUniqueJSONValue(frames, &rootComplete)
	}
}

type uniqueJSONFrame struct {
	typ, valueType reflect.Type
	object         bool
	wantKey        bool
	foldNames      bool
	valueFoldNames bool
	members        map[string]struct{}
}

func beginUniqueJSONValue(frames []uniqueJSONFrame, rootStarted *bool) error {
	if len(frames) == 0 {
		if *rootStarted {
			return errors.New("JSON response contains multiple values")
		}
		*rootStarted = true
		return nil
	}
	if frame := frames[len(frames)-1]; frame.object && frame.wantKey {
		return errors.New("JSON response contains an object value without a key")
	}
	return nil
}

func completeUniqueJSONValue(frames []uniqueJSONFrame, rootComplete *bool) {
	if len(frames) == 0 {
		*rootComplete = true
		return
	}
	frame := &frames[len(frames)-1]
	if frame.object {
		frame.wantKey = true
	}
}

func foldJSONName(name string) string {
	return strings.Map(func(character rune) rune {
		for {
			next := unicode.SimpleFold(character)
			if next <= character {
				return next
			}
			character = next
		}
	}, name)
}

// Limits bound aggregate JSON values and nesting before typed allocation.
// Zero fields leave that dimension unbounded.
type Limits struct{ MaxDepth, MaxValues int }

func ValidateUniqueJSONWithLimits(data []byte, limits Limits) error {
	return validateUniqueJSON(data, false, nil, nil, false, limits)
}

func ValidateTypedJSONWithLimits(data []byte, value any, limits Limits) error {
	return validateUniqueJSON(data, false, nil, reflect.TypeOf(value), true, limits)
}
