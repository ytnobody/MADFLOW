package agent

import (
	"fmt"
	"time"
)

type Role string

const (
	RoleSuperintendent  Role = "superintendent"
	RolePM              Role = "pm"
	RoleArchitect       Role = "architect"
	RoleEngineer        Role = "engineer"
	RoleReviewer        Role = "reviewer"
	RoleReleaseManager  Role = "release_manager"
	RoleOrchestrator    Role = "orchestrator"
)

type AgentID struct {
	Role     Role
	TeamNum  int // 0 for non-team agents (superintendent, pm, rm)
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
	RoleSuperintendent: {RolePM},
	RolePM:             {RoleSuperintendent, RoleArchitect, RoleOrchestrator},
	RoleArchitect:      {RolePM, RoleEngineer},
	RoleEngineer:       {RoleArchitect, RoleReviewer},
	RoleReviewer:       {RoleEngineer, RoleReleaseManager},
	RoleReleaseManager: {RoleReviewer},
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
