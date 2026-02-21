package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
}

// promptFileNames maps roles to their prompt template filenames.
var promptFileNames = map[Role]string{
	RoleSuperintendent: "superintendent.md",
	RolePM:             "pm.md",
	RoleArchitect:      "architect.md",
	RoleEngineer:       "engineer.md",
	RoleReviewer:       "reviewer.md",
	RoleReleaseManager: "release_manager.md",
}

// LoadPrompt reads a role's prompt template and substitutes variables.
func LoadPrompt(promptsDir string, role Role, vars PromptVars) (string, error) {
	filename, ok := promptFileNames[role]
	if !ok {
		return "", fmt.Errorf("no prompt template for role: %s", role)
	}

	path := filepath.Join(promptsDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read prompt %s: %w", path, err)
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
	}

	for placeholder, value := range replacements {
		if value != "" {
			content = strings.ReplaceAll(content, placeholder, value)
		}
	}
	return content
}
