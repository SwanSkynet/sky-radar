package activities

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// scopeNote is prepended to every prompt so the agent doesn't wander into
// automation/ (this pipeline's own code, not part of the product) just
// because it's visible in the same repo checkout.
const scopeNote = "Note: `automation/` at the repo root is internal CI/CD pipeline tooling, " +
	"not part of the sky-radar product. Do not read, modify, or reference it as part of this task.\n\n---\n\n"

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
func invokeClaude(ctx context.Context, root, prompt string) error {
	cmd := exec.CommandContext(ctx, "claude",
		"-p",
		"--output-format", "json",
		"--permission-mode", "bypassPermissions",
		"--strict-mcp-config",
		"--disallowedTools", "WebFetch,WebSearch",
	)
	cmd.Dir = root
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
	root, err := RepoRoot()
	if err != nil {
		return err
	}
	if err := startBranch(ctx, root, task.Branch); err != nil {
		return fmt.Errorf("starting branch %s: %w", task.Branch, err)
	}
	if err := invokeClaude(ctx, root, scopeNote+task.Prompt); err != nil {
		return err
	}
	return commitAndPush(ctx, root, task.Branch, "Automated: "+task.ID)
}

// AddressReviewFeedback re-invokes Claude on the task's existing branch with
// the reviewer's feedback appended, then commits + pushes the fix.
func AddressReviewFeedback(ctx context.Context, task Task, feedback string) error {
	root, err := RepoRoot()
	if err != nil {
		return err
	}
	if err := runGit(ctx, root, "checkout", task.Branch); err != nil {
		return fmt.Errorf("checking out branch %s: %w", task.Branch, err)
	}

	prompt := scopeNote + fmt.Sprintf(
		"You previously implemented the following task on this branch:\n\n%s\n\n---\n\n"+
			"CodeRabbit posted this review on the open pull request (#%d). Address every "+
			"actionable comment; for nitpicks, only fix them if they're trivial:\n\n%s",
		task.Prompt, task.PR, feedback,
	)
	if err := invokeClaude(ctx, root, prompt); err != nil {
		return err
	}
	return commitAndPush(ctx, root, task.Branch, fmt.Sprintf("Automated: address review feedback on PR #%d", task.PR))
}

// startBranch resumes an existing branch (e.g. an activity retry after a
// crash) or, for a fresh task, branches off up-to-date main.
func startBranch(ctx context.Context, root, branch string) error {
	if err := runGit(ctx, root, "checkout", branch); err == nil {
		return nil
	}
	if err := runGit(ctx, root, "checkout", "main"); err != nil {
		return err
	}
	if err := runGit(ctx, root, "pull", "--ff-only", "origin", "main"); err != nil {
		return err
	}
	return runGit(ctx, root, "checkout", "-b", branch)
}

// commitAndPush stages everything except automation/ itself. automation/ is
// pipeline tooling that lives in this same repo checkout for convenience,
// not product code — without this exclusion, `git add -A` sweeps it into
// every task PR (it did, once: PR #8 accidentally shipped the entire
// automation/ tree, CodeRabbit reviewed it, and a headless run "fixed" the
// orchestrator's own source as a review-feedback side effect).
func commitAndPush(ctx context.Context, root, branch, message string) error {
	if err := runGit(ctx, root, "add", "-A", "--", ".", ":!automation"); err != nil {
		return err
	}
	if err := commitIfChanged(ctx, root, message); err != nil {
		return err
	}
	if err := runGit(ctx, root, "push", "-u", "origin", branch); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// commitIfChanged commits staged changes, tolerating only the no-op case
// where there's nothing to commit (e.g. Claude produced no net diff).
// Real failures (hook rejections, lock contention, bad config) are
// propagated instead of being swallowed.
func commitIfChanged(ctx context.Context, dir, message string) error {
	cmd := exec.CommandContext(ctx, "git", "commit", "-m", message)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	os.Stdout.Write(out)
	if err != nil && !strings.Contains(string(out), "nothing to commit") {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
