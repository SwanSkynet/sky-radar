package workflow

import (
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

		if err := workflow.ExecuteActivity(ctx, activities.RunClaude, *task).Get(ctx, nil); err != nil {
			return err
		}

		var prNumber int
		if err := workflow.ExecuteActivity(ctx, activities.CreatePR, *task).Get(ctx, &prNumber); err != nil {
			return err
		}
		task.PR = prNumber
		logger.Info("PR created", "pr", prNumber)

		reviewChan := workflow.GetSignalChannel(ctx, ReviewSignalChannel)

		approved := false
		for !approved {
			// The webhook sends this signal with a nil payload for every
			// pull_request_review event on the repo, not just this PR, so
			// it's intentionally unscoped. FetchReviewStatus below always
			// re-derives the real verdict from prNumber, so a stray signal
			// from another PR just costs one harmless extra poll.
			reviewChan.Receive(ctx, nil) // blocks until webhook signals us

			var result activities.ReviewResult
			if err := workflow.ExecuteActivity(ctx, activities.FetchReviewStatus, prNumber).Get(ctx, &result); err != nil {
				return err
			}

			switch result.Status {
			case activities.ReviewApproved:
				approved = true
			case activities.ReviewChangesRequested:
				logger.Info("Addressing review comments", "pr", prNumber)
				if err := workflow.ExecuteActivity(ctx, activities.AddressFeedback, *task, "CodeRabbit's review", result.Feedback).Get(ctx, nil); err != nil {
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
			if err := workflow.ExecuteActivity(ctx, activities.AddressFeedback, *task, "A failing CI check", checks.Failures).Get(ctx, nil); err != nil {
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
