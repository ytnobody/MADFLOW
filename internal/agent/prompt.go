package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ytnobody/madflow/prompts"
)

// PromptVars holds the variables to substitute into prompt templates.
type PromptVars struct {
	AgentID       string
	ChatLogPath   string
	IssuesDir     string
	DevelopBranch string
	MainBranch    string
	FeaturePrefix string
	TeamNum       string
	RepoPath      string
}

// promptFileNames maps roles to their prompt template filenames.
var promptFileNames = map[Role]string{
	RoleSuperintendent: "superintendent.md",
	RoleEngineer:       "engineer.md",
}

// LoadPrompt reads a role's prompt template and substitutes variables.
// It first looks for the file in promptsDir.  If the file does not exist
// there (e.g. on a fresh project created with madflow init), it falls back
// to the embedded default template bundled into the binary.
func LoadPrompt(promptsDir string, role Role, vars PromptVars) (string, error) {
	filename, ok := promptFileNames[role]
	if !ok {
		return "", fmt.Errorf("no prompt template for role: %s", role)
	}

	path := filepath.Join(promptsDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read prompt %s: %w", path, err)
		}
		// Fall back to the embedded default prompt bundled in the binary.
		data, err = prompts.ReadDefault(filename)
		if err != nil {
			return "", fmt.Errorf("read prompt %s: %w", path, os.ErrNotExist)
		}
	}

	content := string(data)
	content = substituteVars(content, vars)
	return content, nil
}

func substituteVars(content string, vars PromptVars) string {
	replacements := map[string]string{
		"{{AGENT_ID}}":       vars.AgentID,
		"{{CHATLOG_PATH}}":   vars.ChatLogPath,
		"{{ISSUES_DIR}}":     vars.IssuesDir,
		"{{DEVELOP_BRANCH}}": vars.DevelopBranch,
		"{{MAIN_BRANCH}}":    vars.MainBranch,
		"{{FEATURE_PREFIX}}": vars.FeaturePrefix,
		"{{TEAM_NUM}}":       vars.TeamNum,
		"{{REPO_PATH}}":      vars.RepoPath,
	}

	for placeholder, value := range replacements {
		if value != "" {
			content = strings.ReplaceAll(content, placeholder, value)
		}
	}
	return content
}
