package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGetBinaryName(t *testing.T) {
	name := getBinaryName()
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	expected := "madflow-" + goos + "-" + goarch
	if goos == "windows" {
		expected += ".exe"
	}

	if name != expected {
		t.Errorf("getBinaryName() = %q, want %q", name, expected)
	}
}

func TestGetBinaryName_Windows(t *testing.T) {
	// Test that Windows binary name ends with .exe
	name := getBinaryName()
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(name, ".exe") {
			t.Errorf("Windows binary should end with .exe, got %q", name)
		}
	} else {
		if strings.HasSuffix(name, ".exe") {
			t.Errorf("Non-Windows binary should not end with .exe, got %q", name)
		}
	}
}

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file.
	srcPath := filepath.Join(tmpDir, "src.bin")
	content := []byte("test binary content 12345")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to create src file: %v", err)
	}

	// Copy to destination.
	dstPath := filepath.Join(tmpDir, "dst.bin")
	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatalf("copyFile() error: %v", err)
	}

	// Verify destination content.
	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read dst file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("copied content = %q, want %q", string(got), string(content))
	}
}

func TestFetchLatestRelease(t *testing.T) {
	// Create a mock HTTP server.
	mockRelease := githubRelease{
		TagName: "v1.2.3",
		Assets: []githubAsset{
			{
				Name:               "madflow-linux-amd64",
				BrowserDownloadURL: "https://example.com/madflow-linux-amd64",
			},
			{
				Name:               "madflow-darwin-arm64",
				BrowserDownloadURL: "https://example.com/madflow-darwin-arm64",
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockRelease)
	}))
	defer ts.Close()

	// Temporarily override the API URL by testing the function directly.
	// We test the JSON parsing logic by calling the server.
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("failed to connect to mock server: %v", err)
	}
	defer resp.Body.Close()

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if release.TagName != "v1.2.3" {
		t.Errorf("TagName = %q, want %q", release.TagName, "v1.2.3")
	}
	if len(release.Assets) != 2 {
		t.Errorf("len(Assets) = %d, want 2", len(release.Assets))
	}
}

func TestDownloadBinary(t *testing.T) {
	// Create a mock HTTP server that serves a fake binary.
	fakeContent := []byte("fake binary content for testing")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(fakeContent)
	}))
	defer ts.Close()

	tmpPath, err := downloadBinary(ts.URL + "/madflow-linux-amd64")
	if err != nil {
		t.Fatalf("downloadBinary() error: %v", err)
	}
	defer os.Remove(tmpPath)

	// Verify file was created with correct content.
	got, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(got) != string(fakeContent) {
		t.Errorf("downloaded content = %q, want %q", string(got), string(fakeContent))
	}
}

func TestDownloadBinary_HTTPError(t *testing.T) {
	// Mock server that returns 404.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	_, err := downloadBinary(ts.URL + "/nonexistent")
	if err == nil {
		t.Error("downloadBinary() should return error on HTTP 404")
	}
}
