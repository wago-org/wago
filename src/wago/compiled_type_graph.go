package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// Function-only type graphs require no collector-backed storage. This checks
// their representation; native code and reference binding checks remain separate.
func compiledFunctionOnlyTypeGraph(c *Compiled) bool {
	if c == nil || len(c.Types) == 0 || len(c.GCTypeDescs) != len(c.Types) {
		return false
	}
	for index, typ := range c.Types {
		if typ.Kind != CompositeTypeFunction || typ.HasDescribes || typ.HasDescriptor || c.GCTypeDescs[index].Kind != gc.KindFunc {
			return false
		}
		for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
			for _, value := range values {
				if value.Kind <= ValueTypeV128 {
					if value.Ref != (ReferenceTypeDescriptor{}) {
						return false
					}
					continue
				}
				if value.Kind != ValueTypeReference || value.Ref.Exact {
					return false
				}
				heap := value.Ref.Heap
				if heap.Defined {
					if heap.Abstract != 0 || uint64(heap.TypeIndex) >= uint64(len(c.Types)) || c.Types[heap.TypeIndex].Kind != CompositeTypeFunction {
						return false
					}
				} else if heap.TypeIndex != 0 || heap.Abstract != AbstractHeapFunc && heap.Abstract != AbstractHeapNoFunc && heap.Abstract != AbstractHeapExtern && heap.Abstract != AbstractHeapNoExtern {
					return false
				}
			}
		}
	}
	return true
}
