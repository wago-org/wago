package gc

import "testing"

func TestVerifyRejectsPointerFreeHeaderMismatch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  StorageKind
		flags uint32
	}{
		{name: "reference marked pointer-free", kind: StorageRefNull, flags: FlagPointerFree},
		{name: "numeric not marked pointer-free", kind: StorageI32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desc, err := NewStructDesc(0, []StorageKind{tc.kind})
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewCollector(Config{DisableCollection: true}, []TypeDesc{desc})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			object, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			header := c.header(object)
			header.Flags = tc.flags
			c.writeHeader(object, header)
			if err := c.Verify(nil); err == nil {
				t.Fatal("Verify accepted pointer-free header mismatch")
			}
		})
	}
}

func BenchmarkVerifyPointerFreeHeader(b *testing.B) {
	desc, err := NewStructDesc(0, []StorageKind{StorageRefNull})
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{DisableCollection: true}, []TypeDesc{desc})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	if _, err := c.NewStructDefault(0); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.Verify(nil); err != nil {
			b.Fatal(err)
		}
	}
}
