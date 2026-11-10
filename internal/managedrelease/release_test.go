package managedrelease

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wago-org/wago/internal/atomicfile"
)

func testRelease(t *testing.T, launcher, version string) *Release {
	t.Helper()
	r, err := Prepare(launcher, version, func(binary, source string) error {
		if err := os.MkdirAll(source, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte(version), 0644); err != nil {
			return err
		}
		return os.WriteFile(binary, []byte(version), 0755)
	}, func(binary string) error { _, err := os.Stat(binary); return err })
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestReleasePublicationFailuresRetainPair(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), executableName())
	old := testRelease(t, launcher, "old")
	if err := Publish(old, nil, nil); err != nil {
		t.Fatal(err)
	}
	next := testRelease(t, launcher, "new")
	fail := errors.New("injected publication failure")
	for _, hooks := range []*atomicfile.Hooks{{Replace: func(string, string) error { return fail }}, nil} {
		var bootstrap func() (func() error, error)
		if hooks == nil {
			bootstrap = func() (func() error, error) { return nil, fail }
		}
		if err := Publish(next, bootstrap, hooks); !errors.Is(err, fail) {
			t.Fatalf("error = %v", err)
		}
		selected, err := SelectedBinary(launcher)
		if err != nil || selected != old.Binary() {
			t.Fatalf("selected %q: %v", selected, err)
		}
		for _, r := range []*Release{old, next} {
			if data, err := os.ReadFile(filepath.Join(r.Source(), "go.mod")); err != nil || string(data) != r.Version {
				t.Fatalf("retained pair %s: %q %v", r.Version, data, err)
			}
		}
	}
}

func TestReleaseMarkerFailureRestoresSelection(t *testing.T) {
	for _, initial := range []bool{false, true} {
		name := "existing selection"
		if initial {
			name = "first selection"
		}
		t.Run(name, func(t *testing.T) {
			launcher := filepath.Join(t.TempDir(), executableName())
			var oldData []byte
			if !initial {
				old := testRelease(t, launcher, "old")
				if err := Publish(old, nil, nil); err != nil {
					t.Fatal(err)
				}
				var err error
				_, oldData, err = readRecord(old.Root)
				if err != nil {
					t.Fatal(err)
				}
			}
			next := testRelease(t, launcher, "new")
			marker := filepath.Join(next.Directory, publishedFile)
			// A directory is a deterministic marker-publication failure on all OSes.
			if err := os.Mkdir(marker, 0755); err != nil {
				t.Fatal(err)
			}
			if err := Publish(next, nil, nil); err == nil {
				t.Fatal("marker publication unexpectedly succeeded")
			}
			_, got, err := readRecord(next.Root)
			if initial {
				if !os.IsNotExist(err) {
					t.Fatalf("failed first publication left selection: %v", err)
				}
			} else if err != nil || string(got) != string(oldData) {
				t.Fatalf("prior selection not restored: %s, %v", got, err)
			}
			if _, err := os.Stat(next.Source()); err != nil {
				t.Fatalf("staged recovery source lost: %v", err)
			}
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
			if err := Publish(next, nil, nil); err != nil {
				t.Fatalf("retry publication: %v", err)
			}
			for _, version := range []string{"later", "latest"} {
				if err := Publish(testRelease(t, launcher, version), nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(next.Directory); !os.IsNotExist(err) {
				t.Fatalf("successfully retried release was not pruned: %v", err)
			}
		})
	}
}

func TestReleaseBootstrapRollbackRestoresLauncher(t *testing.T) {
	for _, state := range []string{"first", "legacy", "managed"} {
		for _, failure := range []string{"bootstrap", "marker"} {
			t.Run(state+"/"+failure, func(t *testing.T) {
				root := t.TempDir()
				launcher := filepath.Join(root, executableName())
				source := filepath.Join(root, "installer")
				if err := os.WriteFile(source, []byte("new dispatcher"), 0755); err != nil {
					t.Fatal(err)
				}
				var previousMode os.FileMode
				if state != "first" {
					if err := os.WriteFile(launcher, []byte("old launcher"), 0750); err != nil {
						t.Fatal(err)
					}
					info, err := os.Stat(launcher)
					if err != nil {
						t.Fatal(err)
					}
					previousMode = info.Mode().Perm()
				}
				var oldData []byte
				if state == "managed" {
					if err := Publish(testRelease(t, launcher, "old"), nil, nil); err != nil {
						t.Fatal(err)
					}
					var err error
					_, oldData, err = readRecord(root)
					if err != nil {
						t.Fatal(err)
					}
				}
				next := testRelease(t, launcher, "next")
				if failure == "marker" {
					if err := os.Mkdir(filepath.Join(next.Directory, publishedFile), 0755); err != nil {
						t.Fatal(err)
					}
				}
				injected := errors.New("bootstrap failed after replacement")
				bootstrap := func() (func() error, error) {
					undo, err := BootstrapLauncher(next, source, launcher)
					if err == nil && failure == "bootstrap" {
						err = injected
					}
					return undo, err
				}
				if err := Publish(next, bootstrap, nil); err == nil || (failure == "bootstrap" && !errors.Is(err, injected)) {
					t.Fatalf("publication failure: %v", err)
				}
				data, err := os.ReadFile(launcher)
				if state == "first" {
					if !os.IsNotExist(err) {
						t.Fatalf("new dispatcher remains after rollback: %v", err)
					}
				} else {
					if err != nil || string(data) != "old launcher" {
						t.Fatalf("old launcher not restored: %q, %v", data, err)
					}
					info, err := os.Stat(launcher)
					if err != nil || info.Mode().Perm() != previousMode {
						t.Fatalf("launcher permissions not restored: %v, %v", info, err)
					}
				}
				_, selected, err := readRecord(root)
				if oldData == nil {
					if !os.IsNotExist(err) {
						t.Fatalf("selection remains: %v", err)
					}
				} else if err != nil || string(selected) != string(oldData) {
					t.Fatalf("old selection not restored: %s, %v", selected, err)
				}
			})
		}
	}
}

func TestReleaseBootstrapRollbackReportsRetainedBackup(t *testing.T) {
	root := t.TempDir()
	launcher, source := filepath.Join(root, executableName()), filepath.Join(root, "installer")
	for path, contents := range map[string]string{launcher: "old", source: "new"} {
		if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
			t.Fatal(err)
		}
	}
	next := testRelease(t, launcher, "next")
	injected := errors.New("post-bootstrap failure")
	err := Publish(next, func() (func() error, error) {
		undo, err := BootstrapLauncher(next, source, launcher)
		if err != nil {
			return undo, err
		}
		if err := os.Remove(launcher); err != nil {
			return undo, err
		}
		if err := os.Mkdir(launcher, 0755); err != nil {
			return undo, err
		}
		return undo, injected
	}, nil)
	backup := filepath.Join(next.Directory, "previous-launcher")
	if !errors.Is(err, injected) || !strings.Contains(err.Error(), backup) {
		t.Fatalf("rollback must report original failure and backup path: %v", err)
	}
	if data, err := os.ReadFile(backup); err != nil || string(data) != "old" {
		t.Fatalf("backup not retained: %q, %v", data, err)
	}
}
func TestReleaseSelectionKeepsRunningSource(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), executableName())
	old := testRelease(t, launcher, "old")
	next := testRelease(t, launcher, "new")
	if err := Publish(old, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := Publish(next, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := SourceForExecutable(old.Binary()); got != old.Source() {
		t.Fatalf("running old source changed to %q", got)
	}
	if Launcher(old.Binary()) != launcher {
		t.Fatal("old payload lost launcher location")
	}
	record, _, err := readRecord(filepath.Dir(launcher))
	if err != nil || record.Previous != filepath.Base(old.Directory) {
		t.Fatalf("rollback record %+v: %v", record, err)
	}
}
func TestConcurrentReleasePublication(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), executableName())
	a, b := testRelease(t, launcher, "a"), testRelease(t, launcher, "b")
	if err := Publish(a, nil, nil); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	for _, release := range []*Release{a, b} {
		wg.Add(1)
		go func(r *Release) {
			defer wg.Done()
			for i := 0; i < 8; i++ {
				if err := Publish(r, nil, nil); err != nil {
					t.Error(err)
					return
				}
			}
		}(release)
	}
	for i := 0; i < 32; i++ {
		binary, err := SelectedBinary(launcher)
		if err != nil {
			t.Fatal(err)
		}
		contents, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		source, err := os.ReadFile(filepath.Join(SourceForExecutable(binary), "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != string(source) {
			t.Fatalf("mixed pair %q / %q", contents, source)
		}
	}
	wg.Wait()
}
