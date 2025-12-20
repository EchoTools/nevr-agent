package api

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/echotools/nevr-capture/v3/pkg/codecs"
	telemetry "github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
)

// StorageManager handles nevrcap file storage with retention and size limits
type StorageManager struct {
	dir           string
	retention     time.Duration
	maxSize       int64
	logger        Logger
	mu            sync.RWMutex
	activeWriters map[string]*matchWriter
	cleanupTicker *time.Ticker
	stopCh        chan struct{}
}

// matchWriter handles writing frames to a nevrcap file for a specific match
type matchWriter struct {
	matchID   string
	filePath  string
	writer    *codecs.NevrCap
	mu        sync.Mutex
	createdAt time.Time
	lastWrite time.Time
	closed    bool
}

// NewStorageManager creates a new storage manager
func NewStorageManager(dir string, retention time.Duration, maxSize int64, logger Logger) (*StorageManager, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	sm := &StorageManager{
		dir:           dir,
		retention:     retention,
		maxSize:       maxSize,
		logger:        logger,
		activeWriters: make(map[string]*matchWriter),
		stopCh:        make(chan struct{}),
	}

	return sm, nil
}

// Start begins the cleanup routine
func (sm *StorageManager) Start(ctx context.Context) {
	sm.cleanupTicker = time.NewTicker(5 * time.Minute)

	go func() {
		// Run initial cleanup
		sm.cleanup()

		for {
			select {
			case <-ctx.Done():
				return
			case <-sm.stopCh:
				return
			case <-sm.cleanupTicker.C:
				sm.cleanup()
			}
		}
	}()
}

// Stop stops the storage manager
func (sm *StorageManager) Stop() {
	close(sm.stopCh)
	if sm.cleanupTicker != nil {
		sm.cleanupTicker.Stop()
	}

	// Close all active writers
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for matchID, w := range sm.activeWriters {
		if err := w.Close(); err != nil {
			sm.logger.Error("failed to close match writer", "match_id", matchID, "error", err)
		}
	}
	sm.activeWriters = make(map[string]*matchWriter)
}

// WriteFrame writes a frame to the appropriate match file
func (sm *StorageManager) WriteFrame(matchID string, frame *telemetry.LobbySessionStateFrame) error {
	sm.mu.Lock()
	w, exists := sm.activeWriters[matchID]
	if !exists {
		// Create new writer for this match
		filename := fmt.Sprintf("%s_%s.nevrcap", time.Now().Format("2006-01-02_15-04-05"), matchID)
		filePath := filepath.Join(sm.dir, filename)

		writer, err := codecs.NewNevrCapWriter(filePath)
		if err != nil {
			sm.mu.Unlock()
			return fmt.Errorf("failed to create nevrcap writer: %w", err)
		}

		w = &matchWriter{
			matchID:   matchID,
			filePath:  filePath,
			writer:    writer,
			createdAt: time.Now(),
			lastWrite: time.Now(),
		}
		sm.activeWriters[matchID] = w
		sm.logger.Info("created new capture file", "match_id", matchID, "path", filePath)
	}
	sm.mu.Unlock()

	// Write frame
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return fmt.Errorf("writer is closed for match %s", matchID)
	}

	if err := w.writer.WriteFrame(frame); err != nil {
		return fmt.Errorf("failed to write frame: %w", err)
	}
	w.lastWrite = time.Now()

	return nil
}

// CloseMatch closes the writer for a specific match
func (sm *StorageManager) CloseMatch(matchID string) error {
	sm.mu.Lock()
	w, exists := sm.activeWriters[matchID]
	if !exists {
		sm.mu.Unlock()
		return nil
	}
	delete(sm.activeWriters, matchID)
	sm.mu.Unlock()

	return w.Close()
}

// GetMatchFile returns the file path for a completed match
func (sm *StorageManager) GetMatchFile(matchID string) (string, error) {
	// First check active writers
	sm.mu.RLock()
	if _, exists := sm.activeWriters[matchID]; exists {
		sm.mu.RUnlock()
		return "", fmt.Errorf("match %s is still in progress", matchID)
	}
	sm.mu.RUnlock()

	// Search for existing file
	pattern := filepath.Join(sm.dir, fmt.Sprintf("*_%s.nevrcap", matchID))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("failed to search for match file: %w", err)
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("match file not found for %s", matchID)
	}

	return matches[0], nil
}

// IsMatchComplete checks if a match capture is complete (not actively being written)
func (sm *StorageManager) IsMatchComplete(matchID string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	_, exists := sm.activeWriters[matchID]
	return !exists
}

// cleanup removes old files based on retention and size limits
func (sm *StorageManager) cleanup() {
	sm.logger.Debug("running storage cleanup")

	// Get all capture files
	files, err := sm.getFiles()
	if err != nil {
		sm.logger.Error("failed to list capture files", "error", err)
		return
	}

	if len(files) == 0 {
		return
	}

	// Calculate total size
	var totalSize int64
	for _, f := range files {
		totalSize += f.size
	}

	now := time.Now()
	var deleted int

	// Delete files older than retention period
	for _, f := range files {
		if now.Sub(f.modTime) > sm.retention {
			if err := os.Remove(f.path); err != nil {
				sm.logger.Error("failed to delete old file", "path", f.path, "error", err)
			} else {
				sm.logger.Info("deleted old capture file", "path", f.path, "age", now.Sub(f.modTime))
				totalSize -= f.size
				deleted++
			}
		}
	}

	// If still over max size, delete oldest files (echoreplay first, then nevrcap)
	if totalSize > sm.maxSize {
		// Refresh file list after retention cleanup
		files, err = sm.getFiles()
		if err != nil {
			sm.logger.Error("failed to list capture files", "error", err)
			return
		}

		// Sort: echoreplay files first (to delete), then by age (oldest first)
		sort.Slice(files, func(i, j int) bool {
			iIsEchoReplay := strings.HasSuffix(files[i].path, ".echoreplay")
			jIsEchoReplay := strings.HasSuffix(files[j].path, ".echoreplay")
			if iIsEchoReplay != jIsEchoReplay {
				return iIsEchoReplay // echoreplay files come first
			}
			return files[i].modTime.Before(files[j].modTime)
		})

		for _, f := range files {
			if totalSize <= sm.maxSize {
				break
			}

			// Skip active matches
			matchID := extractMatchID(f.path)
			sm.mu.RLock()
			_, isActive := sm.activeWriters[matchID]
			sm.mu.RUnlock()

			if isActive {
				continue
			}

			if err := os.Remove(f.path); err != nil {
				sm.logger.Error("failed to delete file for size limit", "path", f.path, "error", err)
			} else {
				sm.logger.Info("deleted capture file for size limit", "path", f.path, "size", f.size)
				totalSize -= f.size
				deleted++
			}
		}
	}

	if deleted > 0 {
		sm.logger.Info("storage cleanup completed", "deleted", deleted, "remaining_size", totalSize)
	}
}

type fileInfo struct {
	path    string
	size    int64
	modTime time.Time
}

func (sm *StorageManager) getFiles() ([]fileInfo, error) {
	var files []fileInfo

	err := filepath.WalkDir(sm.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		ext := filepath.Ext(path)
		if ext != ".nevrcap" && ext != ".echoreplay" {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil // Skip files we can't stat
		}

		files = append(files, fileInfo{
			path:    path,
			size:    info.Size(),
			modTime: info.ModTime(),
		})

		return nil
	})

	return files, err
}

func extractMatchID(path string) string {
	base := filepath.Base(path)
	// Format: 2006-01-02_15-04-05_matchID.nevrcap
	parts := strings.Split(base, "_")
	if len(parts) >= 3 {
		matchPart := strings.Join(parts[2:], "_")
		return strings.TrimSuffix(strings.TrimSuffix(matchPart, ".nevrcap"), ".echoreplay")
	}
	return ""
}

// Close closes a match writer
func (w *matchWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	w.closed = true
	return w.writer.Close()
}

// GetStorageStats returns current storage statistics
func (sm *StorageManager) GetStorageStats() (totalSize int64, fileCount int, activeMatches int) {
	files, err := sm.getFiles()
	if err != nil {
		return 0, 0, 0
	}

	for _, f := range files {
		totalSize += f.size
	}

	sm.mu.RLock()
	activeMatches = len(sm.activeWriters)
	sm.mu.RUnlock()

	return totalSize, len(files), activeMatches
}
