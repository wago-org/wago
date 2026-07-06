// Example 14: resource handles.
//
// Plugins that manage host resources (files, sockets, timers) hand guests opaque
// integer handles instead of Go pointers. HandleTable tracks them with a
// generation counter, so a stale handle fails cleanly instead of aliasing a new
// resource. Run:
//
//	go run ./examples/14-handles
package main

import (
	"errors"
	"fmt"

	wago "github.com/wago-org/wago"
)

// conn is a stand-in host resource that a guest would refer to by handle.
type conn struct {
	name   string
	closed bool
}

func (c *conn) Close() error { c.closed = true; return nil }

func main() {
	table := wago.NewHandleTable()

	// A plugin's "open" host import would insert a resource and return the handle
	// (as an i64) to the guest.
	h := table.Insert("conn", &conn{name: "db-1"})
	fmt.Printf("opened handle = %d, live handles = %d\n", uint64(h), table.Len())

	// A "use" host import looks the handle back up, kind-checked.
	if r, ok := table.Get(h, "conn"); ok {
		fmt.Printf("using resource: %s\n", r.(*conn).name)
	}

	// Wrong kind is rejected.
	if _, ok := table.Get(h, "socket"); !ok {
		fmt.Println("Get with wrong kind correctly rejected")
	}

	// Close releases the resource and invalidates the handle.
	_ = table.Close(h)
	if _, ok := table.Get(h, "conn"); !ok {
		fmt.Println("handle no longer resolves after Close")
	}

	// A stale handle (slot reused by a new resource) does not alias it.
	h2 := table.Insert("conn", &conn{name: "db-2"})
	if _, ok := table.Get(h, "conn"); !ok && h != h2 {
		fmt.Println("stale handle does not alias the reused slot (generation guard)")
	}

	// Double-close is reported, not silently ignored.
	if err := table.Close(h); errors.Is(err, wago.ErrInvalidHandle) {
		fmt.Println("double-close reported as ErrInvalidHandle")
	}
}
