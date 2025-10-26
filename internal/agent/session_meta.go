package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	ErrAPIAccessDisabled = errors.New("API access is disabled on the server")
)

type SessionMeta struct {
	SessionUUID    string `json:"sessionid"`
	GameStatus     string `json:"game_status"`
	MatchType      string `json:"match_type"`
	MapName        string `json:"map_name"`
	IsPrivateMatch bool   `json:"private_match"`
}

func GetSessionMeta(baseURL string) (r SessionMeta, err error) {
	client := &http.Client{
		Timeout: 3 * time.Second, // Overall request timeout
		Transport: &http.Transport{
			ExpectContinueTimeout: 1 * time.Second,
			DialContext: (&net.Dialer{
				Timeout: 1 * time.Second,
			}).DialContext,
		},
	}
	resp, err := client.Get(EndpointSession(baseURL))
	if err != nil {
		// Ignore connection refused errors
		var netErr *net.OpError
		if ok := errors.As(err, &netErr); ok && netErr.Err != nil {
			if sysErr, ok := netErr.Err.(*os.SyscallError); ok && sysErr.Err.Error() == "connection refused" {
				return r, nil
			}
		}
		// Also handle error string for cross-platform compatibility
		if err.Error() != "" && ( // fallback string check
		strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "connectex: No connection could be made because the target machine actively refused it")) {
			return r, nil
		}
		return r, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		// Active session found, proceed to read metadata
	case http.StatusNotFound:
		// There is no active session, return empty SessionMeta
		return r, nil
	case http.StatusInternalServerError:
		// API access is disabled, return empty SessionMeta
		return r, ErrAPIAccessDisabled
	default:
		// Unexpected status code, return an error
		return r, fmt.Errorf("received non-OK response: %d", resp.StatusCode)
	}
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return r, fmt.Errorf("failed to read response body: %v", err)
	}
	response := SessionMeta{}
	if err := json.Unmarshal(buf, &response); err != nil {
		return r, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	if response.SessionUUID == "" {
		return r, fmt.Errorf("session UUID is empty in response: %s", string(buf))
	}
	return response, nil
}
