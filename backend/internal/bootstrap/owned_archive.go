package bootstrap

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// OwnedArchive writes a user-facing ZIP archive incrementally.
//
// Canvas backups reference gigabytes of local media. Materializing such an
// archive in memory (in the WebView or in the Go heap) is what made large
// exports fail, so entries are streamed: media that already lives in the local
// resource store is copied from disk by the backend and never crosses the
// Wails IPC bridge, and smaller in-memory entries are appended in bounded
// chunks. The archive is published with a single atomic rename so a failed or
// cancelled export never truncates an existing file at the chosen path.
type OwnedArchive struct {
	mu      sync.Mutex
	runtime *Runtime
	file    *os.File
	writer  *zip.Writer
	entry   io.Writer
	dest    string
	tmp     string
	closed  bool
}

var errArchiveFinished = errors.New("导出会话已结束")

// OpenOwnedArchive creates an archive in the destination directory.
//
// runtime may be nil for archives that only carry caller-supplied bytes; any
// attempt to append a stored resource then fails instead of panicking.
func OpenOwnedArchive(runtime *Runtime, destPath string) (*OwnedArchive, error) {
	dest, err := sanitizeSaveDestination(destPath)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(filepath.Dir(dest), ".beeftv-archive-*")
	if err != nil {
		return nil, errors.New("无法写入所选位置")
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, errors.New("无法写入所选位置")
	}
	return &OwnedArchive{
		runtime: runtime,
		file:    file,
		writer:  zip.NewWriter(file),
		dest:    dest,
		tmp:     file.Name(),
	}, nil
}

// OpenEntry starts an entry whose bytes are appended with WriteChunk.
func (a *OwnedArchive) OpenEntry(name string) error {
	return a.openEntry(name, time.Time{})
}

// AddFile appends a complete entry in one call.
func (a *OwnedArchive) AddFile(name string, data []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.openEntryLocked(name, time.Time{}); err != nil {
		return err
	}
	if err := a.writeChunkLocked(data); err != nil {
		a.entry = nil
		return err
	}
	a.entry = nil
	return nil
}

// WriteChunk appends bytes to the entry opened by OpenEntry.
func (a *OwnedArchive) WriteChunk(data []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.writeChunkLocked(data)
}

// CloseEntry finishes the entry opened by OpenEntry.
func (a *OwnedArchive) CloseEntry() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errArchiveFinished
	}
	if a.entry == nil {
		return errors.New("导出条目尚未开始")
	}
	a.entry = nil
	return nil
}

// AddResource streams one stored resource file from disk into the archive.
func (a *OwnedArchive) AddResource(name, resourceID string) error {
	runtime := a.runtime
	if runtime == nil || runtime.service == nil {
		return errors.New("本地后端尚未就绪")
	}
	id, err := sanitizeOwnedResourceID(resourceID)
	if err != nil {
		return err
	}
	owner, err := runtime.service.LocalWorkspaceOwner()
	if err != nil {
		return err
	}
	resource, err := runtime.service.Resource(owner.ID, id)
	if err != nil {
		return errors.New("没有可导出的本机文件")
	}
	source, err := openLocalOwnedResourceFile(resource, runtime.cfg.DataDir)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return errors.New("没有可导出的本机文件")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.openEntryLocked(name, info.ModTime()); err != nil {
		return err
	}
	// A megabyte-sized buffer keeps multi-gigabyte copies to a few thousand
	// syscalls while staying far below the memory an in-heap archive needed.
	buffer := make([]byte, 1<<20)
	if _, err := io.CopyBuffer(a.entry, source, buffer); err != nil {
		a.entry = nil
		return errors.New("无法写出压缩包")
	}
	a.entry = nil
	return nil
}

// Finish writes the archive index and atomically publishes it at the
// destination the user chose.
func (a *OwnedArchive) Finish() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errArchiveFinished
	}
	a.closed = true
	a.entry = nil
	if err := a.writer.Close(); err != nil {
		a.discardLocked()
		return errors.New("无法写出压缩包")
	}
	if err := a.file.Sync(); err != nil {
		a.discardLocked()
		return errors.New("无法写出压缩包")
	}
	if err := a.file.Close(); err != nil {
		a.discardLocked()
		return errors.New("无法写出压缩包")
	}
	if err := replaceFile(a.tmp, a.dest); err != nil {
		_ = os.Remove(a.tmp)
		return err
	}
	return nil
}

// Abort drops the temporary archive without touching the destination.
func (a *OwnedArchive) Abort() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	a.entry = nil
	a.discardLocked()
	return nil
}

func (a *OwnedArchive) discardLocked() {
	if a.file != nil {
		_ = a.file.Close()
	}
	if a.tmp != "" {
		_ = os.Remove(a.tmp)
	}
}

func (a *OwnedArchive) openEntry(name string, modified time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.openEntryLocked(name, modified)
}

func (a *OwnedArchive) openEntryLocked(name string, modified time.Time) error {
	if a.closed {
		return errArchiveFinished
	}
	if a.entry != nil {
		return errors.New("上一个导出条目尚未结束")
	}
	clean, err := sanitizeArchiveEntryName(name)
	if err != nil {
		return err
	}
	header := &zip.FileHeader{Name: clean, Method: zip.Store}
	if !modified.IsZero() {
		header.Modified = modified
	}
	entry, err := a.writer.CreateHeader(header)
	if err != nil {
		return errors.New("无法写出压缩包")
	}
	a.entry = entry
	return nil
}

func (a *OwnedArchive) writeChunkLocked(data []byte) error {
	if a.closed {
		return errArchiveFinished
	}
	if a.entry == nil {
		return errors.New("导出条目尚未开始")
	}
	if len(data) == 0 {
		return nil
	}
	if _, err := a.entry.Write(data); err != nil {
		return errors.New("无法写出压缩包")
	}
	return nil
}

// sanitizeArchiveEntryName rejects anything that could escape the extraction
// root or produce an unreadable entry, mirroring the import-side check.
func sanitizeArchiveEntryName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || strings.ContainsRune(trimmed, '\x00') || strings.Contains(trimmed, `\`) {
		return "", errors.New("导出条目名称非法")
	}
	if strings.HasPrefix(trimmed, "/") || (len(trimmed) > 1 && trimmed[1] == ':') {
		return "", errors.New("导出条目名称非法")
	}
	parts := strings.Split(trimmed, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", errors.New("导出条目名称非法")
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "", errors.New("导出条目名称非法")
	}
	return strings.Join(clean, "/"), nil
}
