//go:build (linux || darwin) && arm64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"
)

func TestTailCallARM64CrossInstancePreservesWideArguments(t *testing.T) {
	// return_call_ref is the supported wide cross-instance surface. A direct
	// imported return_call deliberately has a narrower linking contract.
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	for _, slots := range []int{15, 16} {
		t.Run(fmt.Sprintf("slots=%d", slots), func(t *testing.T) {
			// The target catches its own typed throw before returning slot 14. This
			// proves tail staging preserves both the argument and the target EH context.
			producerCode, err := Compile(cfg, wideCrossTailEHProducerModule(slots))
			if err != nil {
				t.Fatal(err)
			}
			defer producerCode.Close()
			producer, err := Instantiate(producerCode, InstantiateOptions{})
			if err != nil {
				t.Fatalf("instantiate wide-tail producer: %v", err)
			}
			defer producer.Close()
			exported, err := producer.ExportedFunc("pick")
			if err != nil {
				t.Fatal(err)
			}

			consumerCode, err := Compile(cfg, wideCrossTailConsumerModule(slots, true))
			if err != nil {
				t.Fatal(err)
			}
			defer consumerCode.Close()
			consumer, err := Instantiate(consumerCode, InstantiateOptions{Imports: testImports("env.pick", exported)})
			if err != nil {
				t.Fatalf("instantiate wide-tail consumer: %v", err)
			}
			defer consumer.Close()

			args := wideCrossTailArgs(slots)
			got, err := consumer.Invoke("run", args...)
			if err != nil || len(got) != 1 || got[0] != args[14] {
				t.Fatalf("cross-instance tail slot 14 = %v, %v; want %#x", got, err, args[14])
			}
		})
	}
}
