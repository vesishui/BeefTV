package bootstrap

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnedArchiveStreamsEntriesAndPublishesAtomically(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.zip")
	if err := os.WriteFile(dest, []byte("OLD-ZIP"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := OpenOwnedArchive(nil, dest)
	if err != nil {
		t.Fatal(err)
	}
	const entryName = "projects/canvas/files/resource:abc.png"
	if err := archive.OpenEntry(entryName); err != nil {
		t.Fatal(err)
	}
	if err := archive.WriteChunk([]byte("first-")); err != nil {
		t.Fatal(err)
	}
	if err := archive.WriteChunk([]byte("second")); err != nil {
		t.Fatal(err)
	}
	if err := archive.CloseEntry(); err != nil {
		t.Fatal(err)
	}
	if err := archive.AddFile("projects.json", []byte(`{"app":"infinite-canvas"}`)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Finish(); err != nil {
		t.Fatal(err)
	}

	entries := readOwnedArchiveEntries(t, dest)
	if got := string(entries["projects.json"]); got != `{"app":"infinite-canvas"}` {
		t.Fatalf("projects.json = %q", got)
	}
	if got := string(entries[entryName]); got != "first-second" {
		t.Fatalf("%s = %q", entryName, got)
	}
	assertNoOwnedArchiveTemps(t, dir)
}

func TestOwnedArchiveAbortKeepsDestinationAndDropsTemp(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.zip")
	if err := os.WriteFile(dest, []byte("OLD-ZIP"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := OpenOwnedArchive(nil, dest)
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.AddFile("projects.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Abort(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "OLD-ZIP" {
		t.Fatalf("dest = %q", got)
	}
	assertNoOwnedArchiveTemps(t, dir)
	if err := archive.Abort(); err != nil {
		t.Fatalf("abort must be idempotent: %v", err)
	}
}

func TestOwnedArchiveRejectsUseAfterFinish(t *testing.T) {
	archive, err := OpenOwnedArchive(nil, filepath.Join(t.TempDir(), "backup.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := archive.AddFile("projects.json", []byte("{}")); err == nil {
		t.Fatal("a finished archive must reject further entries")
	}
	if err := archive.Finish(); err == nil {
		t.Fatal("a finished archive must not be published twice")
	}
}

func TestOwnedArchiveRequiresOpenEntryBeforeChunks(t *testing.T) {
	archive, err := OpenOwnedArchive(nil, filepath.Join(t.TempDir(), "backup.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Abort()
	if err := archive.WriteChunk([]byte("stray")); err == nil {
		t.Fatal("chunks without an open entry must be rejected")
	}
	if err := archive.CloseEntry(); err == nil {
		t.Fatal("closing without an open entry must be rejected")
	}
}

func TestOwnedArchiveRejectsEscapingEntryNames(t *testing.T) {
	for _, name := range []string{"../escape.png", "projects/../../escape.png", "/absolute.png", `back\slash.png`, "", "..", "C:/windows.png"} {
		archive, err := OpenOwnedArchive(nil, filepath.Join(t.TempDir(), "backup.zip"))
		if err != nil {
			t.Fatal(err)
		}
		if err := archive.OpenEntry(name); err == nil {
			archive.Abort()
			t.Fatalf("entry name %q was accepted", name)
		}
		archive.Abort()
	}
}

func TestOwnedArchiveAddResourceNeedsAReadyRuntime(t *testing.T) {
	archive, err := OpenOwnedArchive(nil, filepath.Join(t.TempDir(), "backup.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Abort()
	if err := archive.AddResource("projects/canvas/files/resource:abc.png", "abc"); err == nil {
		t.Fatal("a nil runtime must not silently produce a broken archive")
	}
}

func readOwnedArchiveEntries(t *testing.T, path string) map[string][]byte {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entries := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		handle, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buffer bytes.Buffer
		if _, err := io.Copy(&buffer, handle); err != nil {
			handle.Close()
			t.Fatal(err)
		}
		handle.Close()
		entries[file.Name] = buffer.Bytes()
	}
	return entries
}

func assertNoOwnedArchiveTemps(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".beeftv-archive-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("leftover archive temp files: %v", matches)
	}
}
