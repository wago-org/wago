package gc

import (
	"strconv"
	"testing"
)

func TestAddTypesIndependentRootsAvoidPerAppendAllocations(t *testing.T) {
	c, err := NewCollector(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	allocs := testing.AllocsPerRun(128, func() {
		id := TypeID(len(c.types))
		if err := c.AddTypes([]TypeDesc{{ID: id, Kind: KindStruct, Align: 1, Final: true}}); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 1 {
		t.Fatalf("independent root append allocated %.2f times per call; want at most 1", allocs)
	}
	for i := range c.types {
		if got, err := c.TypeSubtype(TypeID(i), TypeID(i)); err != nil || !got {
			t.Fatalf("type %d is not its own subtype: %v, %v", i, got, err)
		}
	}
	if got, err := c.TypeSubtype(TypeID(len(c.types)-1), 0); err != nil || got {
		t.Fatalf("distinct roots share a subtype relation: %v, %v", got, err)
	}
}

func TestAddTypesSuffixValidationAndSubtypeIntervals(t *testing.T) {
	root := TypeDesc{ID: 0, Kind: KindStruct, Align: 1}
	c, err := NewCollector(Config{}, []TypeDesc{root})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cycle := []TypeDesc{
		{ID: 1, Kind: KindStruct, Align: 1, HasSuper: true, Super: 2},
		{ID: 2, Kind: KindStruct, Align: 1, HasSuper: true, Super: 1},
	}
	if err := c.AddTypes(cycle); err == nil {
		t.Fatal("appended supertype cycle accepted")
	}
	if len(c.types) != 1 || c.NativeView().SubtypeIntervalCount != 1 {
		t.Fatal("rejected append changed the type table or native view")
	}
	child := TypeDesc{ID: 1, Kind: KindStruct, Align: 1, HasSuper: true, Super: 0}
	if err := c.AddTypes([]TypeDesc{child}); err != nil {
		t.Fatal(err)
	}
	if got, err := c.TypeSubtype(1, 0); err != nil || !got {
		t.Fatalf("appended child subtype = %v, %v", got, err)
	}
	if err := c.AddTypes([]TypeDesc{{ID: 2, Kind: KindStruct, Align: 1}}); err != nil {
		t.Fatal(err)
	}
	if got, err := c.TypeSubtype(2, 0); err != nil || got {
		t.Fatalf("new root is subtype of earlier root = %v, %v", got, err)
	}
	if got, err := c.TypeSubtype(2, 1); err != nil || got {
		t.Fatalf("new root is subtype of earlier child = %v, %v", got, err)
	}
}

func BenchmarkAddTypesIndependentRoots(b *testing.B) {
	for _, count := range []int{256, 512, 1024} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				c, err := NewCollector(Config{}, nil)
				if err != nil {
					b.Fatal(err)
				}
				for i := range count {
					if err := c.AddTypes([]TypeDesc{{ID: TypeID(i), Kind: KindStruct, Align: 1, Final: true}}); err != nil {
						b.Fatal(err)
					}
				}
				c.Close()
			}
		})
	}
}
