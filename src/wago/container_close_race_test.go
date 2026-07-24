package wago

import (
	"sync"
	"testing"
)

func TestTableCloseSynchronization(t *testing.T) {
	for i := 0; i < 100; i++ {
		table, err := NewTable(1, 1)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(4)
		go func() { defer wg.Done(); <-start; _ = table.Close() }()
		go func() { defer wg.Done(); <-start; _ = table.Close() }()
		go func() { defer wg.Done(); <-start; _ = table.Size() }()
		go func() { defer wg.Done(); <-start; _ = table.funcrefProducerRoots() }()
		close(start)
		wg.Wait()
		if err := table.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTableCloseVersusAttachValidation(t *testing.T) {
	for i := 0; i < 100; i++ {
		table, err := NewTable(1, 1)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		attached := make(chan bool, 1)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			attached <- table.attachImporter(ValFuncRef, nil) == nil
		}()
		go func() { defer wg.Done(); <-start; _ = table.Close() }()
		close(start)
		wg.Wait()
		if <-attached {
			table.detachImporter()
		}
		if err := table.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGlobalCloseSynchronization(t *testing.T) {
	for i := 0; i < 100; i++ {
		t.Run("scalar", func(t *testing.T) {
			global := NewGlobalI64(1, true)
			raceGlobalClose(t, global,
				func() { _ = global.Get() },
				func() { _ = global.Set(2) },
			)
		})
		t.Run("v128", func(t *testing.T) {
			global := NewGlobalV128(V128{1, 2, 3}, true)
			raceGlobalClose(t, global,
				func() { _ = global.GetV128() },
				func() { _ = global.SetV128(V128{9}) },
			)
		})
		t.Run("reference", func(t *testing.T) {
			rt := NewRuntime()
			global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
			if err != nil {
				t.Fatal(err)
			}
			raceGlobalClose(t, global,
				func() { _, _ = global.GetValue() },
				func() { _ = global.SetValue(ValueFuncRef(NullFuncRef())) },
			)
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func raceGlobalClose(t *testing.T, global *Global, read, write func()) {
	t.Helper()
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); <-start; _ = global.Close() }()
	go func() { defer wg.Done(); <-start; _ = global.Close() }()
	go func() { defer wg.Done(); <-start; read() }()
	go func() { defer wg.Done(); <-start; write() }()
	close(start)
	wg.Wait()
	if err := global.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalCloseVersusAttachValidation(t *testing.T) {
	for i := 0; i < 100; i++ {
		rt := NewRuntime()
		global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		attached := make(chan bool, 1)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			attached <- global.attachReferenceImporter(rt.refStore) == nil
		}()
		go func() { defer wg.Done(); <-start; _ = global.Close() }()
		close(start)
		wg.Wait()
		if <-attached {
			global.detachReferenceImporter()
		}
		if err := global.Close(); err != nil {
			t.Fatal(err)
		}
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
