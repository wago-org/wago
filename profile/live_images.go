package profile

import (
	"fmt"
	"sort"

	"github.com/wago-org/wago/internal/jitprofile"
)

// liveImages indexes the load addresses once. The Fenwick tree records which
// loads are live, so a sample can find the preceding live address in O(log N).
type liveImages struct {
	bases     []uint64
	images    []*jitprofile.Image
	tree      []int
	positions []int // event index to address-order index
	active    map[uint64]int
	last      *jitprofile.Image
	topBit    int
}

func newLiveImages(events []jitprofile.Event) *liveImages {
	type load struct {
		base  uint64
		event int
	}
	loads := make([]load, 0, len(events))
	for i, event := range events {
		if event.Kind == "load" && event.Image != nil {
			loads = append(loads, load{event.Image.Base, i})
		}
	}
	sort.Slice(loads, func(i, j int) bool {
		if loads[i].base != loads[j].base {
			return loads[i].base < loads[j].base
		}
		return loads[i].event < loads[j].event
	})
	index := &liveImages{
		bases:     make([]uint64, len(loads)),
		images:    make([]*jitprofile.Image, len(loads)),
		tree:      make([]int, len(loads)+1),
		positions: make([]int, len(events)),
		active:    make(map[uint64]int),
	}
	for i, load := range loads {
		index.bases[i] = load.base
		index.positions[load.event] = i
	}
	index.topBit = 1
	for index.topBit<<1 <= len(loads) {
		index.topBit <<= 1
	}
	return index
}

func (index *liveImages) add(position, delta int) {
	for i := position + 1; i < len(index.tree); i += i & -i {
		index.tree[i] += delta
	}
}

func (index *liveImages) count(end int) int {
	n := 0
	for i := end; i > 0; i -= i & -i {
		n += index.tree[i]
	}
	return n
}

// selectRank returns the zero-based position of the one-based live rank.
func (index *liveImages) selectRank(rank int) int {
	position := 0
	for step := index.topBit; step > 0; step >>= 1 {
		if next := position + step; next < len(index.tree) && index.tree[next] < rank {
			position = next
			rank -= index.tree[next]
		}
	}
	return position
}

func (index *liveImages) load(event int, image *jitprofile.Image) int {
	index.last = nil
	if old, ok := index.active[image.ID]; ok {
		index.images[old] = nil
		index.add(old, -1)
		delete(index.active, image.ID)
	}
	position := index.positions[event]
	// An empty mapping cannot contain a sample PC or overlap another mapping.
	if image.Size != 0 {
		index.images[position] = image
		index.add(position, 1)
		index.active[image.ID] = position
	}
	return position
}

func (index *liveImages) retire(id uint64) {
	index.last = nil
	if position, ok := index.active[id]; ok {
		index.images[position] = nil
		index.add(position, -1)
		delete(index.active, id)
	}
}

// checkLoad checks only loads that survive the lifecycle batch. Retirements
// cannot create an overlap, and previously live neighbors were checked before.
func (index *liveImages) checkLoad(position int) error {
	image := index.images[position]
	if image == nil {
		return nil
	}
	if image.Base > ^uint64(0)-image.Size {
		return fmt.Errorf("image address overflow")
	}
	if before := index.count(position); before > 0 {
		previous := index.images[index.selectRank(before)]
		if previous.Base+previous.Size > image.Base {
			return fmt.Errorf("overlapping live images")
		}
	}
	if through := index.count(position + 1); through < len(index.active) {
		next := index.images[index.selectRank(through+1)]
		if image.Base+image.Size > next.Base {
			return fmt.Errorf("overlapping live images")
		}
	}
	return nil
}

func (index *liveImages) lookup(pc uint64) *jitprofile.Image {
	if image := index.last; image != nil && pc >= image.Base && pc-image.Base < image.Size {
		return image
	}
	end := sort.Search(len(index.bases), func(i int) bool { return index.bases[i] > pc })
	if rank := index.count(end); rank > 0 {
		image := index.images[index.selectRank(rank)]
		index.last = image
		return image
	}
	return nil
}
