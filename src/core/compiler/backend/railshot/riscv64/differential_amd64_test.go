//go:build linux && amd64 && !tinygo && !wago_profile

package riscv64

import (
	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"testing"
)

func TestEstablishedAMD64SameScalarFixtures(t *testing.T) {
	inputs := dirtyInputs()
	for _, w := range []bool{false, true} {
		for _, op := range scalarOps {
			for _, branch := range []bool{false, true} {
				if branch && op < shared.IntEq {
					continue
				}
				data := binaryModule(numericFixture(w, opcode(op, w), branch))
				if _, err := Compile(data); err != nil {
					t.Fatal("RV64 shared admission", err)
				}
				c, err := wago.Compile(data)
				if err != nil {
					t.Fatal(err)
				}
				i, err := wago.Instantiate(c)
				if err != nil {
					c.Close()
					t.Fatal(err)
				}
				for _, v := range inputs {
					want := reference(op, w, v[0], v[1])
					if branch {
						if want == 1 {
							want = 11
						} else {
							want = 22
						}
					}
					got, err := i.Invoke("a", v[0], v[1])
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("amd64 op=%d wide=%v branch=%v a=%x b=%x got=%x want=%x err=%v", op, w, branch, v[0], v[1], got, want, err)
					}
				}
				i.Close()
				c.Close()
			}
		}
	}
	c, err := wago.Compile(binaryModule(pressureFixture(), joinFixture()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	i, err := wago.Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	for _, x := range []uint64{0, 1, 0xffffffff, 0xdeadbeef80000000} {
		got, err := i.Invoke("a", x)
		want := uint64(uint32(uint32(x)*48 + 1176))
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatal("amd64 pressure", got, want, err)
		}
		for _, cond := range []uint64{0, 1, 0xdeadbeef00000000, 0xdeadbeef00000001} {
			want := uint32(x)*3 - 6
			if uint32(cond) != 0 {
				want = uint32(x)*3 + 17
			}
			got, err := i.Invoke("b", x, cond)
			if err != nil || len(got) != 1 || got[0] != uint64(want) {
				t.Fatal("amd64 join", got, want, err)
			}
		}
	}
}

func TestEstablishedAMD64SameDirectCallFixtures(t *testing.T) {
	x := uint32(0x80000000)
	for _, tc := range []struct {
		data []byte
		args []uint64
		want uint64
	}{
		{binaryModule(sumCallFixture(0)), []uint64{dirty(32)}, 528},
		{binaryModule(mutualCallFixture(1, 1), mutualCallFixture(0, 0)), []uint64{dirty(31)}, 0},
		{binaryModule(pressureCallFixture(), pressureFixture()), []uint64{dirty(x), dirty(3)}, uint64((x*98 + 2352) / 3)},
	} {
		c, err := wago.Compile(tc.data)
		if err != nil {
			t.Fatal(err)
		}
		i, err := wago.Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		got, err := i.Invoke("a", tc.args...)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatal("established call result", got, tc.want, err)
		}
		i.Close()
		c.Close()
	}
}

func TestEstablishedAMD64SameIntegerControls(t *testing.T) {
	for _, tc := range integerControls() {
		t.Run(tc.name, func(t *testing.T) {
			c, err := wago.Compile(binaryModule(tc.f))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			i, err := wago.Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer i.Close()
			for range 3 {
				for _, call := range tc.calls {
					got, err := i.Invoke("a", call.args...)
					if call.trap != 0 {
						if err == nil {
							t.Fatal("expected trap", got)
						}
						continue
					}
					if err != nil || len(got) != 1 || got[0] != call.want {
						t.Fatal(got, call.want, err)
					}
				}
			}
		})
	}
	c, err := wago.Compile(binaryModule(deepIntegerFixture()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	i, err := wago.Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	if got, err := i.Invoke("a"); err != nil || len(got) != 1 || got[0] != 320 {
		t.Fatal(got, err)
	}
}
