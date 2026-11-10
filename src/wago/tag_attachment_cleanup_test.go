//go:build (linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package wago

import (
	"errors"
	"testing"
)

func TestTagAttachmentCleanupContinuesAfterProducerPanic(t *testing.T) {
	pc := compileStagedExceptionHandling(t, stagedTagProductProducerModule())
	defer pc.Close()
	cc := compileStagedExceptionHandling(t, stagedTagProductConsumerModule(0, 0))
	defer cc.Close()
	var producers [2]*Instance
	var tags [2]*Tag
	for i := range producers {
		var err error
		producers[i], err = Instantiate(pc)
		if err != nil {
			t.Fatal(err)
		}
		defer producers[i].Close()
		tags[i], err = producers[i].ExportedTag("primary")
		if err != nil {
			t.Fatal(err)
		}
	}
	consumer, err := Instantiate(cc, NewImports().Tag("env", "first", tags[0]).Tag("env", "second", tags[1]))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	visited := [2]int{}
	for i, producer := range producers {
		index := i
		producer.referenceLifetime().afterPhysicalRelease(func() { visited[index]++; panic(index) })
		if err := producer.Close(); err != nil || producer.resourcesClosed {
			t.Fatal("actual tag root failed to retain closed producer", err)
		}
	}
	if err := consumer.Close(); !errors.Is(err, ErrCallbackPanic) || visited != [2]int{1, 1} {
		t.Fatal("first producer panic skipped later tag attachment", err, visited)
	}
	for _, in := range []*Instance{producers[0], producers[1], consumer} {
		if !in.resourcesClosed || in.resourceRefs != 0 || in.jm != nil || in.ar != nil || in.eng != nil {
			t.Fatal("tag panic stranded actual native resources")
		}
	}
	if tags[0].importers != 0 || tags[1].importers != 0 {
		t.Fatal("tag importer counts did not drain")
	}
}
