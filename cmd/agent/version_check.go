package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const (
	githubRepoOwner = "EchoTools"
	githubRepoName  = "nevr-agent"
	githubAPIURL    = "https://api.github.com"
)

// GitHubRelease represents a GitHub release
type GitHubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

func newVersionCheckCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-update",
		Short: "Check if a new version is available",
		Long:  `Queries GitHub releases to check if a newer version of the agent is available.`,
		RunE:  runVersionCheck,
	}

	return cmd
}

func runVersionCheck(cmd *cobra.Command, args []string) error {
	currentVersion := version
	if currentVersion == "" {
		currentVersion = "dev"
	}

	fmt.Printf("Current version: %s\n", currentVersion)

	latestRelease, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	if latestRelease == nil {
		fmt.Println("No releases found.")
		return nil
	}

	fmt.Printf("Latest version:  %s\n", latestRelease.TagName)

	if isNewerVersion(currentVersion, latestRelease.TagName) {
		fmt.Printf("\n🎉 A new version is available!\n")
		fmt.Printf("   Release: %s\n", latestRelease.Name)
		fmt.Printf("   Download: %s\n", latestRelease.HTMLURL)
	} else {
		fmt.Println("\n✓ You are running the latest version.")
	}

	return nil
}

func getLatestRelease() (*GitHubRelease, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", githubAPIURL, githubRepoOwner, githubRepoName)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", fmt.Sprintf("%s/%s", githubRepoName, version))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// No releases found
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

// isNewerVersion compares version strings and returns true if latest is newer than current
func isNewerVersion(current, latest string) bool {
	// Normalize versions by removing 'v' prefix
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")

	// Handle dev versions - always consider releases newer
	if current == "dev" || current == "" {
		return true
	}

	// Simple string comparison for semver-like versions
	// For more robust comparison, consider using a semver library
	currentParts := strings.Split(current, ".")
	latestParts := strings.Split(latest, ".")

	// Pad shorter version with zeros
	for len(currentParts) < 3 {
		currentParts = append(currentParts, "0")
	}
	for len(latestParts) < 3 {
		latestParts = append(latestParts, "0")
	}

	for i := 0; i < 3; i++ {
		// Extract numeric portion (handle versions like "1.2.3-beta")
		currentNum := extractNumeric(currentParts[i])
		latestNum := extractNumeric(latestParts[i])

		if latestNum > currentNum {
			return true
		}
		if latestNum < currentNum {
			return false
		}
	}

	return false
}

func extractNumeric(s string) int {
	// Extract leading numeric portion
	var num int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
		} else {
			break
		}
	}
	return num
}

// CheckForUpdateAsync checks for updates in the background and logs if a new version is available
func CheckForUpdateAsync(logger *zap.Logger) {
	go func() {
		latestRelease, err := getLatestRelease()
		if err != nil {
			logger.Debug("Failed to check for updates", zap.Error(err))
			return
		}

		if latestRelease != nil && isNewerVersion(version, latestRelease.TagName) {
			logger.Info("A new version is available",
				zap.String("current_version", version),
				zap.String("latest_version", latestRelease.TagName),
				zap.String("download_url", latestRelease.HTMLURL))
		}
	}()
}
