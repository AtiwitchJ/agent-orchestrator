package deliverable

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func TestFilesystemWatcherFindsMatchingGlob(t *testing.T) {
	watcher := newFilesystemWatcher(nil)
	tmp := t.TempDir()
	f1 := filepath.Join(tmp, "report.md")
	f2 := filepath.Join(tmp, "output.txt")
	os.WriteFile(f1, []byte("content"), 0644)
	os.WriteFile(f2, []byte("content"), 0644)

	spec := &domain.FilesystemSpec{Glob: "*.md"}
	found, err := watcher.check(context.Background(), spec, tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected to find *.md file")
	}
}

func TestFilesystemWatcherNoMatch(t *testing.T) {
	watcher := newFilesystemWatcher(nil)
	tmp := t.TempDir()
	f1 := filepath.Join(tmp, "report.md")
	os.WriteFile(f1, []byte("content"), 0644)

	spec := &domain.FilesystemSpec{Glob: "*.pdf"}
	found, err := watcher.check(context.Background(), spec, tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected no match for *.pdf")
	}
}

func TestFilesystemWatcherGlobstar(t *testing.T) {
	watcher := newFilesystemWatcher(nil)
	tmp := t.TempDir()
	subdir := filepath.Join(tmp, "reports")
	os.Mkdir(subdir, 0755)
	f1 := filepath.Join(subdir, "report.md")
	os.WriteFile(f1, []byte("content"), 0644)

	spec := &domain.FilesystemSpec{Glob: "**/*.md"}
	found, err := watcher.check(context.Background(), spec, tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected to find **/*.md file in subdir")
	}
}

func TestFilesystemWatcherNilSpec(t *testing.T) {
	watcher := newFilesystemWatcher(nil)
	found, err := watcher.check(context.Background(), nil, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected no match for nil spec")
	}
}
