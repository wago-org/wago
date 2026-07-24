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
		global := NewGlobalV128(V128{1, 2, 3}, true)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(8)
		go func() { defer wg.Done(); <-start; _ = global.Close() }()
		go func() { defer wg.Done(); <-start; _ = global.Close() }()
		go func() { defer wg.Done(); <-start; _ = global.Get() }()
		go func() { defer wg.Done(); <-start; _ = global.GetV128() }()
		go func() { defer wg.Done(); <-start; _, _ = global.GetValue() }()
		go func() { defer wg.Done(); <-start; _ = global.Set(1) }()
		go func() { defer wg.Done(); <-start; _ = global.SetV128(V128{9}) }()
		go func() { defer wg.Done(); <-start; _ = global.SetValue(Value{}) }()
		close(start)
		wg.Wait()
		if err := global.Close(); err != nil {
			t.Fatal(err)
		}
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
