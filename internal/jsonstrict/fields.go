package jsonstrict

import (
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode"
)

const maxDescriptorEntries = 128
const maxDescriptorBytes = 256 << 10

type jsonField struct {
	typ reflect.Type
	id  int
}
type jsonDescriptor struct {
	exact, folded map[string]jsonField
	bytes         int
}

var descriptorCache = struct {
	sync.RWMutex
	entries map[reflect.Type]*jsonDescriptor
	order   []reflect.Type
	bytes   int
}{entries: make(map[reflect.Type]*jsonDescriptor)}

func descriptorFor(t reflect.Type) *jsonDescriptor {
	descriptorCache.RLock()
	d := descriptorCache.entries[t]
	descriptorCache.RUnlock()
	if d != nil {
		return d
	}
	d = buildDescriptor(t)
	// Large host-defined types use a transient descriptor; they cannot evict the
	// entire cache or create permanently retained storage proportional to input.
	if d.bytes > maxDescriptorBytes {
		return d
	}
	descriptorCache.Lock()
	defer descriptorCache.Unlock()
	if existing := descriptorCache.entries[t]; existing != nil {
		return existing
	}
	for len(descriptorCache.order) >= maxDescriptorEntries || descriptorCache.bytes+d.bytes > maxDescriptorBytes {
		oldest := descriptorCache.order[0]
		descriptorCache.order = descriptorCache.order[1:]
		descriptorCache.bytes -= descriptorCache.entries[oldest].bytes
		delete(descriptorCache.entries, oldest)
	}
	descriptorCache.entries[t] = d
	descriptorCache.order = append(descriptorCache.order, t)
	descriptorCache.bytes += d.bytes
	return d
}

func (d *jsonDescriptor) lookupCanonical(key string) (jsonField, string, bool) {
	if field, ok := d.exact[key]; ok {
		canonical := key
		if field.id >= 64 {
			canonical = foldJSONName(key)
		}
		return field, canonical, true
	}
	canonical := foldJSONName(key)
	field, ok := d.folded[canonical]
	return field, canonical, ok
}

type fieldCandidate struct {
	name              string
	typ               reflect.Type
	depth, order      int
	tagged, ambiguous bool
}

func buildDescriptor(t reflect.Type) *jsonDescriptor {
	candidates := make(map[string]fieldCandidate)
	ancestors := make(map[reflect.Type]bool)
	order := 0
	var visit func(reflect.Type, int)
	visit = func(t reflect.Type, depth int) {
		if ancestors[t] {
			return
		}
		ancestors[t] = true
		defer delete(ancestors, t)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			embedded := f.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if !f.IsExported() && (!f.Anonymous || embedded.Kind() != reflect.Struct) {
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			if !validJSONTag(name) {
				name = ""
			}
			if f.Anonymous && name == "" && embedded.Kind() == reflect.Struct {
				visit(embedded, depth+1)
				continue
			}
			tagged := name != ""
			if name == "" {
				name = f.Name
			}
			candidate := fieldCandidate{name: name, typ: f.Type, depth: depth, order: order, tagged: tagged}
			order++
			previous, found := candidates[name]
			switch {
			case !found || depth < previous.depth || depth == previous.depth && tagged && !previous.tagged:
				candidates[name] = candidate
			case depth == previous.depth && tagged == previous.tagged:
				previous.ambiguous = true
				candidates[name] = previous
			}
		}
	}
	visit(t, 0)
	fields := make([]fieldCandidate, 0, len(candidates))
	for _, field := range candidates {
		if !field.ambiguous {
			fields = append(fields, field)
		}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].order < fields[j].order })
	d := &jsonDescriptor{exact: make(map[string]jsonField, len(fields)), folded: make(map[string]jsonField, len(fields)), bytes: 128}
	for _, field := range fields {
		folded := foldJSONName(field.name)
		group, found := d.folded[folded]
		if !found {
			group = jsonField{typ: field.typ, id: len(d.folded)}
			d.folded[folded] = group
		}
		// Exact lookup determines the child type; all case aliases share a duplicate
		// ID, preserving strict typed-object rules even for distinct exact fields.
		d.exact[field.name] = jsonField{typ: field.typ, id: group.id}
		d.bytes += 128 + len(field.name) + len(folded)
	}
	return d
}

func validJSONTag(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", r) || unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}
