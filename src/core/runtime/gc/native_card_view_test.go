package gc

import (
	"testing"
	"unsafe"
)

func TestNativeObjectCardViewTracksAndMutatesStableCard(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{DisableCollection: true}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	array, err := c.NewArrayDefault(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	c.CardMarkArray(array, 0)
	view := c.NativeView()
	if view.ObjectCards == 0 || view.ObjectCardCount != 1 {
		t.Fatalf("native card view = %+v", view)
	}
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&view.ObjectCards))
	words := unsafe.Slice((*uint32)(ptr), NativeObjectCardStride/4)
	if words[NativeObjectCardHandleOffset/4] != handleOf(array) || words[NativeObjectCardStartOffset/4] != 0 || words[NativeObjectCardEndOffset/4] != 15 {
		t.Fatalf("native card words = %v", words)
	}
	if card := c.objectCards[0]; card.handle != handleOf(array) || card.index != 0 || card.end != 15 {
		t.Fatalf("native-published card = %+v", card)
	}
	root := Root(array)
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	c.clearCardMetadata()
	if view.ObjectCards != 0 || view.ObjectCardCount != 0 {
		t.Fatalf("cleared native card view = %+v", view)
	}
}
