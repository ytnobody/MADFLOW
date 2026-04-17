package orchestrator

import "strings"

// CommandType is a sum type representing the set of valid orchestrator commands.
// Using iota instead of raw strings prevents invalid command values from being constructed.
type CommandType int

const (
	// CommandTeamCreate requests creation of a new team for an issue.
	CommandTeamCreate CommandType = iota
	// CommandTeamDisband requests disbanding an existing team.
	CommandTeamDisband
	// CommandRelease requests a new release.
	CommandRelease
	// CommandWakeGitHub wakes the GitHub polling subsystem from dormancy.
	CommandWakeGitHub
	// CommandPatrolComplete signals that an issue patrol cycle has completed.
	CommandPatrolComplete
	// CommandUnknown represents an unrecognized command.
	CommandUnknown
)

// String returns the canonical command keyword for ct.
func (ct CommandType) String() string {
	switch ct {
	case CommandTeamCreate:
		return "TEAM_CREATE"
	case CommandTeamDisband:
		return "TEAM_DISBAND"
	case CommandRelease:
		return "RELEASE"
	case CommandWakeGitHub:
		return "WAKE_GITHUB"
	case CommandPatrolComplete:
		return "PATROL_COMPLETE"
	default:
		return "UNKNOWN"
	}
}

// Command is a parsed orchestrator command with its type and arguments.
type Command struct {
	// Type identifies the kind of command.
	Type CommandType
	// Args contains the whitespace-separated tokens following the command keyword.
	// For CommandUnknown, Args is nil.
	Args []string
}

// ParseCommand parses a chatlog message body into a Command.
// It is a pure function with no side effects.
// If body is empty, whitespace-only, or begins with an unrecognized keyword,
// a Command with Type == CommandUnknown is returned.
func ParseCommand(body string) Command {
	body = strings.TrimSpace(body)
	if body == "" {
		return Command{Type: CommandUnknown}
	}
	fields := strings.Fields(body)
	keyword := fields[0]
	args := fields[1:]

	var cmdType CommandType
	switch keyword {
	case "TEAM_CREATE":
		cmdType = CommandTeamCreate
	case "TEAM_DISBAND":
		cmdType = CommandTeamDisband
	case "RELEASE":
		cmdType = CommandRelease
	case "WAKE_GITHUB":
		cmdType = CommandWakeGitHub
	case "PATROL_COMPLETE":
		cmdType = CommandPatrolComplete
	default:
		return Command{Type: CommandUnknown}
	}

	return Command{Type: cmdType, Args: args}
}
