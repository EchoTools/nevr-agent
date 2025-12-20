package agent

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/echotools/nevr-capture/v3/pkg/events"
	"github.com/echotools/nevr-capture/v3/pkg/processing"
	"go.uber.org/zap"
)

// PollerConfig holds configuration for frame polling and filtering
type PollerConfig struct {
	AllFrames     bool     // Send all frames, not just event frames
	FPS           int      // Target frames per second for streaming (0 = use interval)
	IncludeModes  []string // Only stream these game modes
	ExcludeModes  []string // Exclude these game modes from streaming
	ExcludeBones  bool     // Exclude player bone data
	ActiveOnly    bool     // Only stream frames during active gameplay
	ExcludePaused bool     // Exclude paused frames (only with ActiveOnly)
	IdleFPS       int      // Frame rate for non-gametime frames
}

// shouldStreamMode checks if the given match_type should be streamed based on include/exclude filters
func (c *PollerConfig) shouldStreamMode(matchType string) bool {
	matchType = strings.ToLower(matchType)

	// If include modes specified, only allow those
	if len(c.IncludeModes) > 0 {
		for _, mode := range c.IncludeModes {
			if strings.ToLower(mode) == matchType {
				return true
			}
		}
		return false
	}

	// If exclude modes specified, block those
	if len(c.ExcludeModes) > 0 {
		for _, mode := range c.ExcludeModes {
			if strings.ToLower(mode) == matchType {
				return false
			}
		}
	}

	return true
}

// isActiveGameplay checks if the game status indicates active gameplay
func isActiveGameplay(gameStatus string) bool {
	return gameStatus == "playing"
}

// isPausedState checks if the game is in a paused state
func isPausedState(gameStatus string) bool {
	return gameStatus == "round_paused" || gameStatus == "paused"
}

var (
	EndpointSession = func(baseURL string) string {
		return baseURL + "/session"
	}

	EndpointPlayerBones = func(baseURL string) string {
		return baseURL + "/player_bones"
	}
)

func NewHTTPFramePoller(ctx context.Context, logger *zap.Logger, client *http.Client, baseURL string, interval time.Duration, session FrameWriter, pollerCfg PollerConfig) {

	// Start a goroutine to fetch data from the URLs at the specified interval

	// Use FPS override if specified
	if pollerCfg.FPS > 0 {
		interval = time.Second / time.Duration(pollerCfg.FPS)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Calculate idle interval for non-gametime frames
	idleInterval := interval
	if pollerCfg.IdleFPS > 0 {
		idleInterval = time.Second / time.Duration(pollerCfg.IdleFPS)
	}

	var (
		wg                sync.WaitGroup
		sessionURL        = EndpointSession(baseURL)
		playerBonesURL    = EndpointPlayerBones(baseURL)
		processor         = processing.NewWithDetector(events.New(events.WithSynchronousProcessing()))
		sessionBuffer     = bytes.NewBuffer(make([]byte, 0, 64*1024)) // 64KB buffer
		playerBonesBuffer = bytes.NewBuffer(make([]byte, 0, 64*1024)) // 64KB buffer
		lastGameStatus    string
		isIdle            bool
	)

	requestCount := 0
	dataWritten := 0

	defer session.Close()

	go func() {
		<-ctx.Done()
		logger.Debug("HTTP frame poller done", zap.Int("request_count", requestCount), zap.Int("data_written", dataWritten))
	}()

	enableDebugLogging := logger.Core().Enabled(zap.DebugLevel)
	timeoutTimer := time.NewTimer(5 * time.Second)
	for {

		select {
		case <-ctx.Done():
			return
		case <-timeoutTimer.C:
			logger.Debug("HTTP frame poller timeout, stopping", zap.Int("request_count", requestCount), zap.Int("data_written", dataWritten))
			return
		case <-ticker.C:
		}

		wg.Add(2)
		// Reset the buffers
		for url, buf := range map[string]*bytes.Buffer{
			sessionURL:     sessionBuffer,
			playerBonesURL: playerBonesBuffer,
		} {
			buf.Reset()
			requestCount++
			go func() {
				defer wg.Done()
				resp, err := client.Get(url)
				if err != nil {
					if enableDebugLogging {
						logger.Debug("Failed to fetch data from URL", zap.String("url", url), zap.Error(err))
					}
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					if resp.StatusCode == http.StatusNotFound {
						if enableDebugLogging {
							// The game is in transition. Try again after a slight delay.
							logger.Debug("Received 404 Not Found from URL, likely game transition", zap.String("url", url))
						}
						time.Sleep(500 * time.Millisecond)
						return
					}

					logger.Debug("Received unexpected response code response from URL", zap.String("url", url), zap.Int("status_code", resp.StatusCode), zap.String("response_body", resp.Status))
					// If the response is not OK, skip processing this URL
					time.Sleep(500 * time.Millisecond)
					return
				}

				// Use a buffer to read the response body
				n, err := io.Copy(buf, resp.Body)
				if err != nil {
					logger.Warn("Failed to read response body", zap.String("url", url), zap.Error(err))
					return
				}
				dataWritten += int(n)
			}()
		}

		wg.Wait()

		// Check if the context is done before processing the data
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Skip processing if no session data was received
		if sessionBuffer.Len() == 0 {
			continue
		}

		frame, err := processor.ProcessAndDetectEvents(sessionBuffer.Bytes(), playerBonesBuffer.Bytes(), time.Now().Add(time.Millisecond))
		if err != nil {
			logger.Debug("Failed to process frame", zap.Error(err))
			continue
		}

		// Collect any events detected synchronously and attach them to the frame
		select {
		case detectedEvents := <-processor.EventsChan():
			frame.Events = append(frame.Events, detectedEvents...)
		default:
			// No events detected
		}

		// Apply frame filtering based on PollerConfig
		var gameStatus string
		var matchType string
		if frame.Session != nil {
			gameStatus = frame.Session.GetGameStatus()
			matchType = frame.Session.GetMatchType()
		}

		// Check if game mode should be streamed
		if !pollerCfg.shouldStreamMode(matchType) {
			continue
		}

		// Check active-only filter
		if pollerCfg.ActiveOnly {
			if !isActiveGameplay(gameStatus) {
				// Check exclude-paused (only meaningful with active-only)
				if pollerCfg.ExcludePaused && isPausedState(gameStatus) {
					continue
				}
				// For non-active, non-paused states, skip if active-only
				if !isPausedState(gameStatus) {
					continue
				}
			}
		}

		// If not AllFrames, only send frames with events
		if !pollerCfg.AllFrames && len(frame.Events) == 0 {
			continue
		}

		// Exclude bones if configured
		if pollerCfg.ExcludeBones {
			frame.PlayerBones = nil
		}

		// Adjust ticker interval based on game state
		newIsIdle := !isActiveGameplay(gameStatus)
		if newIsIdle != isIdle {
			isIdle = newIsIdle
			if isIdle && pollerCfg.IdleFPS > 0 && pollerCfg.IdleFPS != pollerCfg.FPS {
				ticker.Reset(idleInterval)
				logger.Debug("Switched to idle polling rate", zap.Duration("interval", idleInterval))
			} else if !isIdle {
				ticker.Reset(interval)
				logger.Debug("Switched to active polling rate", zap.Duration("interval", interval))
			}
		}
		lastGameStatus = gameStatus
		_ = lastGameStatus // suppress unused warning

		// Write the data to the FrameWriter
		if err := session.WriteFrame(frame); err != nil {
			logger.Error("Failed to write frame data",
				zap.Error(err))
			continue
		}
		timeoutTimer.Reset(5 * time.Second) // Reset the timer for the next iteration
	}
}
