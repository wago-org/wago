// Example 06: a long-lived runtime service.
//
// The service compiles once and gives each concurrent request a fresh instance.
// Run:
//
//	go run ./examples/06-runtime-service
package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

type service struct {
	runtime *wago.Runtime
	module  *wago.Module
}

func newService() (*service, error) {
	runtime := wago.NewRuntime()
	module, err := runtime.Compile(mods.Add())
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	return &service{runtime: runtime, module: module}, nil
}

func (s *service) add(ctx context.Context, a, b int32) (int32, error) {
	instance, err := s.runtime.Instantiate(ctx, s.module)
	if err != nil {
		return 0, err
	}
	defer instance.Close()

	result, err := instance.Call(ctx, "add", wago.ValueI32(a), wago.ValueI32(b))
	if err != nil {
		return 0, err
	}
	return result[0].I32(), nil
}

func (s *service) close(ctx context.Context) error {
	return errors.Join(s.module.Close(), s.runtime.CloseContext(ctx))
}

func main() {
	service, err := newService()
	if err != nil {
		panic(err)
	}
	defer service.close(context.Background())

	type job struct{ a, b int32 }
	jobs := []job{{1, 2}, {3, 4}}
	results := make(chan int, len(jobs))
	var group sync.WaitGroup

	for _, item := range jobs {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := service.add(context.Background(), item.a, item.b)
			if err != nil {
				panic(err)
			}
			results <- int(result)
		}()
	}

	group.Wait()
	close(results)
	var ordered []int
	for result := range results {
		ordered = append(ordered, result)
	}
	sort.Ints(ordered)
	fmt.Println("results:", ordered)
}
