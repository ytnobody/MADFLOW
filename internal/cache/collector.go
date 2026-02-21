package cache

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type CacheContent struct {
	SystemInstruction string
	ProjectFiles      []ProjectFile
}

type ProjectFile struct {
	Path    string
	Content string
}

type Collector struct {
	repoPath  string
	patterns  []string
	maxFiles  int
	maxSizeKB int64
}

const (
	defaultMaxFiles  = 200
	defaultMaxSizeKB = 1024
)

func NewCollector(repoPath string, patterns []string, maxSizeKB int64) *Collector {
	if len(patterns) == 0 {
		patterns = defaultPatterns()
	}
	if maxSizeKB <= 0 {
		maxSizeKB = defaultMaxSizeKB
	}
	return &Collector{repoPath: repoPath, patterns: patterns, maxFiles: defaultMaxFiles, maxSizeKB: maxSizeKB}
}

func defaultPatterns() []string {
	return []string{"*.go", "internal/**/*.go", "cmd/**/*.go", "prompts/*.md", "SPEC.md", "IMPLEMENTATION_PLAN.md", "madflow.toml", "go.mod"}
}

func (c *Collector) Collect() (*CacheContent, error) {
	var files []ProjectFile
	seen := make(map[string]bool)
	maxSizeBytes := c.maxSizeKB * 1024
	for _, pattern := range c.patterns {
		if len(files) >= c.maxFiles {
			break
		}
		if strings.Contains(pattern, "**") {
			matched, err := c.globDoublestar(pattern, maxSizeBytes)
			if err != nil {
				continue
			}
			for _, f := range matched {
				if !seen[f.Path] {
					seen[f.Path] = true
					files = append(files, f)
				}
			}
		} else {
			fullPattern := filepath.Join(c.repoPath, pattern)
			matches, err := filepath.Glob(fullPattern)
			if err != nil {
				continue
			}
			for _, match := range matches {
				relPath, err := filepath.Rel(c.repoPath, match)
				if err != nil || seen[relPath] || c.shouldExclude(relPath) {
					continue
				}
				info, err := os.Stat(match)
				if err != nil || info.IsDir() || info.Size() > maxSizeBytes {
					continue
				}
				content, err := os.ReadFile(match)
				if err != nil {
					continue
				}
				seen[relPath] = true
				files = append(files, ProjectFile{Path: relPath, Content: string(content)})
			}
		}
	}
	return &CacheContent{ProjectFiles: files}, nil
}

func (c *Collector) globDoublestar(pattern string, maxSizeBytes int64) ([]ProjectFile, error) {
	parts := strings.SplitN(pattern, "/**/", 2)
	var baseDir, filePattern string
	if len(parts) == 2 {
		baseDir = filepath.Join(c.repoPath, parts[0])
		filePattern = parts[1]
	} else {
		baseDir = c.repoPath
		filePattern = strings.TrimPrefix(pattern, "**/")
	}
	var files []ProjectFile
	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n := d.Name()
			if n == ".git" || n == "vendor" || strings.HasPrefix(n, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		relPath, err := filepath.Rel(c.repoPath, path)
		if err != nil || c.shouldExclude(relPath) {
			return nil
		}
		if ok, _ := filepath.Match(filePattern, filepath.Base(path)); !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxSizeBytes {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		files = append(files, ProjectFile{Path: relPath, Content: string(content)})
		return nil
	})
	return files, err
}

func (c *Collector) shouldExclude(relPath string) bool {
	for _, prefix := range []string{".git/", "vendor/"} {
		if strings.HasPrefix(relPath, prefix) {
			return true
		}
	}
	return false
}

func (c *Collector) Hash(content *CacheContent) string {
	h := sha256.New()
	h.Write([]byte(content.SystemInstruction))
	for _, f := range content.ProjectFiles {
		h.Write([]byte(f.Path))
		h.Write([]byte(f.Content))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
