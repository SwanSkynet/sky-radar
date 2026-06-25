package activities

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// invokeClaude runs the Claude Code CLI headlessly inside the repo checkout.
//
// Guardrails (per repo owner's instructions, not the SDK's defaults):
//   - --strict-mcp-config with no --mcp-config means zero MCP servers load,
//     so the agent has no Slack/email/issue-comment-style tools available.
//   - --disallowedTools WebFetch,WebSearch removes open-web browsing/fetch.
//   - --permission-mode bypassPermissions is required because nothing can
//     answer an interactive approval prompt in this unattended loop.
//
// Bash itself stays available: this project's own tasks need outbound HTTP
// to public ADS-B providers (adsb.lol, OpenSky, airplanes.live), so blocking
// network access at the Bash level would break legitimate work.
func invokeClaude(ctx context.Context, prompt string) error {
	cmd := exec.CommandContext(ctx, "claude",
		"-p",
		"--output-format", "json",
		"--permission-mode", "bypassPermissions",
		"--strict-mcp-config",
		"--disallowedTools", "WebFetch,WebSearch",
	)
	cmd.Dir = RepoRoot
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude run failed: %w", err)
	}
	return nil
}

// RunClaude checks out a fresh branch off latest main, runs Claude on the
// task's prompt, and commits + pushes whatever it produced. Commit/push is
// done here deterministically rather than trusting the model to do it.
func RunClaude(ctx context.Context, task Task) error {
	if err := startBranch(task.Branch); err != nil {
		return fmt.Errorf("starting branch %s: %w", task.Branch, err)
	}
	if err := invokeClaude(ctx, task.Prompt); err != nil {
		return err
	}
	return commitAndPush(task.Branch, "Automated: "+task.ID)
}

// AddressReviewFeedback re-invokes Claude on the task's existing branch with
// the reviewer's feedback appended, then commits + pushes the fix.
func AddressReviewFeedback(ctx context.Context, task Task, feedback string) error {
	if err := runGit(RepoRoot, "checkout", task.Branch); err != nil {
		return fmt.Errorf("checking out branch %s: %w", task.Branch, err)
	}

	prompt := fmt.Sprintf(
		"You previously implemented the following task on this branch:\n\n%s\n\n---\n\n"+
			"CodeRabbit posted this review on the open pull request (#%d). Address every "+
			"actionable comment; for nitpicks, only fix them if they're trivial:\n\n%s",
		task.Prompt, task.PR, feedback,
	)
	if err := invokeClaude(ctx, prompt); err != nil {
		return err
	}
	return commitAndPush(task.Branch, fmt.Sprintf("Automated: address review feedback on PR #%d", task.PR))
}

// startBranch resumes an existing branch (e.g. an activity retry after a
// crash) or, for a fresh task, branches off up-to-date main.
func startBranch(branch string) error {
	if err := runGit(RepoRoot, "checkout", branch); err == nil {
		return nil
	}
	if err := runGit(RepoRoot, "checkout", "main"); err != nil {
		return err
	}
	if err := runGit(RepoRoot, "pull", "--ff-only", "origin", "main"); err != nil {
		return err
	}
	return runGit(RepoRoot, "checkout", "-b", branch)
}

func commitAndPush(branch, message string) error {
	if err := runGit(RepoRoot, "add", "-A"); err != nil {
		return err
	}
	if err := runGit(RepoRoot, "commit", "-m", message); err != nil {
		fmt.Println("git commit produced no changes (or already committed):", err)
	}
	if err := runGit(RepoRoot, "push", "-u", "origin", branch); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
