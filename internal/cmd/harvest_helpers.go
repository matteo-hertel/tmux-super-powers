package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type worktreeInfo struct {
	sessionName  string
	branch       string
	baseBranch   string
	worktreePath string
	filesChanged int
	insertions   int
	deletions    int
	status       string // "ready", "wip", "clean"
	diffOutput   string
	aheadCount   int
	prNumber     int
	prURL        string
	ciStatus     string // "pass", "fail", "pending", ""
	reviewCount  int
}

type prComment struct {
	File   string
	Line   int
	Author string
	Body   string
}

var diffStatSummary = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// parseDiffStat parses the summary line of `git diff --stat`.
func parseDiffStat(output string) (files, insertions, deletions int) {
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

// collectWorktreeInfo gathers diff and PR data for a worktree.
func collectWorktreeInfo(wt Worktree, repoName string) worktreeInfo {
	info := worktreeInfo{
		sessionName:  fmt.Sprintf("%s-%s", repoName, wt.Branch),
		branch:       wt.Branch,
		worktreePath: wt.Path,
	}

	// Get diff stat
	statCmd := exec.Command("git", "-C", wt.Path, "diff", "--stat")
	if out, err := statCmd.Output(); err == nil {
		info.filesChanged, info.insertions, info.deletions = parseDiffStat(string(out))
	}

	// Determine status
	if info.filesChanged > 0 {
		info.status = "wip"
	} else {
		// Check commits ahead of base
		logCmd := exec.Command("git", "-C", wt.Path, "log", "--oneline", "HEAD")
		if out, err := logCmd.Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			info.aheadCount = len(lines)
		}
		if info.aheadCount > 0 {
			info.status = "ready"
		} else {
			info.status = "clean"
		}
	}

	// Get full diff
	diffCmd := exec.Command("git", "-C", wt.Path, "diff")
	if out, err := diffCmd.Output(); err == nil {
		info.diffOutput = string(out)
	}

	// Try to find PR
	info.prNumber, info.prURL = findPRForBranch(wt.Branch)
	if info.prNumber > 0 {
		info.ciStatus = getCIStatus(info.prNumber)
		info.reviewCount = getReviewCommentCount(info.prNumber)
	}

	return info
}

// findPRForBranch uses gh CLI to find a PR for the given branch.
func findPRForBranch(branch string) (int, string) {
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

// getCIStatus checks CI status for a PR number.
func getCIStatus(prNumber int) string {
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

// getReviewCommentCount returns the number of PR review comments.
func getReviewCommentCount(prNumber int) int {
	cmd := exec.Command("gh", "pr", "view", fmt.Sprintf("%d", prNumber), "--json", "comments", "--jq", ".comments | length")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	count, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return count
}

// fetchFailingCILogs fetches failing CI logs for a PR.
func fetchFailingCILogs(prNumber int) (string, error) {
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

// fetchPRComments fetches review comments for a PR.
func fetchPRComments(prNumber int) ([]prComment, error) {
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

	var comments []prComment
	for _, r := range raw {
		comments = append(comments, prComment{
			File:   r.Path,
			Line:   r.Line,
			Author: r.User.Login,
			Body:   r.Body,
		})
	}
	return comments, nil
}

// formatPRComments formats PR comments grouped by file.
func formatPRComments(comments []prComment) string {
	byFile := make(map[string][]prComment)
	for _, c := range comments {
		byFile[c.File] = append(byFile[c.File], c)
	}

	var b strings.Builder
	b.WriteString("## PR Review Comments\n\n")
	for file, cs := range byFile {
		b.WriteString(fmt.Sprintf("### %s\n", file))
		for _, c := range cs {
			b.WriteString(fmt.Sprintf("Line %d — @%s: %q\n", c.Line, c.Author, c.Body))
		}
		b.WriteString("\n")
	}
	return b.String()
}
