package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type resolvedRunnerRetryer interface {
	runResolvedRunner(string, func() (string, error), func(string) error) error
}

func TestResolvedRunnerRetriesAcrossPluginPublicationRename(t *testing.T) {
	const publicationCount = 2
	root := t.TempDir()
	active := filepath.Join(root, "active")
	target := filepath.Join(active, "bin", "wago")
	builds := map[string]string{active: "old"}
	for publication := 0; publication < publicationCount; publication++ {
		builds[filepath.Join(root, fmt.Sprintf("staged-%d", publication))] = fmt.Sprintf("new-%d", publication)
	}
	for dir, contents := range builds {
		if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bin", "wago"), []byte(contents), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	resolved := make([]chan struct{}, publicationCount)
	publicationGap := make([]chan struct{}, publicationCount)
	finishPublication := make([]chan struct{}, publicationCount)
	publicationDone := make([]chan error, publicationCount)
	for publication := 0; publication < publicationCount; publication++ {
		resolved[publication] = make(chan struct{})
		publicationGap[publication] = make(chan struct{})
		finishPublication[publication] = make(chan struct{})
		publicationDone[publication] = make(chan error, 1)
	}
	stop := make(chan struct{})
	stopped := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		<-stopped
	})
	go func() {
		defer close(stopped)
		for publication := 0; publication < publicationCount; publication++ {
			select {
			case <-resolved[publication]:
			case <-stop:
				return
			}
			backup := filepath.Join(root, fmt.Sprintf("backup-%d", publication))
			if err := os.Rename(active, backup); err != nil {
				publicationDone[publication] <- err
				return
			}
			close(publicationGap[publication])
			select {
			case <-finishPublication[publication]:
			case <-stop:
				return
			}
			staged := filepath.Join(root, fmt.Sprintf("staged-%d", publication))
			if err := os.Rename(staged, active); err != nil {
				publicationDone[publication] <- err
				return
			}
			publicationDone[publication] <- nil
		}
	}()

	resolveCount := 0
	resolve := func() (string, error) {
		if resolveCount > 0 {
			if err := <-publicationDone[resolveCount-1]; err != nil {
				return "", err
			}
		}
		if resolveCount < publicationCount {
			close(resolved[resolveCount])
		}
		resolveCount++
		return target, nil
	}
	launchCount := 0
	launch := func(path string) error {
		publication := launchCount
		launchCount++
		if publication < publicationCount {
			select {
			case <-publicationGap[publication]:
			case err := <-publicationDone[publication]:
				t.Fatalf("publish runner fixture: %v", err)
			}
			_, err := os.ReadFile(path)
			close(finishPublication[publication])
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(contents) != "new-1" {
			return fmt.Errorf("launched runner contents %q, want newly published runner", contents)
		}
		return nil
	}

	path, err := resolve()
	if err != nil {
		t.Fatal(err)
	}
	run := func(path string, _ func() (string, error), launch func(string) error) error {
		return launch(path) // Current behavior: launch the path returned before publication.
	}
	if retryer, ok := any(commandEnvironment{}).(resolvedRunnerRetryer); ok {
		run = retryer.runResolvedRunner
	}
	if err := run(path, resolve, launch); err != nil {
		t.Fatalf("launch across plugin publication: %v", err)
	}
	if resolveCount != publicationCount+1 || launchCount != publicationCount+1 {
		t.Fatalf("resolve/launch counts = %d/%d, want bounded retries for %d publications", resolveCount, launchCount, publicationCount)
	}
}
