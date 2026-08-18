package webserver

import "github.com/xyzjace/terraplane/pkg/command"

type agentJobClaimPayload struct {
	AgentID string `json:"agent_id"`
}

type agentJobClaimResponse struct {
	Command command.Command `json:"command"`
}

type agentHeartbeatPayload struct {
	AgentID string `json:"agent_id"`
}
