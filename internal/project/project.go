package project

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	madflowDir     = ".madflow"
	projectsFile   = "projects.toml"
	configFileName = "madflow.toml"
)

type Project struct {
	ID      string
	DataDir string
	Paths   []string
}

type projectsRegistry struct {
	Projects map[string]projectEntry `toml:"projects"`
}

type projectEntry struct {
	Paths []string `toml:"paths"`
}

func BaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, madflowDir), nil
}

func DataDir(projectID string) (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, projectID), nil
}

// Init registers a new project and creates its data directory.
func Init(name string, paths []string) error {
	base, err := BaseDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(base, 0755); err != nil {
		return fmt.Errorf("create base dir: %w", err)
	}

	registry, err := loadRegistry(base)
	if err != nil {
		return err
	}

	if registry.Projects == nil {
		registry.Projects = make(map[string]projectEntry)
	}

	absPaths := make([]string, len(paths))
	for i, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("resolve path %s: %w", p, err)
		}
		absPaths[i] = abs
	}

	registry.Projects[name] = projectEntry{Paths: absPaths}

	if err := saveRegistry(base, registry); err != nil {
		return err
	}

	dataDir := filepath.Join(base, name)
	for _, sub := range []string{"issues", "memos"} {
		if err := os.MkdirAll(filepath.Join(dataDir, sub), 0755); err != nil {
			return fmt.Errorf("create data dir: %w", err)
		}
	}

	return nil
}

// Detect finds the project for the current working directory.
func Detect() (*Project, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get cwd: %w", err)
	}

	// Check if madflow.toml exists in cwd
	configPath := filepath.Join(cwd, configFileName)
	if _, err := os.Stat(configPath); err == nil {
		return detectFromConfig(configPath)
	}

	// Check projects.toml registry
	base, err := BaseDir()
	if err != nil {
		return nil, err
	}

	registry, err := loadRegistry(base)
	if err != nil {
		return nil, err
	}

	for name, entry := range registry.Projects {
		for _, p := range entry.Paths {
			if p == cwd {
				dataDir := filepath.Join(base, name)
				return &Project{
					ID:      name,
					DataDir: dataDir,
					Paths:   entry.Paths,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no madflow project found for %s (run 'madflow init' first)", cwd)
}

func detectFromConfig(configPath string) (*Project, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var partial struct {
		Project struct {
			Name string `toml:"name"`
		} `toml:"project"`
	}
	if err := toml.Unmarshal(data, &partial); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if partial.Project.Name == "" {
		return nil, fmt.Errorf("project.name not found in %s", configPath)
	}

	base, err := BaseDir()
	if err != nil {
		return nil, err
	}

	dataDir := filepath.Join(base, partial.Project.Name)
	return &Project{
		ID:      partial.Project.Name,
		DataDir: dataDir,
	}, nil
}

func loadRegistry(base string) (*projectsRegistry, error) {
	path := filepath.Join(base, projectsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &projectsRegistry{}, nil
		}
		return nil, fmt.Errorf("read registry: %w", err)
	}

	var reg projectsRegistry
	if err := toml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	return &reg, nil
}

func saveRegistry(base string, reg *projectsRegistry) error {
	path := filepath.Join(base, projectsFile)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create registry: %w", err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(reg); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	return nil
}
