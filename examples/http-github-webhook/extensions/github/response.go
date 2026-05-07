// Package github exposes the GitHub REST API as a Skytime extension.
// Output types live here so they're easy to find from .star authors
// who want to know what fields are available on ctx.<step_output>.
package github

// GitHubRepoOutput is returned by get_repo. Fields are the subset of
// the github.Repository struct that flows commonly need; expand here
// if a real flow surfaces a missing field.
type GitHubRepoOutput struct {
	FullName      string `json:"full_name"`
	Stars         int    `json:"stars"`
	Forks         int    `json:"forks"`
	OpenIssues    int    `json:"open_issues"`
	DefaultBranch string `json:"default_branch"`
}

// IsOperationOutput marks GitHubRepoOutput as a dag.OperationOutput.
func (GitHubRepoOutput) IsOperationOutput() {}

// GitHubIssueOutput is returned by get_issue and is the element type
// of GitHubIssueListOutput.Issues.
type GitHubIssueOutput struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Body      string   `json:"body"`
	CreatedAt string   `json:"created_at"` // RFC3339; empty if missing
	UpdatedAt string   `json:"updated_at"` // RFC3339; empty if missing
	Labels    []string `json:"labels"`
}

// IsOperationOutput marks GitHubIssueOutput as a dag.OperationOutput.
func (GitHubIssueOutput) IsOperationOutput() {}

// GitHubIssueListOutput is returned by list_open_issues.
type GitHubIssueListOutput struct {
	Issues []GitHubIssueOutput `json:"issues"`
}

// IsOperationOutput marks GitHubIssueListOutput as a dag.OperationOutput.
func (GitHubIssueListOutput) IsOperationOutput() {}

// GitHubCommentOutput is returned by add_comment.
type GitHubCommentOutput struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// IsOperationOutput marks GitHubCommentOutput as a dag.OperationOutput.
func (GitHubCommentOutput) IsOperationOutput() {}

// GitHubLabelsOutput is returned by add_label. Labels is the FULL
// post-add label set on the issue (go-github's AddLabelsToIssue
// returns this shape natively).
type GitHubLabelsOutput struct {
	Labels []string `json:"labels"`
}

// IsOperationOutput marks GitHubLabelsOutput as a dag.OperationOutput.
func (GitHubLabelsOutput) IsOperationOutput() {}

// GitHubPROutput is the element type of GitHubPRListOutput.PullRequests.
type GitHubPROutput struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	State    string `json:"state"`
	MergedAt string `json:"merged_at"` // RFC3339; "" if not merged
	Author   string `json:"author"`    // pr.User.Login
}

// GitHubPRListOutput is returned by list_prs and list_recent_merged_prs.
type GitHubPRListOutput struct {
	PullRequests []GitHubPROutput `json:"pull_requests"`
}

// IsOperationOutput marks GitHubPRListOutput as a dag.OperationOutput.
func (GitHubPRListOutput) IsOperationOutput() {}
