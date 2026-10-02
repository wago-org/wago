//go:build tinygo

package wago

import "sync"

// TinyGo's conservative collector needs an explicit process root for the heap
// graph owned only by a queued shutdown goroutine. Entries exist only while the
// corresponding close task is active and are removed by runtimeCloseTask.run.
var runtimeCloseTaskRoots struct {
	sync.Mutex
	active map[*runtimeCloseTask]struct{}
}

func retainRuntimeCloseTask(task *runtimeCloseTask) {
	runtimeCloseTaskRoots.Lock()
	if runtimeCloseTaskRoots.active == nil {
		runtimeCloseTaskRoots.active = make(map[*runtimeCloseTask]struct{})
	}
	runtimeCloseTaskRoots.active[task] = struct{}{}
	runtimeCloseTaskRoots.Unlock()
}

func releaseRuntimeCloseTask(task *runtimeCloseTask) {
	runtimeCloseTaskRoots.Lock()
	delete(runtimeCloseTaskRoots.active, task)
	runtimeCloseTaskRoots.Unlock()
}
