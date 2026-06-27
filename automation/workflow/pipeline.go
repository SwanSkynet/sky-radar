package workflow

import (
	"errors"
	"time"

	"automation/activities"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// ReviewSignalChannel is signaled by the webhook receiver when CodeRabbit
// (or any reviewer) submits a review on the current PR.
const ReviewSignalChannel = "review-submitted"

// PipelineWorkflow processes every task file in /prompts, one at a time,
// driving each through: branch -> Claude -> PR -> review -> merge.
func PipelineWorkflow(ctx workflow.Context) error {
	logger := workflow.GetLogger(ctx)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute, // Claude runs can take a while
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Activities that invoke Claude can hit the CLI's session/usage limit
	// (a 429 that names a fixed reset time, e.g. "resets 6:20am"). The default
	// 3-quick-attempts policy above just burns through retries instantly and
	// fails the whole pipeline, requiring a manual restart every time the
	// limit is hit - the opposite of unattended operation. This policy backs
	// off up to an hour between attempts for up to 14 hours, comfortably
	// covering a daily reset window without giving up.
	claudeCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    60 * time.Minute,
		ScheduleToCloseTimeout: 14 * time.Hour,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Minute,
			BackoffCoefficient: 2.0,
			MaximumInterval:    1 * time.Hour,
			MaximumAttempts:    0, // unbounded; ScheduleToCloseTimeout is the real bound
		},
	})

	for {
		var task *activities.Task
		if err := workflow.ExecuteActivity(ctx, activities.GetNextTask).Get(ctx, &task); err != nil {
			return err
		}
		if task == nil {
			logger.Info("No more tasks. Pipeline complete.")
			return nil
		}

		logger.Info("Starting task", "id", task.ID)

		if err := workflow.ExecuteActivity(claudeCtx, activities.RunClaude, *task).Get(ctx, nil); err != nil {
			return err
		}

		var prNumber int
		for {
			err := workflow.ExecuteActivity(ctx, activities.CreatePR, *task).Get(ctx, &prNumber)
			if err == nil {
				break
			}
			var appErr *temporal.ApplicationError
			if !errors.As(err, &appErr) || appErr.Type() != activities.ErrNoCommitsType {
				return err
			}
			// Claude's prior session ended without committing anything (e.g. it
			// was still waiting on a long-running command). Resume the task on
			// the same branch instead of failing the whole pipeline.
			logger.Info("No commits yet, continuing task", "id", task.ID)
			if err := workflow.ExecuteActivity(claudeCtx, activities.ContinueWork, *task).Get(ctx, nil); err != nil {
				return err
			}
		}
		task.PR = prNumber
		logger.Info("PR created", "pr", prNumber)

		reviewChan := workflow.GetSignalChannel(ctx, ReviewSignalChannel)

		approved := false
		for !approved {
			// Webhook delivery isn't a complete picture of "CodeRabbit said
			// something": when a re-review finds nothing actionable, CodeRabbit
			// sometimes posts a plain issue comment ("No actionable comments
			// were generated...") instead of submitting a formal review, which
			// fires no pull_request_review event at all - our only signal
			// source. Waiting on the signal alone can then block forever on a
			// verdict that already exists. So this also wakes up on a timer:
			// webhooks give a fast path in the common case, the timer is the
			// fallback that guarantees forward progress regardless of which
			// GitHub event shape actually fired (or didn't).
			//
			// The webhook also sends this signal with a nil payload for every
			// pull_request_review event on the repo, not just this PR, so
			// it's intentionally unscoped. FetchReviewStatus below always
			// re-derives the real verdict from prNumber, so a stray signal
			// from another PR just costs one harmless extra poll.
			pollCtx, cancelPoll := workflow.WithCancel(ctx)
			timer := workflow.NewTimer(pollCtx, 2*time.Minute)
			sel := workflow.NewSelector(ctx)
			sel.AddReceive(reviewChan, func(c workflow.ReceiveChannel, more bool) { c.Receive(ctx, nil) })
			sel.AddFuture(timer, func(f workflow.Future) {})
			sel.Select(ctx)
			cancelPoll()

			var result activities.ReviewResult
			if err := workflow.ExecuteActivity(ctx, activities.FetchReviewStatus, prNumber, task.Branch).Get(ctx, &result); err != nil {
				return err
			}

			switch result.Status {
			case activities.ReviewApproved:
				approved = true
			case activities.ReviewChangesRequested:
				logger.Info("Addressing review comments", "pr", prNumber)
				if err := workflow.ExecuteActivity(claudeCtx, activities.AddressFeedback, *task, "CodeRabbit's review", result.Feedback).Get(ctx, nil); err != nil {
					return err
				}
			default:
				logger.Info("Review still pending, waiting for next signal", "pr", prNumber)
			}
		}

		// CodeRabbit approving isn't enough on its own — PR #8 was merged once
		// with a failing frontend CI check because nothing checked CI status
		// before calling MergePR. Poll until every check is green, fixing and
		// re-checking in between if anything fails.
		for {
			var checks activities.ChecksResult
			if err := workflow.ExecuteActivity(ctx, activities.WaitForChecks, prNumber).Get(ctx, &checks); err != nil {
				return err
			}
			if checks.AllPassed {
				break
			}
			logger.Info("CI checks failed, addressing", "pr", prNumber)
			if err := workflow.ExecuteActivity(claudeCtx, activities.AddressFeedback, *task, "A failing CI check", checks.Failures).Get(ctx, nil); err != nil {
				return err
			}
		}

		if err := workflow.ExecuteActivity(ctx, activities.MergePR, prNumber).Get(ctx, nil); err != nil {
			return err
		}
		logger.Info("PR merged", "pr", prNumber)

		if err := workflow.ExecuteActivity(ctx, activities.MarkComplete, *task).Get(ctx, nil); err != nil {
			return err
		}

		logger.Info("Task complete", "id", task.ID)
	}
}
