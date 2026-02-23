package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
)

const (
	githubReleasesAPI = "https://api.github.com/repos/ytnobody/madflow/releases/latest"
)

// githubRelease represents the GitHub Releases API response.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// githubAsset represents a downloadable asset in a GitHub release.
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// cmdUpgrade upgrades the madflow binary to the latest version.
func cmdUpgrade(currentVersion string) error {
	fmt.Printf("Current version: %s\n", currentVersion)
	fmt.Println("Checking for latest release...")

	release, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to fetch latest release: %w", err)
	}

	latestVersion := release.TagName
	fmt.Printf("Latest version: %s\n", latestVersion)

	if currentVersion != "dev" && currentVersion == latestVersion {
		fmt.Println("Already up to date.")
		return nil
	}

	// Determine the target binary name based on current platform.
	binaryName := getBinaryName()
	fmt.Printf("Looking for asset: %s\n", binaryName)

	// Find the matching asset.
	downloadURL := ""
	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no matching asset found for %s in release %s", binaryName, latestVersion)
	}

	// Get current executable path.
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	fmt.Printf("Downloading %s...\n", downloadURL)
	newBinary, err := downloadBinary(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}
	defer os.Remove(newBinary)

	// Backup current binary.
	backupPath := exePath + ".bak"
	fmt.Printf("Backing up current binary to %s...\n", backupPath)
	if err := copyFile(exePath, backupPath); err != nil {
		return fmt.Errorf("failed to backup binary: %w", err)
	}

	// Replace current binary with the new one.
	fmt.Println("Installing new binary...")
	if err := os.Rename(newBinary, exePath); err != nil {
		// Rename may fail across filesystems; try copy+delete.
		if err2 := copyFile(newBinary, exePath); err2 != nil {
			// Restore backup on failure.
			_ = os.Rename(backupPath, exePath)
			return fmt.Errorf("failed to replace binary: %w (copy also failed: %v)", err, err2)
		}
	}

	// Set executable permission.
	if err := os.Chmod(exePath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	fmt.Printf("Successfully upgraded to %s!\n", latestVersion)
	return nil
}

// fetchLatestRelease retrieves the latest release info from GitHub API.
func fetchLatestRelease() (*githubRelease, error) {
	req, err := http.NewRequest("GET", githubReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "madflow-upgrade/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release JSON: %w", err)
	}
	return &release, nil
}

// getBinaryName returns the expected binary name for the current platform.
// Format: madflow-{os}-{arch} (with .exe suffix on Windows).
func getBinaryName() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	name := fmt.Sprintf("madflow-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// downloadBinary downloads a binary from the given URL to a temporary file
// and returns the path to the temporary file.
func downloadBinary(url string) (string, error) {
	resp, err := http.Get(url) //nolint:gosec // URL is validated from GitHub API
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Determine a meaningful temp file name from the URL.
	parts := strings.Split(url, "/")
	baseName := "madflow-download"
	if len(parts) > 0 {
		baseName = parts[len(parts)-1]
	}

	tmpFile, err := os.CreateTemp("", baseName+"-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write download: %w", err)
	}

	return tmpFile.Name(), nil
}

// copyFile copies a file from src to dst, overwriting dst if it exists.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
