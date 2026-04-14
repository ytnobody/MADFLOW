package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ytnobody/madflow/internal/chatlog"
	"github.com/ytnobody/madflow/internal/config"
	"github.com/ytnobody/madflow/internal/orchestrator"
	"github.com/ytnobody/madflow/internal/project"
	"github.com/ytnobody/madflow/prompts"
)

// version is set via ldflags at build time (e.g., -ldflags "-X main.version=v1.2.3").
var version = "dev"

const usage = `Usage: madflow <command> [options]

Commands:
  init                      Initialize a new project
  start                     Start all agents
  use <preset>              Switch the active model preset in madflow.toml
                            Presets: claude, gemini, claude-cheap, gemini-cheap, hybrid, hybrid-cheap,
                                     claude-api-standard, claude-api-cheap (require ANTHROPIC_API_KEY)
  version                   Show current version
  upgrade                   Upgrade madflow to the latest version
`

// ANSI color codes for role-based coloring.
var roleColors = map[string]string{
	"superintendent": "\033[31m", // red
	"engineer":       "\033[33m", // yellow
	"orchestrator":   "\033[37m", // white
}

const colorReset = "\033[0m"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit()
	case "start":
		err = cmdStart()
	case "version", "--version", "-v":
		fmt.Printf("madflow %s\n", version)
		return
	case "use":
		preset := ""
		if len(os.Args) >= 3 {
			preset = os.Args[2]
		}
		err = cmdUse(preset)
	case "upgrade":
		err = cmdUpgrade(version)
	case "help", "--help", "-h":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdInit() error {
	name := ""
	var repoPaths []string

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 < len(args) {
				i++
				name = args[i]
			}
		case "--repo":
			if i+1 < len(args) {
				i++
				repoPaths = append(repoPaths, args[i])
			}
		}
	}

	if name == "" {
		// Use current directory name
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		name = filepath.Base(cwd)
	}

	if len(repoPaths) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		repoPaths = []string{cwd}
	}

	if err := project.Init(name, repoPaths); err != nil {
		return err
	}

	// Create madflow.toml in current directory if it doesn't exist
	cwd, _ := os.Getwd()
	configPath := filepath.Join(cwd, "madflow.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		tmpl := fmt.Sprintf(`[project]
name = "%s"

[[project.repos]]
name = "%s"
path = "%s"

[agent]
context_reset_minutes = 8

[agent.models]
superintendent = "claude-opus-4-6"
engineer = "claude-sonnet-4-6"

[branches]
main = "main"
develop = "develop"
feature_prefix = "feature/issue-"
`, name, filepath.Base(repoPaths[0]), repoPaths[0])
		if err := os.WriteFile(configPath, []byte(tmpl), 0644); err != nil {
			return fmt.Errorf("create config: %w", err)
		}
	}

	// Create prompts/ directory with default templates so that `madflow start`
	// works immediately after `madflow init` on a new project.
	promptsDir := filepath.Join(cwd, "prompts")
	if err := prompts.WriteDefaults(promptsDir); err != nil {
		return fmt.Errorf("create default prompts: %w", err)
	}

	fmt.Printf("Project '%s' initialized.\n", name)
	fmt.Printf("Config: %s\n", configPath)
	fmt.Printf("Prompts: %s\n", promptsDir)
	return nil
}

func cmdStart() error {
	configPath, cfg, proj, err := loadProjectConfig()
	if err != nil {
		return err
	}

	// Determine prompts directory
	promptDir := findPromptsDir(cfg.PromptsDir)

	orc := orchestrator.New(cfg, proj.DataDir, promptDir).WithConfigPath(configPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Start chatlog display in foreground mode
	go displayChatLog(ctx, orc.ChatLogPath())

	fmt.Printf("Starting MADFLOW for project '%s'...\n", proj.ID)
	err = orc.Run(ctx)
	// context.Canceled is the expected outcome when the user stops the process
	// (Ctrl+C / SIGTERM). Treat it as a clean exit rather than an error.
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// loadProjectConfig detects the project and loads its config.
// It returns the resolved config file path along with the parsed config and
// project metadata so that the caller can enable hot-reload.
func loadProjectConfig() (string, *config.Config, *project.Project, error) {
	configPath, err := findConfigPath()
	if err != nil {
		return "", nil, nil, err
	}

	proj, err := project.Detect()
	if err != nil {
		return "", nil, nil, err
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return "", nil, nil, err
	}

	return configPath, cfg, proj, nil
}

// findPromptsDir looks for the prompts directory.
// If configDir is set (from madflow.toml), it takes priority.
func findPromptsDir(configDir string) string {
	// 1. Config-specified directory
	if configDir != "" {
		if info, err := os.Stat(configDir); err == nil && info.IsDir() {
			return configDir
		}
	}

	// 2. Check relative to cwd
	cwd, _ := os.Getwd()
	dir := filepath.Join(cwd, "prompts")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}

	// 3. Check relative to the binary
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Join(filepath.Dir(exe), "prompts")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	// Fallback
	return "prompts"
}

// displayChatLog streams the chatlog to stdout with role-based colors.
func displayChatLog(ctx context.Context, logPath string) {
	cl := chatlog.New(logPath)
	msgCh := cl.WatchAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			printColoredMessage(msg)
		}
	}
}

// printColoredMessage prints a chatlog message with role-based coloring.
func printColoredMessage(msg chatlog.Message) {
	color := colorReset
	// Extract base role from sender (e.g., "architect-1" -> "architect")
	sender := msg.Sender
	for role, c := range roleColors {
		if strings.HasPrefix(sender, role) {
			color = c
			break
		}
	}
	fmt.Printf("%s%s%s\n", color, msg.Raw, colorReset)
}
