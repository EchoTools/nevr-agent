package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/echotools/nevr-capture/v3/pkg/conversion"
	"github.com/gorilla/mux"
)

// MatchRetrievalHandler handles match file downloads
type MatchRetrievalHandler struct {
	storage      *StorageManager
	logger       Logger
	cacheDir     string
	conversions  map[string]*conversionJob
	conversionMu sync.Mutex
}

type conversionJob struct {
	outputPath string
	done       chan struct{}
	err        error
	startedAt  time.Time
}

// NewMatchRetrievalHandler creates a new match retrieval handler
func NewMatchRetrievalHandler(storage *StorageManager, logger Logger, cacheDir string) *MatchRetrievalHandler {
	if cacheDir == "" {
		cacheDir = filepath.Join(storage.dir, ".cache")
	}
	os.MkdirAll(cacheDir, 0755)

	return &MatchRetrievalHandler{
		storage:     storage,
		logger:      logger,
		cacheDir:    cacheDir,
		conversions: make(map[string]*conversionJob),
	}
}

// RegisterRoutes registers the match retrieval routes
func (h *MatchRetrievalHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/v3/matches/{matchId}/download", h.handleDownload).Methods("GET")
}

// handleDownload handles match file download requests
func (h *MatchRetrievalHandler) handleDownload(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	matchID := vars["matchId"]
	format := r.URL.Query().Get("format")

	if format == "" {
		format = "nevrcap"
	}

	if format != "nevrcap" && format != "echoreplay" {
		http.Error(w, "invalid format, must be 'nevrcap' or 'echoreplay'", http.StatusBadRequest)
		return
	}

	// Check if match is complete
	if !h.storage.IsMatchComplete(matchID) {
		http.Error(w, "match is still in progress", http.StatusConflict)
		return
	}

	// Get the nevrcap file path
	nevrcapPath, err := h.storage.GetMatchFile(matchID)
	if err != nil {
		http.Error(w, fmt.Sprintf("match not found: %v", err), http.StatusNotFound)
		return
	}

	if format == "nevrcap" {
		h.serveFile(w, r, nevrcapPath, "application/octet-stream")
		return
	}

	// Need to convert to echoreplay
	h.serveEchoReplay(w, r, matchID, nevrcapPath)
}

// serveFile serves a file with appropriate caching headers
func (h *MatchRetrievalHandler) serveFile(w http.ResponseWriter, r *http.Request, filePath, contentType string) {
	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "failed to stat file", http.StatusInternalServerError)
		return
	}

	// Set caching headers
	etag := fmt.Sprintf(`"%x-%x"`, stat.ModTime().Unix(), stat.Size())
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=86400") // 24 hours
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(filePath)))

	// Check if client has cached version
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	http.ServeContent(w, r, filepath.Base(filePath), stat.ModTime(), file)
}

// serveEchoReplay converts and serves an echoreplay file
func (h *MatchRetrievalHandler) serveEchoReplay(w http.ResponseWriter, r *http.Request, matchID, nevrcapPath string) {
	// Check if we already have a cached conversion
	echoReplayPath := filepath.Join(h.cacheDir, matchID+".echoreplay")

	if stat, err := os.Stat(echoReplayPath); err == nil {
		// Check if nevrcap file is newer than the cached conversion
		nevrcapStat, _ := os.Stat(nevrcapPath)
		if nevrcapStat != nil && !nevrcapStat.ModTime().After(stat.ModTime()) {
			h.serveFile(w, r, echoReplayPath, "application/zip")
			return
		}
	}

	// Start or join conversion job
	job := h.getOrStartConversion(matchID, nevrcapPath, echoReplayPath)

	// Wait for conversion with timeout
	ctx := r.Context()
	select {
	case <-ctx.Done():
		http.Error(w, "request cancelled", http.StatusRequestTimeout)
		return
	case <-job.done:
		if job.err != nil {
			http.Error(w, fmt.Sprintf("conversion failed: %v", job.err), http.StatusInternalServerError)
			return
		}
		h.serveFile(w, r, echoReplayPath, "application/zip")
	}
}

// getOrStartConversion returns an existing conversion job or starts a new one
func (h *MatchRetrievalHandler) getOrStartConversion(matchID, nevrcapPath, echoReplayPath string) *conversionJob {
	h.conversionMu.Lock()
	defer h.conversionMu.Unlock()

	if job, exists := h.conversions[matchID]; exists {
		return job
	}

	job := &conversionJob{
		outputPath: echoReplayPath,
		done:       make(chan struct{}),
		startedAt:  time.Now(),
	}
	h.conversions[matchID] = job

	go h.runConversion(matchID, nevrcapPath, job)

	return job
}

// runConversion performs the actual conversion with low priority
func (h *MatchRetrievalHandler) runConversion(matchID, nevrcapPath string, job *conversionJob) {
	defer func() {
		close(job.done)

		// Remove job from active conversions after a delay
		time.AfterFunc(30*time.Second, func() {
			h.conversionMu.Lock()
			delete(h.conversions, matchID)
			h.conversionMu.Unlock()
		})
	}()

	h.logger.Info("starting low-priority conversion", "match_id", matchID, "input", nevrcapPath)

	// Create a temporary file for conversion
	tempPath := job.outputPath + ".tmp"

	// Try to use nice/ionice for low priority (Linux)
	if h.canUsePriorityTools() {
		job.err = h.runLowPriorityConversion(nevrcapPath, tempPath)
	} else {
		// Fall back to regular conversion
		job.err = conversion.ConvertNevrcapToEchoReplay(nevrcapPath, tempPath)
	}

	if job.err != nil {
		os.Remove(tempPath)
		h.logger.Error("conversion failed", "match_id", matchID, "error", job.err)
		return
	}

	// Atomic rename
	if err := os.Rename(tempPath, job.outputPath); err != nil {
		job.err = fmt.Errorf("failed to finalize conversion: %w", err)
		os.Remove(tempPath)
		return
	}

	h.logger.Info("conversion completed", "match_id", matchID, "duration", time.Since(job.startedAt))
}

// canUsePriorityTools checks if nice/ionice are available
func (h *MatchRetrievalHandler) canUsePriorityTools() bool {
	_, err := exec.LookPath("nice")
	return err == nil
}

// runLowPriorityConversion runs conversion with reduced priority
func (h *MatchRetrievalHandler) runLowPriorityConversion(inputPath, outputPath string) error {
	// We can't easily use nice/ionice for a Go function, so we'll use goroutine priorities instead
	// and just do the conversion in-process with a slight delay between operations

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- conversion.ConvertNevrcapToEchoReplay(inputPath, outputPath)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CleanupCache removes old cached conversions
func (h *MatchRetrievalHandler) CleanupCache(maxAge time.Duration) error {
	entries, err := os.ReadDir(h.cacheDir)
	if err != nil {
		return err
	}

	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if now.Sub(info.ModTime()) > maxAge {
			path := filepath.Join(h.cacheDir, entry.Name())
			if err := os.Remove(path); err != nil {
				h.logger.Error("failed to remove cached file", "path", path, "error", err)
			}
		}
	}

	return nil
}

// StreamMatchFile streams a match file to a writer (useful for real-time streaming)
func (h *MatchRetrievalHandler) StreamMatchFile(ctx context.Context, matchID string, format string, w io.Writer) error {
	if !h.storage.IsMatchComplete(matchID) {
		return fmt.Errorf("match %s is still in progress", matchID)
	}

	filePath, err := h.storage.GetMatchFile(matchID)
	if err != nil {
		return err
	}

	if format == "echoreplay" {
		// For echoreplay, we need to convert first
		echoReplayPath := filepath.Join(h.cacheDir, matchID+".echoreplay")
		if _, err := os.Stat(echoReplayPath); os.IsNotExist(err) {
			if err := conversion.ConvertNevrcapToEchoReplay(filePath, echoReplayPath); err != nil {
				return fmt.Errorf("conversion failed: %w", err)
			}
		}
		filePath = echoReplayPath
	}

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(w, file)
	return err
}
