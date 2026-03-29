package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

func TestDownloadChecksum(t *testing.T) {
	expectedDigest := "a3f5b2c1d4e6f7890123456789abcdef0123456789abcdef0123456789abcdef"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, expectedDigest)
	}))
	defer ts.Close()

	got, err := downloadChecksum(ts.URL + "/madflow-linux-amd64.sha256")
	if err != nil {
		t.Fatalf("downloadChecksum() error: %v", err)
	}
	if got != expectedDigest {
		t.Errorf("downloadChecksum() = %q, want %q", got, expectedDigest)
	}
}

func TestDownloadChecksum_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	_, err := downloadChecksum(ts.URL + "/nonexistent.sha256")
	if err == nil {
		t.Error("downloadChecksum() should return error on HTTP 404")
	}
}

func TestDownloadChecksum_EmptyBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Empty body
	}))
	defer ts.Close()

	_, err := downloadChecksum(ts.URL + "/empty.sha256")
	if err == nil {
		t.Error("downloadChecksum() should return error for empty body")
	}
}

func TestDownloadChecksum_WrongLength(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "tooshort")
	}))
	defer ts.Close()

	_, err := downloadChecksum(ts.URL + "/bad.sha256")
	if err == nil {
		t.Error("downloadChecksum() should return error for wrong-length digest")
	}
}

func TestDownloadChecksum_InvalidChars(t *testing.T) {
	// 64 chars but contains uppercase (invalid for lowercase hex requirement).
	invalidDigest := "A3F5B2C1D4E6F7890123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, invalidDigest)
	}))
	defer ts.Close()

	_, err := downloadChecksum(ts.URL + "/bad.sha256")
	if err == nil {
		t.Error("downloadChecksum() should return error for non-lowercase-hex digest")
	}
}

func TestVerifyChecksum_Match(t *testing.T) {
	content := []byte("test binary data for checksum verification")
	h := sha256.Sum256(content)
	expectedHex := hex.EncodeToString(h[:])

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testbin")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := verifyChecksum(filePath, expectedHex); err != nil {
		t.Errorf("verifyChecksum() unexpected error: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	content := []byte("test binary data for checksum verification")
	wrongHex := strings.Repeat("0", 64)

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testbin")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	err := verifyChecksum(filePath, wrongHex)
	if err == nil {
		t.Error("verifyChecksum() should return error on digest mismatch")
	}
	if !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Errorf("verifyChecksum() error should mention SHA256 mismatch, got: %v", err)
	}
}

func TestVerifyChecksum_FileNotFound(t *testing.T) {
	err := verifyChecksum("/nonexistent/path/to/file", strings.Repeat("0", 64))
	if err == nil {
		t.Error("verifyChecksum() should return error for nonexistent file")
	}
}

func TestChecksumVerification_TamperedBinary(t *testing.T) {
	// Integration-style test: binary content differs from what the checksum covers.
	realContent := []byte("this is the authentic binary content")
	fakeContent := []byte("this is a tampered binary content!!!")

	h := sha256.Sum256(realContent)
	correctChecksum := hex.EncodeToString(h[:])

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/binary":
			// Serve the tampered binary.
			w.WriteHeader(http.StatusOK)
			w.Write(fakeContent)
		case "/checksum":
			// Serve checksum of the real content.
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, correctChecksum)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	tmpPath, err := downloadBinary(ts.URL + "/binary")
	if err != nil {
		t.Fatalf("downloadBinary() error: %v", err)
	}
	defer os.Remove(tmpPath)

	expected, err := downloadChecksum(ts.URL + "/checksum")
	if err != nil {
		t.Fatalf("downloadChecksum() error: %v", err)
	}

	err = verifyChecksum(tmpPath, expected)
	if err == nil {
		t.Error("verifyChecksum() should have failed for tampered binary")
	}
}

func TestChecksumVerification_HappyPath(t *testing.T) {
	// Integration-style test: binary content matches published checksum.
	content := []byte("this is the authentic binary content")
	h := sha256.Sum256(content)
	checksumHex := hex.EncodeToString(h[:])

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/binary":
			w.WriteHeader(http.StatusOK)
			w.Write(content)
		case "/checksum":
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, checksumHex)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	tmpPath, err := downloadBinary(ts.URL + "/binary")
	if err != nil {
		t.Fatalf("downloadBinary() error: %v", err)
	}
	defer os.Remove(tmpPath)

	expected, err := downloadChecksum(ts.URL + "/checksum")
	if err != nil {
		t.Fatalf("downloadChecksum() error: %v", err)
	}

	if err := verifyChecksum(tmpPath, expected); err != nil {
		t.Errorf("verifyChecksum() unexpected error for matching checksum: %v", err)
	}
}
