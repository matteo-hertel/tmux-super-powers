package service

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// PRComment represents a single review comment on a pull request.
type PRComment struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Author string `json:"author"`
	Body   string `json:"body"`
}

var diffStatSummary = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// ParseDiffStat parses the summary line of `git diff --stat` output.
// It extracts the number of files changed, insertions, and deletions.
func ParseDiffStat(output string) (files, insertions, deletions int) {
	matches := diffStatSummary.FindStringSubmatch(output)
	if len(matches) == 0 {
		return 0, 0, 0
	}
	files, _ = strconv.Atoi(matches[1])
	if matches[2] != "" {
		insertions, _ = strconv.Atoi(matches[2])
	}
	if matches[3] != "" {
		deletions, _ = strconv.Atoi(matches[3])
	}
	return
}

// GetDiffStat runs `git diff --stat` and `git diff` in the given git directory.
// Returns parsed stat values and the full diff output.
func GetDiffStat(gitPath string) (files, insertions, deletions int, diffOutput string) {
	statCmd := exec.Command("git", "-C", gitPath, "diff", "--stat")
	if out, err := statCmd.Output(); err == nil {
		files, insertions, deletions = ParseDiffStat(string(out))
	}
	diffCmd := exec.Command("git", "-C", gitPath, "diff")
	if out, err := diffCmd.Output(); err == nil {
		diffOutput = string(out)
	}
	return
}

// FindPRForBranch uses the gh CLI to find a pull request for the given branch.
// Returns the PR number and URL, or (0, "") if none found.
func FindPRForBranch(branch string) (int, string) {
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--json", "number,url", "--limit", "1")
	out, err := cmd.Output()
	if err != nil {
		return 0, ""
	}
	var prs []struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(out, &prs); err != nil || len(prs) == 0 {
		return 0, ""
	}
	return prs[0].Number, prs[0].URL
}

// GetCIStatus checks the CI status for a given PR number.
// Returns "fail", "pending", "pass", or "" if unknown.
func GetCIStatus(prNumber int) string {
	cmd := exec.Command("gh", "pr", "checks", fmt.Sprintf("%d", prNumber), "--json", "conclusion", "--jq", ".[].conclusion")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	conclusions := strings.TrimSpace(string(out))
	if strings.Contains(conclusions, "failure") {
		return "fail"
	}
	if strings.Contains(conclusions, "pending") || strings.Contains(conclusions, "queued") {
		return "pending"
	}
	if conclusions != "" {
		return "pass"
	}
	return ""
}

// GetReviewCommentCount returns the number of review comments on a PR.
func GetReviewCommentCount(prNumber int) int {
	cmd := exec.Command("gh", "pr", "view", fmt.Sprintf("%d", prNumber), "--json", "comments", "--jq", ".comments | length")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	count, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return count
}

// FetchFailingCILogs fetches logs from failing CI checks for a PR.
// Returns the concatenated logs or an error if no failing checks are found.
func FetchFailingCILogs(prNumber int) (string, error) {
	cmd := exec.Command("gh", "pr", "checks", fmt.Sprintf("%d", prNumber), "--json", "name,conclusion,detailsUrl")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get checks: %w", err)
	}

	var checks []struct {
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
		DetailsURL string `json:"detailsUrl"`
	}
	if err := json.Unmarshal(out, &checks); err != nil {
		return "", err
	}

	var logs strings.Builder
	for _, check := range checks {
		if check.Conclusion == "failure" {
			logs.WriteString(fmt.Sprintf("### %s (FAILED)\n", check.Name))
			logCmd := exec.Command("gh", "run", "view", "--log-failed")
			logOut, err := logCmd.Output()
			if err == nil {
				logs.WriteString(string(logOut))
			} else {
				logs.WriteString(fmt.Sprintf("(could not fetch logs: %v)\n", err))
			}
			logs.WriteString("\n")
		}
	}

	if logs.Len() == 0 {
		return "", fmt.Errorf("no failing checks found")
	}
	return logs.String(), nil
}

// FetchPRComments fetches review comments for a PR using the GitHub API.
func FetchPRComments(prNumber int) ([]PRComment, error) {
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/comments", prNumber))
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var raw []struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}

	var comments []PRComment
	for _, r := range raw {
		comments = append(comments, PRComment{
			File:   r.Path,
			Line:   r.Line,
			Author: r.User.Login,
			Body:   r.Body,
		})
	}
	return comments, nil
}

// FormatPRComments formats PR comments grouped by file as markdown.
func FormatPRComments(comments []PRComment) string {
	byFile := make(map[string][]PRComment)
	var fileOrder []string
	for _, c := range comments {
		if _, seen := byFile[c.File]; !seen {
			fileOrder = append(fileOrder, c.File)
		}
		byFile[c.File] = append(byFile[c.File], c)
	}

	var b strings.Builder
	b.WriteString("## PR Review Comments\n\n")
	for _, file := range fileOrder {
		cs := byFile[file]
		b.WriteString(fmt.Sprintf("### %s\n", file))
		for _, c := range cs {
			b.WriteString(fmt.Sprintf("Line %d — @%s: %q\n", c.Line, c.Author, c.Body))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// CreatePR pushes the branch and creates a pull request using the gh CLI.
// Returns the PR URL or an error.
func CreatePR(gitPath, branch string) (string, error) {
	pushCmd := exec.Command("git", "-C", gitPath, "push", "-u", "origin", branch)
	if err := pushCmd.Run(); err != nil {
		return "", fmt.Errorf("push failed: %w", err)
	}
	prCmd := exec.Command("gh", "pr", "create",
		"--head", branch,
		"--title", branch,
		"--body", fmt.Sprintf("Auto-created from `tsp dash`\n\nBranch: %s", branch),
	)
	prCmd.Dir = gitPath
	out, err := prCmd.Output()
	if err != nil {
		return "", fmt.Errorf("pr creation failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// MergePR merges a pull request by number using the gh CLI.
func MergePR(prNumber int, gitPath string) error {
	cmd := exec.Command("gh", "pr", "merge", fmt.Sprintf("%d", prNumber), "--merge")
	cmd.Dir = gitPath
	return cmd.Run()
}

// EnrichSessionWithPRData populates PR-related fields if prNumber is 0.
// It looks up the PR for the given branch and fetches CI status and review count.
func EnrichSessionWithPRData(prNumber *int, prURL *string, ciStatus *string, reviewCount *int, branch string) {
	if *prNumber > 0 {
		return
	}
	*prNumber, *prURL = FindPRForBranch(branch)
	if *prNumber > 0 {
		*ciStatus = GetCIStatus(*prNumber)
		*reviewCount = GetReviewCommentCount(*prNumber)
	}
}

// EnrichWithPRData populates PR info on a Session.
func EnrichWithPRData(s *Session) {
	if s.PR != nil && s.PR.Number > 0 {
		return
	}
	prNum, prURL := FindPRForBranch(s.Branch)
	if prNum > 0 {
		s.PR = &PRInfo{
			Number:      prNum,
			URL:         prURL,
			CIStatus:    GetCIStatus(prNum),
			ReviewCount: GetReviewCommentCount(prNum),
		}
	}
}
