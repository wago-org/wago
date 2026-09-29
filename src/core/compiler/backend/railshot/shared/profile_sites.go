package shared

import (
	"fmt"

	"github.com/wago-org/wago/internal/jitprofile"
)

type NativeCodeSite = jitprofile.CodeSite

// CodeSites records non-overlapping emission sites. Rewind follows tentative
// native-code rollback; finalization subsequently projects the surviving spans.
type CodeSites struct{ ranges []NativeCodeSite }

func (s *CodeSites) Add(start, end int, kind string) {
	if start == end {
		return
	}
	if start < 0 || end < start || kind == "" || len(s.ranges) != 0 && s.ranges[len(s.ranges)-1].Offset+s.ranges[len(s.ranges)-1].Size > uint64(start) {
		panic("invalid compiler code-site emission")
	}
	s.ranges = append(s.ranges, NativeCodeSite{Offset: uint64(start), Size: uint64(end - start), Kind: kind})
}

func (s *CodeSites) Rewind(at int) {
	for len(s.ranges) != 0 {
		last := &s.ranges[len(s.ranges)-1]
		if last.Offset >= uint64(at) {
			s.ranges = s.ranges[:len(s.ranges)-1]
			continue
		}
		if last.Offset+last.Size > uint64(at) {
			last.Size = uint64(at) - last.Offset
		}
		break
	}
}

func (s *CodeSites) Ranges() []NativeCodeSite { return s.ranges }

// RemapNativeCodeSites retains distinct sites even when deletion makes them
// adjacent. Deleted instructions lose their sites; unknown gaps stay unknown.
func RemapNativeCodeSites(sites []NativeCodeSite, mapper sourceRangeMapper) ([]NativeCodeSite, error) {
	if err := jitprofile.ValidateCodeSites(sites, ^uint64(0)); err != nil {
		return nil, err
	}
	out := make([]NativeCodeSite, 0, len(sites))
	for i, site := range sites {
		end := site.Offset + site.Size
		if uint64(int(site.Offset)) != site.Offset || uint64(int(end)) != end || int(end) < 0 {
			return nil, fmt.Errorf("native compiler site %d exceeds offset domain", i)
		}
		start, finish, ok := mapper.MapRange(int(site.Offset), int(end))
		if !ok || start < 0 || finish < start {
			return nil, fmt.Errorf("unmappable native compiler site %d", i)
		}
		if start != finish {
			site.Offset, site.Size = uint64(start), uint64(finish-start)
			out = append(out, site)
		}
	}
	return out, nil
}
