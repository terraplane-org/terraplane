package agentapi

const AgentIDHeader = "X-Agent-ID"

// Job is the payload returned by POST /agent/jobs/poll.
type Job struct {
	JobID     string `json:"job_id"`
	Action    string `json:"action"`
	Repo      string `json:"repo"`
	PRNumber  int32  `json:"pr_number"`
	CommitSHA string `json:"commit_sha"`
	StackName string `json:"stack_name"`
	Dir       string `json:"dir"`
	PlanFlags string `json:"plan_flags,omitempty"`
}

// Result is the body of POST /agent/jobs/{id}/result.
type Result struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error"`
}
