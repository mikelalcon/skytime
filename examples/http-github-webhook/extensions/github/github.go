// Package github exposes the GitHub REST API as a Skytime extension.
//
// Surface (in .star):
//
//	gh = github.client(credential = "github_token")    # bearer-style auth
//	step(action = gh.get_issue(owner = "octocat", repo = "Hello-World", number = 1))
//
// Unauthenticated form (public-API-only flows like public_repo_check.star):
//
//	gh = github.client()
//	step(action = gh.get_repo(owner = "octocat", repo = "Hello-World"))
//
// Seven operations covering the surface needed by the locked Phase 6 flows:
//
//	get_repo                 GET /repos/{owner}/{repo}                    Idempotent: true
//	get_issue                GET /repos/{owner}/{repo}/issues/{n}         Idempotent: true
//	list_open_issues         GET /repos/{owner}/{repo}/issues?state=open  Idempotent: true
//	add_comment              POST /repos/{owner}/{repo}/issues/{n}/comments    Idempotent: false
//	add_label                POST /repos/{owner}/{repo}/issues/{n}/labels      Idempotent: false
//	list_prs                 GET /repos/{owner}/{repo}/pulls               Idempotent: true
//	list_recent_merged_prs   GET /repos/{owner}/{repo}/pulls?state=closed  Idempotent: true (filters merged-in-7-days client-side)
//
// Idempotence rationale (RESEARCH.md § 1a + RFC-7231 + GitHub REST):
//   - GETs are protocol-idempotent and application-idempotent.
//   - add_comment + add_label are non-idempotent at the application layer:
//     calling either twice creates a duplicate row on the issue.
//
// Implementation: github.com/google/go-github/v78. The library has only one
// external dep (google/go-querystring) plus stdlib net/http; no protobuf
// collisions with the Temporal SDK.
package github

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	gogh "github.com/google/go-github/v78/github"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// skytimeGitHub is the GitHub extension implementation. New() returns
// a value of this type as extension.Extension.
type skytimeGitHub struct{}

// New constructs the github extension for registration with cli.WithExtensions.
func New() extension.Extension { return skytimeGitHub{} }

// Name returns "github" — the parser-side global identifier and the
// registration key for mock-router lookups (Tier-3 tests use this name,
// not the local Starlark variable from `gh = github.client(...)`).
func (skytimeGitHub) Name() string { return "github" }

// Initialize returns the parse-time `github` namespace value with two
// factory builtins:
//   - client(credential=...) — outbound REST client (Phase 6).
//   - webhook(events=..., secret_credential=...) — inbound webhook source
//     factory (Phase 7.1, D-7.1-02). Signature scheme is hardcoded
//     HMAC-SHA256 + X-Hub-Signature-256 per TRIG-09 + D-7.1-04 — see
//     webhook.go for the locked contract.
//
// Called ONCE per parser at extension Register time per the Extension
// contract.
func (skytimeGitHub) Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error) {
	return &starlarkstruct.Module{
		Name: "github",
		Members: starlark.StringDict{
			"client":  starlark.NewBuiltin("github.client", clientFactory),
			"webhook": starlark.NewBuiltin("github.webhook", webhookFactory),
		},
	}, nil
}

// Operations declares per-op specs with idempotence flags. The matrix
// reflects GitHub REST + RFC-7231 + application semantics:
//   - GETs (get_repo / get_issue / list_open_issues / list_prs /
//     list_recent_merged_prs) are idempotent — true.
//   - add_comment + add_label are non-idempotent — false. Each call
//     creates a new comment / appends a new label row.
//
// The Registry rejects nil Idempotent at registration (D-12), so
// extension.Ptr(...) is mandatory at every spec.
func (skytimeGitHub) Operations() map[string]*extension.OperationSpec {
	return map[string]*extension.OperationSpec{
		"get_repo":               {Name: "get_repo", Idempotent: extension.Ptr(true), Func: doGetRepo, KwargsType: reflect.TypeOf(GetRepoArgs{}), DefaultTimeout: 30 * time.Second},
		"get_issue":              {Name: "get_issue", Idempotent: extension.Ptr(true), Func: doGetIssue, KwargsType: reflect.TypeOf(GetIssueArgs{}), DefaultTimeout: 30 * time.Second},
		"list_open_issues":       {Name: "list_open_issues", Idempotent: extension.Ptr(true), Func: doListOpenIssues, KwargsType: reflect.TypeOf(ListIssuesArgs{}), DefaultTimeout: 30 * time.Second},
		"add_comment":            {Name: "add_comment", Idempotent: extension.Ptr(false), Func: doAddComment, KwargsType: reflect.TypeOf(AddCommentArgs{}), DefaultTimeout: 30 * time.Second},
		"add_label":              {Name: "add_label", Idempotent: extension.Ptr(false), Func: doAddLabel, KwargsType: reflect.TypeOf(AddLabelArgs{}), DefaultTimeout: 30 * time.Second},
		"list_prs":               {Name: "list_prs", Idempotent: extension.Ptr(true), Func: doListPRs, KwargsType: reflect.TypeOf(ListPRsArgs{}), DefaultTimeout: 30 * time.Second},
		"list_recent_merged_prs": {Name: "list_recent_merged_prs", Idempotent: extension.Ptr(true), Func: doListRecentMergedPRs, KwargsType: reflect.TypeOf(ListPRsArgs{}), DefaultTimeout: 30 * time.Second},
	}
}

// Compile-time interface check.
var _ extension.Extension = skytimeGitHub{}

// ----- kwargs schemas -----
// The `star:"name,required"` tags drive extension.UnpackOperationKwargs
// reflection. Tags MUST match the kwarg names used in .star files.

// GetRepoArgs is the kwargs schema for .get_repo.
type GetRepoArgs struct {
	Owner string `star:"owner,required"`
	Repo  string `star:"repo,required"`
}

// GetIssueArgs is the kwargs schema for .get_issue.
type GetIssueArgs struct {
	Owner  string `star:"owner,required"`
	Repo   string `star:"repo,required"`
	Number int    `star:"number,required"`
}

// ListIssuesArgs is the kwargs schema for .list_open_issues.
type ListIssuesArgs struct {
	Owner string `star:"owner,required"`
	Repo  string `star:"repo,required"`
}

// AddCommentArgs is the kwargs schema for .add_comment.
type AddCommentArgs struct {
	Owner  string `star:"owner,required"`
	Repo   string `star:"repo,required"`
	Number int    `star:"number,required"`
	Body   string `star:"body,required"`
}

// AddLabelArgs is the kwargs schema for .add_label.
type AddLabelArgs struct {
	Owner  string `star:"owner,required"`
	Repo   string `star:"repo,required"`
	Number int    `star:"number,required"`
	Label  string `star:"label,required"`
}

// ListPRsArgs is the kwargs schema for .list_prs and .list_recent_merged_prs.
type ListPRsArgs struct {
	Owner string `star:"owner,required"`
	Repo  string `star:"repo,required"`
}

// ----- factory + per-op builtin glue -----

// clientFactory implements `github.client(credential="...")`. Returns a
// sub-Module exposing the seven op builtins, each closing over the
// credential ID. An empty credential is the unauthenticated path used by
// public_repo_check.star.
func clientFactory(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var credential string
	if err := starlark.UnpackArgs("github.client", args, kwargs, "credential?", &credential); err != nil {
		return nil, err
	}
	return &starlarkstruct.Module{
		Name: "github.client",
		Members: starlark.StringDict{
			"get_repo":               newOpBuiltin("github.get_repo", credential),
			"get_issue":              newOpBuiltin("github.get_issue", credential),
			"list_open_issues":       newOpBuiltin("github.list_open_issues", credential),
			"add_comment":            newOpBuiltin("github.add_comment", credential),
			"add_label":              newOpBuiltin("github.add_label", credential),
			"list_prs":               newOpBuiltin("github.list_prs", credential),
			"list_recent_merged_prs": newOpBuiltin("github.list_recent_merged_prs", credential),
		},
	}, nil
}

// newOpBuiltin returns a *starlark.Builtin that, when called from a
// .star file, builds an *dag.ActionRef carrying:
//
//	Kind_         = "github.<op>"  (e.g. "github.get_repo") — the activity-side dispatcher keys on this string
//	Kwargs        = the unpacked starlark.Dict of kwargs
//	CredentialID  = credential captured at github.client(credential=...) time
//
// Construction is via struct literal — there is no dag.NewActionRef
// constructor (verified by grep -n "func NewActionRef" pkg/dag/).
// The activity-side OperationFunc reads kwargs via
// extension.UnpackOperationKwargs against KwargsType from Operations().
func newOpBuiltin(kind, credential string) *starlark.Builtin {
	return starlark.NewBuiltin(kind, func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		// Build the kwargs Dict the parser stores on the ActionRef.
		// Mirrors pkg/extension/builtin/http/http.go newMethodBuiltin
		// shape; the GitHub extension does not need an injected kwarg
		// like the HTTP extension's base_url.
		outDict := starlark.NewDict(len(kwargs))
		for _, kv := range kwargs {
			if err := outDict.SetKey(kv[0], kv[1]); err != nil {
				return nil, fmt.Errorf("%s: bad kwarg: %w", kind, err)
			}
		}
		outDict.Freeze()
		return &dag.ActionRef{
			Pos:          callerPosition(thread),
			Kind_:        kind,
			Kwargs:       outDict,
			CredentialID: credential,
		}, nil
	})
}

// callerPosition extracts the .star call-site position. Mirrors
// parser.callerPosition: use thread.CallFrame(1).Pos so errors point at
// the .star site, not the builtin def site. Returns the zero value when
// the call stack is too shallow (defensive — every .star-driven
// invocation will have depth >= 2).
func callerPosition(thread *starlark.Thread) syntax.Position {
	if thread.CallStackDepth() < 2 {
		return syntax.Position{}
	}
	return thread.CallFrame(1).Pos
}

// ----- OperationFunc implementations -----

func doGetRepo(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asGetRepoArgs(args)
	client := newClientForCredential(cred)
	repo, _, err := client.Repositories.Get(ctx, a.Owner, a.Repo)
	if err != nil {
		return nil, classifyGitHubError("get_repo", err)
	}
	return GitHubRepoOutput{
		FullName:      repo.GetFullName(),
		Stars:         repo.GetStargazersCount(),
		Forks:         repo.GetForksCount(),
		OpenIssues:    repo.GetOpenIssuesCount(),
		DefaultBranch: repo.GetDefaultBranch(),
	}, nil
}

func doGetIssue(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asGetIssueArgs(args)
	client := newClientForCredential(cred)
	issue, _, err := client.Issues.Get(ctx, a.Owner, a.Repo, a.Number)
	if err != nil {
		return nil, classifyGitHubError("get_issue", err)
	}
	return issueToOutput(issue), nil
}

func doListOpenIssues(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asListIssuesArgs(args)
	client := newClientForCredential(cred)
	issues, _, err := client.Issues.ListByRepo(ctx, a.Owner, a.Repo, &gogh.IssueListByRepoOptions{
		State:       "open",
		ListOptions: gogh.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, classifyGitHubError("list_open_issues", err)
	}
	out := GitHubIssueListOutput{Issues: make([]GitHubIssueOutput, 0, len(issues))}
	for _, iss := range issues {
		// Skip PRs: GitHub returns PRs from the issues endpoint,
		// distinguished by .PullRequestLinks.
		if iss.PullRequestLinks != nil {
			continue
		}
		out.Issues = append(out.Issues, issueToOutput(iss))
	}
	return out, nil
}

func doAddComment(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asAddCommentArgs(args)
	client := newClientForCredential(cred)
	body := a.Body
	comment, _, err := client.Issues.CreateComment(ctx, a.Owner, a.Repo, a.Number, &gogh.IssueComment{Body: &body})
	if err != nil {
		return nil, classifyGitHubError("add_comment", err)
	}
	return GitHubCommentOutput{ID: comment.GetID(), Body: comment.GetBody()}, nil
}

func doAddLabel(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asAddLabelArgs(args)
	client := newClientForCredential(cred)
	labels, _, err := client.Issues.AddLabelsToIssue(ctx, a.Owner, a.Repo, a.Number, []string{a.Label})
	if err != nil {
		return nil, classifyGitHubError("add_label", err)
	}
	names := make([]string, 0, len(labels))
	for _, l := range labels {
		names = append(names, l.GetName())
	}
	return GitHubLabelsOutput{Labels: names}, nil
}

func doListPRs(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asListPRsArgs(args)
	client := newClientForCredential(cred)
	prs, _, err := client.PullRequests.List(ctx, a.Owner, a.Repo, &gogh.PullRequestListOptions{
		State:       "open",
		ListOptions: gogh.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, classifyGitHubError("list_prs", err)
	}
	return prsToOutput(prs), nil
}

func doListRecentMergedPRs(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	a := asListPRsArgs(args)
	client := newClientForCredential(cred)
	prs, _, err := client.PullRequests.List(ctx, a.Owner, a.Repo, &gogh.PullRequestListOptions{
		State:       "closed",
		Sort:        "updated",
		Direction:   "desc",
		ListOptions: gogh.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, classifyGitHubError("list_recent_merged_prs", err)
	}
	// time.Now() inside an activity is permitted (activities run in
	// normal Go and may use time/random freely; only WORKFLOW code is
	// constrained by Temporal's deterministic runner).
	cutoff := time.Now().AddDate(0, 0, -7)
	recent := make([]*gogh.PullRequest, 0, len(prs))
	for _, pr := range prs {
		if pr.MergedAt != nil && pr.GetMergedAt().After(cutoff) {
			recent = append(recent, pr)
		}
	}
	return prsToOutput(recent), nil
}

// ----- helpers -----

// newClientForCredential constructs a *gogh.Client with auth applied if
// the credential is a BearerCredential. Other credential kinds (or nil)
// fall back to the unauthenticated client — public endpoints work, and
// protected endpoints will return 401 which classifyGitHubError wraps as
// ErrNonRetryable.
func newClientForCredential(cred extension.Credential) *gogh.Client {
	if cred == nil {
		return gogh.NewClient(nil) // unauthenticated; public API only
	}
	bearer, ok := cred.(*extension.BearerCredential)
	if !ok {
		return gogh.NewClient(nil) // wrong cred kind; protected endpoints will 401 → ErrNonRetryable
	}
	return gogh.NewClient(nil).WithAuthToken(bearer.Token.Reveal())
}

// classifyGitHubError wraps go-github errors per RESEARCH.md § 1a:
//   - *github.RateLimitError → pass through (Temporal retries; rate
//     limits are transient by definition).
//   - *github.ErrorResponse with 4xx → wrap extension.ErrNonRetryable
//     (configuration / permission / not-found bugs; retrying won't help).
//   - 5xx / other → pass through (Temporal retries; transient).
func classifyGitHubError(op string, err error) error {
	var rate *gogh.RateLimitError
	if errors.As(err, &rate) {
		return fmt.Errorf("github %s: rate-limited (reset=%v): %w", op, rate.Rate.Reset.Time, err)
	}
	var resp *gogh.ErrorResponse
	if errors.As(err, &resp) && resp.Response != nil {
		sc := resp.Response.StatusCode
		if sc >= 400 && sc < 500 {
			return fmt.Errorf("github %s: HTTP %d %s: %w", op, sc, resp.Message, extension.ErrNonRetryable)
		}
	}
	return fmt.Errorf("github %s: %w", op, err)
}

// issueToOutput projects a *gogh.Issue into the wire-stable
// GitHubIssueOutput. time.Time fields are stringified to RFC3339 in UTC
// for deterministic Temporal JSON encoding (no timezone drift).
func issueToOutput(iss *gogh.Issue) GitHubIssueOutput {
	out := GitHubIssueOutput{
		Number: iss.GetNumber(),
		Title:  iss.GetTitle(),
		State:  iss.GetState(),
		Body:   iss.GetBody(),
		Labels: make([]string, 0, len(iss.Labels)),
	}
	if !iss.GetCreatedAt().IsZero() {
		out.CreatedAt = iss.GetCreatedAt().UTC().Format(time.RFC3339)
	}
	if !iss.GetUpdatedAt().IsZero() {
		out.UpdatedAt = iss.GetUpdatedAt().UTC().Format(time.RFC3339)
	}
	for _, l := range iss.Labels {
		out.Labels = append(out.Labels, l.GetName())
	}
	return out
}

// prsToOutput projects a []*gogh.PullRequest into the wire-stable
// GitHubPRListOutput. MergedAt is RFC3339-UTC string (empty when not merged).
func prsToOutput(prs []*gogh.PullRequest) GitHubPRListOutput {
	out := GitHubPRListOutput{PullRequests: make([]GitHubPROutput, 0, len(prs))}
	for _, pr := range prs {
		p := GitHubPROutput{
			Number: pr.GetNumber(),
			Title:  pr.GetTitle(),
			State:  pr.GetState(),
			Author: pr.GetUser().GetLogin(),
		}
		if pr.MergedAt != nil && !pr.GetMergedAt().IsZero() {
			p.MergedAt = pr.GetMergedAt().UTC().Format(time.RFC3339)
		}
		out.PullRequests = append(out.PullRequests, p)
	}
	return out
}

// ----- args coercion helpers -----
//
// Mirrors pkg/extension/builtin/http/http.go's asGetArgs / asBodyArgs
// pattern. The activity-side decoder may pass either a value or a
// pointer; both must work to keep the OperationFunc tolerant to caller
// convention. (See pkg/extension/builtin/http/http.go quick 260502-guu
// Rule 1 fix for the original motivation.)

func asGetRepoArgs(args any) *GetRepoArgs {
	if p, ok := args.(*GetRepoArgs); ok {
		return p
	}
	v := args.(GetRepoArgs)
	return &v
}

func asGetIssueArgs(args any) *GetIssueArgs {
	if p, ok := args.(*GetIssueArgs); ok {
		return p
	}
	v := args.(GetIssueArgs)
	return &v
}

func asListIssuesArgs(args any) *ListIssuesArgs {
	if p, ok := args.(*ListIssuesArgs); ok {
		return p
	}
	v := args.(ListIssuesArgs)
	return &v
}

func asAddCommentArgs(args any) *AddCommentArgs {
	if p, ok := args.(*AddCommentArgs); ok {
		return p
	}
	v := args.(AddCommentArgs)
	return &v
}

func asAddLabelArgs(args any) *AddLabelArgs {
	if p, ok := args.(*AddLabelArgs); ok {
		return p
	}
	v := args.(AddLabelArgs)
	return &v
}

func asListPRsArgs(args any) *ListPRsArgs {
	if p, ok := args.(*ListPRsArgs); ok {
		return p
	}
	v := args.(ListPRsArgs)
	return &v
}
