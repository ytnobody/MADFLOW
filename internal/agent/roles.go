package agent

import (
	"fmt"
	"time"
)

type Role string

const (
	RoleSuperintendent  Role = "superintendent"

	RoleEngineer        Role = "engineer"
	RoleOrchestrator    Role = "orchestrator"
)

type AgentID struct {
	Role     Role
	TeamNum  int // 0 for non-team agents (superintendent, rm)
}

func (id AgentID) String() string {
	if id.TeamNum == 0 {
		return string(id.Role)
	}
	return fmt.Sprintf("%s-%d", id.Role, id.TeamNum)
}

type ChatMessage struct {
	Timestamp time.Time
	Recipient string
	Sender    string
	Body      string
	Raw       string
}

type WorkMemo struct {
	AgentID      AgentID
	Timestamp    time.Time
	CurrentState string
	Decisions    string
	OpenIssues   string
	NextStep     string
}

// AllowedTargets defines communication permissions per role (chain principle).
var AllowedTargets = map[Role][]Role{
	RoleSuperintendent: {RoleEngineer, RoleOrchestrator},

	RoleEngineer:       {RoleSuperintendent}, // Engineer can send to Superintendent
}

func CanSendTo(from, to Role) bool {
	targets, ok := AllowedTargets[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}