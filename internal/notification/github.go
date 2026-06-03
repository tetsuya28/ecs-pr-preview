package notification

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v71/github"
)

// prPreviewMarker is an HTML comment embedded in bot comments to identify them on subsequent runs.
const prPreviewMarker = "<!-- pr-preview:%s -->"

// GitHubCommenter upserts comments on a pull request using the GitHub API.
type GitHubCommenter struct {
	client   *github.Client
	owner    string
	repo     string
	prNumber int
}

// NewGitHubCommenter creates a GitHubCommenter.
// repository must be in "owner/repo" format.
func NewGitHubCommenter(token, repository string, prNumber int) (*GitHubCommenter, error) {
	parts := strings.SplitN(repository, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repository format (expected owner/repo): %s", repository)
	}
	return &GitHubCommenter{
		client:   github.NewClient(nil).WithAuthToken(token),
		owner:    parts[0],
		repo:     parts[1],
		prNumber: prNumber,
	}, nil
}

// UpsertComment creates or updates the PR comment identified by marker.
func (g *GitHubCommenter) UpsertComment(ctx context.Context, marker, body string) error {
	markedBody := fmt.Sprintf(prPreviewMarker+"\n%s", marker, body)

	existing, err := g.findComment(ctx, marker)
	if err != nil {
		return err
	}
	if existing != nil {
		_, _, err = g.client.Issues.EditComment(ctx, g.owner, g.repo, existing.GetID(), &github.IssueComment{
			Body: github.Ptr(markedBody),
		})
		return err
	}
	_, _, err = g.client.Issues.CreateComment(ctx, g.owner, g.repo, g.prNumber, &github.IssueComment{
		Body: github.Ptr(markedBody),
	})
	return err
}

// findComment returns the existing bot comment for marker, or nil if not found.
func (g *GitHubCommenter) findComment(ctx context.Context, marker string) (*github.IssueComment, error) {
	needle := fmt.Sprintf(prPreviewMarker, marker)
	opts := &github.IssueListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	for {
		comments, resp, err := g.client.Issues.ListComments(ctx, g.owner, g.repo, g.prNumber, opts)
		if err != nil {
			return nil, err
		}
		for _, c := range comments {
			if strings.Contains(c.GetBody(), needle) {
				return c, nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return nil, nil
}
