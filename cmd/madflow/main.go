package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ytnobody/madflow/internal/chatlog"
	"github.com/ytnobody/madflow/internal/config"
	"github.com/ytnobody/madflow/internal/github"
	"github.com/ytnobody/madflow/internal/issue"
	"github.com/ytnobody/madflow/internal/orchestrator"
	"github.com/ytnobody/madflow/internal/project"
)

const usage = `Usage: madflow <command> [options]

Commands:
  init                      Initialize a new project
  start                     Start all agents
  status                    Show running agents and teams
  logs                      Show chatlog in real-time
  stop                      Stop all agents
  issue create <title>      Create a new issue
  issue list                List issues
  issue show <id>           Show issue details
  issue close <id>          Close an issue
  release                   Merge develop into main
  sync                      Sync GitHub issues manually
  use <claude|gemini>       Switch all models to a specific backend
`

// ANSI color codes for role-based coloring.
var roleColors = map[string]string{
	"superintendent":  "\033[31m", // red
	"engineer":        "\033[33m", // yellow
	"reviewer":        "\033[35m", // magenta
	"release_manager": "\033[36m", // cyan
	"orchestrator":    "\033[37m", // white
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
	case "status":
		err = cmdStatus()
	case "logs":
		err = cmdLogs()
	case "stop":
		err = cmdStop()
	case "issue":
		err = cmdIssue()
	case "release":
		err = cmdRelease()
	case "sync":
		err = cmdSync()
	case "use":
		err = cmdUse()
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
reviewer = "claude-sonnet-4-6"
release_manager = "claude-haiku-4-5"

[branches]
main = "main"
develop = "develop"
feature_prefix = "feature/issue-"
`, name, filepath.Base(repoPaths[0]), repoPaths[0])
		if err := os.WriteFile(configPath, []byte(tmpl), 0644); err != nil {
			return fmt.Errorf("create config: %w", err)
		}
	}

	fmt.Printf("Project '%s' initialized.\n", name)
	fmt.Printf("Config: %s\n", configPath)
	return nil
}

func cmdStart() error {
	cfg, proj, err := loadProjectConfig()
	if err != nil {
		return err
	}

	// Determine prompts directory
	promptDir := findPromptsDir(cfg.PromptsDir)

	orc := orchestrator.New(cfg, proj.DataDir, promptDir)

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
	return orc.Run(ctx)
}

func cmdStatus() error {
	_, proj, err := loadProjectConfig()
	if err != nil {
		return err
	}

	fmt.Printf("Project: %s\n", proj.ID)
	fmt.Printf("Data dir: %s\n", proj.DataDir)

	// Show issues summary
	store := issue.NewStore(filepath.Join(proj.DataDir, "issues"))
	issues, err := store.List(issue.StatusFilter{})
	if err != nil {
		return err
	}

	statusCounts := map[issue.Status]int{}
	for _, iss := range issues {
		statusCounts[iss.Status]++
	}

	fmt.Println("\nIssues:")
	fmt.Printf("  open:        %d\n", statusCounts[issue.StatusOpen])
	fmt.Printf("  in_progress: %d\n", statusCounts[issue.StatusInProgress])
	fmt.Printf("  resolved:    %d\n", statusCounts[issue.StatusResolved])
	fmt.Printf("  closed:      %d\n", statusCounts[issue.StatusClosed])

	return nil
}

func cmdLogs() error {
	_, proj, err := loadProjectConfig()
	if err != nil {
		return err
	}

	logPath := filepath.Join(proj.DataDir, "chatlog.txt")

	// Show last N lines first
	n := 20
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		if args[i] == "-n" && i+1 < len(args) {
			i++
			fmt.Sscanf(args[i], "%d", &n)
		}
	}

	showLastLines(logPath, n)

	// Then stream new lines
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	displayChatLog(ctx, logPath)
	return nil
}

func cmdStop() error {
	// For now, signal-based stop through the pidfile or process management
	fmt.Println("To stop MADFLOW, press Ctrl+C in the terminal where 'madflow start' is running.")
	return nil
}

func cmdIssue() error {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: madflow issue <create|list|show|close> [args]")
		return nil
	}

	_, proj, err := loadProjectConfig()
	if err != nil {
		return err
	}

	store := issue.NewStore(filepath.Join(proj.DataDir, "issues"))

	switch os.Args[2] {
	case "create":
		return issueCreate(store)
	case "list":
		return issueList(store)
	case "show":
		return issueShow(store)
	case "close":
		return issueClose(store)
	default:
		return fmt.Errorf("unknown issue subcommand: %s", os.Args[2])
	}
}

func issueCreate(store *issue.Store) error {
	if len(os.Args) < 4 {
		return fmt.Errorf("usage: madflow issue create <title> [--body <body>]")
	}
	title := os.Args[3]
	body := ""

	args := os.Args[4:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--body" && i+1 < len(args) {
			i++
			body = args[i]
		}
	}

	iss, err := store.Create(title, body)
	if err != nil {
		return err
	}

	fmt.Printf("Created issue: %s\n", iss.ID)
	fmt.Printf("  Title: %s\n", iss.Title)
	return nil
}

func issueList(store *issue.Store) error {
	var filter issue.StatusFilter

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--status" && i+1 < len(args) {
			i++
			s := issue.Status(args[i])
			filter.Status = &s
		}
	}

	issues, err := store.List(filter)
	if err != nil {
		return err
	}

	if len(issues) == 0 {
		fmt.Println("No issues found.")
		return nil
	}

	fmt.Printf("%-20s %-12s %-6s %s\n", "ID", "STATUS", "TEAM", "TITLE")
	fmt.Println(strings.Repeat("-", 70))
	for _, iss := range issues {
		teamStr := "-"
		if iss.AssignedTeam > 0 {
			teamStr = fmt.Sprintf("%d", iss.AssignedTeam)
		}
		fmt.Printf("%-20s %-12s %-6s %s\n", iss.ID, iss.Status, teamStr, iss.Title)
	}

	return nil
}

func issueShow(store *issue.Store) error {
	if len(os.Args) < 4 {
		return fmt.Errorf("usage: madflow issue show <id>")
	}
	id := os.Args[3]

	iss, err := store.Get(id)
	if err != nil {
		return err
	}

	fmt.Printf("ID:     %s\n", iss.ID)
	fmt.Printf("Title:  %s\n", iss.Title)
	fmt.Printf("Status: %s\n", iss.Status)
	if iss.URL != "" {
		fmt.Printf("URL:    %s\n", iss.URL)
	}
	if iss.AssignedTeam > 0 {
		fmt.Printf("Team:   %d\n", iss.AssignedTeam)
	}
	if len(iss.Repos) > 0 {
		fmt.Printf("Repos:  %s\n", strings.Join(iss.Repos, ", "))
	}
	if len(iss.Labels) > 0 {
		fmt.Printf("Labels: %s\n", strings.Join(iss.Labels, ", "))
	}
	if iss.Body != "" {
		fmt.Printf("\n%s\n", iss.Body)
	}
	if iss.Acceptance != "" {
		fmt.Printf("\n## Acceptance Criteria\n%s\n", iss.Acceptance)
	}

	return nil
}

func issueClose(store *issue.Store) error {
	if len(os.Args) < 4 {
		return fmt.Errorf("usage: madflow issue close <id>")
	}
	id := os.Args[3]

	iss, err := store.Get(id)
	if err != nil {
		return err
	}

	iss.Status = issue.StatusClosed
	if err := store.Update(iss); err != nil {
		return err
	}

	fmt.Printf("Issue %s closed.\n", id)
	return nil
}

func cmdRelease() error {
	cfg, proj, err := loadProjectConfig()
	if err != nil {
		return err
	}

	// Write a release command to the chatlog for the orchestrator to process
	logPath := filepath.Join(proj.DataDir, "chatlog.txt")
	msg := chatlog.FormatMessage("orchestrator", "human", "RELEASE")

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open chatlog: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, msg); err != nil {
		return fmt.Errorf("write chatlog: %w", err)
	}

	fmt.Printf("Release command sent. Merging %s -> %s...\n", cfg.Branches.Develop, cfg.Branches.Main)
	return nil
}

func cmdSync() error {
	cfg, proj, err := loadProjectConfig()
	if err != nil {
		return err
	}

	if cfg.GitHub == nil {
		return fmt.Errorf("no [github] section in madflow.toml")
	}

	store := issue.NewStore(filepath.Join(proj.DataDir, "issues"))
	syncer := github.NewSyncer(store, cfg.GitHub.Owner, cfg.GitHub.Repos, 0)

	fmt.Println("Syncing GitHub issues...")
	if err := syncer.SyncOnce(); err != nil {
		return err
	}
	fmt.Println("Sync complete.")
	return nil
}

// loadProjectConfig detects the project and loads its config.
func loadProjectConfig() (*config.Config, *project.Project, error) {
	configPath, err := findConfigPath()
	if err != nil {
		return nil, nil, err
	}

	proj, err := project.Detect()
	if err != nil {
		return nil, nil, err
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, err
	}

	return cfg, proj, nil
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

// showLastLines displays the last n lines of a file.
func showLastLines(path string, n int) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}

	for _, line := range lines[start:] {
		msg, err := chatlog.ParseMessage(line)
		if err != nil {
			fmt.Println(line)
			continue
		}
		printColoredMessage(msg)
	}
}

// Ensure time is used (for future daemon mode).
var _ = time.Second
