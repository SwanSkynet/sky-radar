# Claude/Temporal automation pipeline

Drives the task files in `prompts/phase-*/` through a fixed loop, one at a
time, with no human in the loop unless something goes genuinely wrong:

```
pick next unfinished task -> branch -> run Claude -> open PR
  -> wait for CodeRabbit approval (fixing review comments in between)
  -> wait for CI to go green (fixing failures in between)
  -> squash-merge -> mark task complete -> repeat
```

It's a [Temporal](https://temporal.io) workflow. Temporal exists here so the
loop survives crashes, restarts, and Claude's own session/usage limits
without losing track of where it was — all real state lives either in
Temporal's own execution history or in plain files on disk, never in the
worker process's memory.

## Components

| Binary | What it does |
|---|---|
| `worker/` | Long-running process that executes the workflow + activities. Stateless — just polls Temporal for work. |
| `starter/` | One-shot: starts a new `PipelineWorkflow` execution. |
| `webhook/` | HTTP server (`:8090/webhook`) that signals the running workflow when GitHub sends a `pull_request_review` event. |
| `workflow/pipeline.go` | The actual loop logic above. |
| `activities/` | The individual steps (`RunClaude`, `CreatePR`, `FetchReviewStatus`, `WaitForChecks`, `AddressFeedback`, `MergePR`, `MarkComplete`, `GetNextTask`). |

## Starting it

1. Temporal dev server (Postgres + Elasticsearch + Temporal + Web UI):
   ```bash
   cd automation
   docker compose up -d
   ```
   Web UI at `http://localhost:8080`. **Note:** this compose file uses
   anonymous volumes — `docker compose down` (or recreating containers) wipes
   all workflow history. Use `docker compose stop`/`start` to preserve it.

2. Worker:
   ```bash
   cd automation
   go run ./worker
   ```
   Needs the Temporal server up. Logs every activity it executes.

3. Kick off a run (only if there's no existing execution — see below):
   ```bash
   cd automation
   go run ./starter
   ```

4. Webhook receiver, for the review-feedback loop to actually wake up:
   ```bash
   cd automation
   export $(cat .env.local | xargs)   # loads GITHUB_WEBHOOK_SECRET
   go run ./webhook
   ```
   Needs a GitHub webhook configured (Settings → Webhooks) pointed at this
   server's public URL (via `ngrok http 8090` or similar), subscribed to
   **"Pull request reviews"** only, with a secret matching
   `automation/.env.local` (gitignored — generate one with
   `openssl rand -hex 32` if it doesn't exist).

## Stopping it

`Ctrl+C` (or `kill <pid>`) any of the three processes — they all handle
`SIGINT`/`SIGTERM` cleanly.

- **Safe anytime** for `webhook` and `starter` (starter is one-shot anyway).
- **Safe for `worker`** as long as it's not actively mid-activity (check the
  log — if the last line is an `ExecuteActivity` with no matching
  completion yet, an activity is in flight). Stopping it between activities
  loses nothing: Temporal still has the workflow's full state server-side.
- **Don't stop the worker mid-`RunClaude`/`AddressFeedback`** if you can
  avoid it — the in-flight headless Claude run is abandoned, and Temporal
  won't redeliver that activity to a new worker until its timeout elapses
  (up to an hour, see retries below) rather than instantly.

## How it knows what to do next

Two independent layers, neither of which lives in the worker process:

1. **The Temporal workflow execution** (`claude-pipeline-run`) holds the
   live state for whatever task is currently in flight — which PR number,
   whether it's mid-review-loop, etc. Restarting *just the worker* resumes
   this exact execution with zero setup; Temporal redelivers whatever
   activity was pending.
2. **`prompts/.completed/<task-id>.done` marker files** are how a *new*
   workflow execution (after the old one is terminated, e.g. because it
   failed for real) figures out where to resume — `GetNextTask` walks
   `prompts/phase-*/*.md` in order and skips anything with a marker. This is
   why it's safe to terminate a dead/failed execution and run `starter`
   again: nothing is re-derived from memory, only from what's actually been
   merged.

Run `go run ./starter` again only after the previous execution has reached a
terminal state (Completed/Failed/Terminated) — check via the Web UI or
`tctl workflow describe -w claude-pipeline-run` (inside the
`temporal-admin-tools` container). Starting a second one while the first is
still `Running` will error.

## How retries work

Two different retry policies, because "Claude failed" covers two very
different situations:

- **Everything except Claude invocations** (`CreatePR`, `FetchReviewStatus`,
  `WaitForChecks`, `MergePR`, `MarkComplete`, `GetNextTask`): 3 quick
  attempts, default backoff. These call `gh`/`git` directly — a failure here
  is almost always a real bug or a transient API hiccup, not something
  worth waiting hours for.
- **`RunClaude` / `AddressFeedback`** (anything that runs `claude -p`): these
  can hit the CLI's own session/usage limit (a `429` whose message names a
  fixed daily reset time). Burning through 3 attempts in seconds against a
  limit that resets hours later just kills the whole pipeline and forces a
  manual restart — exactly what this is supposed to avoid. So these use a
  separate policy: back off from 5 minutes up to a 1-hour cap, for up to 14
  hours total, before giving up. A daily limit reset gets absorbed with no
  intervention.

`WaitForChecks` has its own internal poll loop (every 10s) inside a single
activity attempt — that's CI status polling, unrelated to the retry policy
above.

## Known gotchas

- **Don't hand-edit files under `automation/` while the worker is running.**
  `RunClaude`/`AddressFeedback` operate directly in this same repo checkout
  (no isolated worktree) — editing source live while an activity is
  mid-`git checkout`/`commit`/`push` on a different branch can collide with
  it. If you need to change `automation/` code, stop the worker first.
- **`automation/` is deliberately excluded from every task commit**
  (`commitAndPush` uses a pathspec exclusion). It's pipeline tooling, not
  product code — it's tracked on `main` directly, via direct pushes, not
  through the task-PR flow. (PR #8 originally shipped the entire
  `automation/` tree by accident via an unscoped `git add -A`; this is the
  fix.)
- **CodeRabbit approval alone does not trigger a merge.** `WaitForChecks`
  also has to see every CI check pass first (PR #8 was merged once with a
  failing frontend build before this existed).
- Webhook deliveries are deduped by commit SHA (`FetchReviewStatus` ignores
  any CodeRabbit review whose `commit_id` isn't the PR's current head) —
  two webhook events landing back-to-back for the same review won't trigger
  two redundant fix attempts.
