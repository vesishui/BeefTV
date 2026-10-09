package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"infinite-canvas/backend/internal/bootstrap"
)

// Streamed archive bindings.
//
// Canvas backups can reference gigabytes of local media. Sending such an
// archive through the single-shot SaveOwnedArtifact binding meant building the
// whole ZIP in the WebView and then base64-encoding it across the IPC bridge,
// which is why large exports never finished. These methods keep the archive on
// disk: media that already lives in the local resource store is copied by the
// backend, and small in-memory entries are appended in bounded chunks.

// BeginOwnedArchive asks the user where to save and opens an archive session.
// An empty session id means the save panel was cancelled.
func (a *DesktopApp) BeginOwnedArchive(fileName string) (string, error) {
	ctx, err := a.dialogContext()
	if err != nil {
		return "", err
	}
	path, err := a.promptSavePath(ctx, bootstrap.SanitizeSaveFileName(fileName))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	if err := ctx.Err(); err != nil {
		return "", errors.New("应用已关闭，无法保存文件")
	}
	archive, err := a.openArchive(path)
	if err != nil {
		return "", err
	}
	sessionID, err := newArchiveSessionID()
	if err != nil {
		_ = archive.Abort()
		return "", err
	}
	a.archiveMu.Lock()
	if a.archiveSessions == nil {
		a.archiveSessions = make(map[string]*bootstrap.OwnedArchive)
	}
	a.archiveSessions[sessionID] = archive
	a.archiveMu.Unlock()
	return sessionID, nil
}

// BeginOwnedArchiveEntry starts an archive entry that is filled by chunks.
func (a *DesktopApp) BeginOwnedArchiveEntry(sessionID string, entryName string) error {
	archive, err := a.archiveSession(sessionID)
	if err != nil {
		return err
	}
	return archive.OpenEntry(entryName)
}

// AppendOwnedArchiveChunk appends bytes to the entry currently open.
func (a *DesktopApp) AppendOwnedArchiveChunk(sessionID string, data []byte) error {
	archive, err := a.archiveSession(sessionID)
	if err != nil {
		return err
	}
	return archive.WriteChunk(data)
}

// EndOwnedArchiveEntry closes the entry currently open.
func (a *DesktopApp) EndOwnedArchiveEntry(sessionID string) error {
	archive, err := a.archiveSession(sessionID)
	if err != nil {
		return err
	}
	return archive.CloseEntry()
}

// AppendOwnedArchiveResource streams one stored resource file into the archive
// without routing its bytes through the frontend.
func (a *DesktopApp) AppendOwnedArchiveResource(sessionID string, entryName string, resourceID string) error {
	archive, err := a.archiveSession(sessionID)
	if err != nil {
		return err
	}
	return archive.AddResource(entryName, resourceID)
}

// FinishOwnedArchive publishes the archive at the chosen destination. The
// session is released whether publishing succeeds or fails.
func (a *DesktopApp) FinishOwnedArchive(sessionID string) (bool, error) {
	archive, err := a.takeArchiveSession(sessionID)
	if err != nil {
		return false, err
	}
	if err := archive.Finish(); err != nil {
		return false, err
	}
	return true, nil
}

// AbortOwnedArchive releases a session and deletes its temp file. Releasing an
// unknown or already finished session is a no-op so the frontend can always
// clean up in a deferred path.
func (a *DesktopApp) AbortOwnedArchive(sessionID string) error {
	archive, err := a.takeArchiveSession(sessionID)
	if err != nil {
		return nil
	}
	return archive.Abort()
}

func (a *DesktopApp) archiveSession(sessionID string) (*bootstrap.OwnedArchive, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil, errors.New("导出会话无效")
	}
	a.archiveMu.Lock()
	defer a.archiveMu.Unlock()
	archive := a.archiveSessions[id]
	if archive == nil {
		return nil, errors.New("导出会话已结束")
	}
	return archive, nil
}

func (a *DesktopApp) takeArchiveSession(sessionID string) (*bootstrap.OwnedArchive, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil, errors.New("导出会话无效")
	}
	a.archiveMu.Lock()
	defer a.archiveMu.Unlock()
	archive := a.archiveSessions[id]
	if archive == nil {
		return nil, errors.New("导出会话已结束")
	}
	delete(a.archiveSessions, id)
	return archive, nil
}

func (a *DesktopApp) abortArchiveSessions() {
	a.archiveMu.Lock()
	sessions := a.archiveSessions
	a.archiveSessions = nil
	a.archiveMu.Unlock()
	for _, archive := range sessions {
		_ = archive.Abort()
	}
}

func (a *DesktopApp) openArchive(destPath string) (*bootstrap.OwnedArchive, error) {
	a.mu.RLock()
	open := a.openOwnedArchive
	a.mu.RUnlock()
	if open != nil {
		return open(destPath)
	}
	return bootstrap.OpenOwnedArchive(a.runtime(), destPath)
}

func newArchiveSessionID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", errors.New("无法开始导出")
	}
	return hex.EncodeToString(buffer), nil
}
