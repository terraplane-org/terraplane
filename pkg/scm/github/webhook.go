package github

type issueCommentWebhook struct {
	Action string `json:"action"`
	Issue  struct {
		Number      int       `json:"number"`
		PullRequest *struct{} `json:"pull_request"`
	} `json:"issue"`
	Comment struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}
