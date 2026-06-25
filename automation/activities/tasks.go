package activities

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Task represents one prompt file to be worked on.
type Task struct {
	ID     string // e.g. "phase-1-07-basic-frontend"
	Path   string // full path to the .md file
	Prompt string // file contents
	Branch string // git branch name to use
	PR     int    // PR number, filled in later
}

// RepoRoot is the sky-radar repo root, resolved once via `git rev-parse
// --show-toplevel` so it's correct regardless of where `go run` is launched
// from (the automation module lives in a subdirectory of the repo).
var RepoRoot = mustRepoRoot()

func mustRepoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		panic(fmt.Errorf("resolving repo root: %w", err))
	}
	return strings.TrimSpace(string(out))
}

func promptsDir() string         { return filepath.Join(RepoRoot, "prompts") }
func completedMarkerDir() string { return filepath.Join(promptsDir(), ".completed") }

// GetNextTask walks the prompts/phase-*/*.md tree in lexical order (phase-1
// before phase-2, and numerically-prefixed files within each phase) and
// returns the first task that hasn't been marked complete yet. Returns nil
// if the queue is empty.
func GetNextTask(ctx context.Context) (*Task, error) {
	if err := os.MkdirAll(completedMarkerDir(), 0755); err != nil {
		return nil, err
	}

	var relFiles []string
	err := filepath.WalkDir(promptsDir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".completed" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(promptsDir(), path)
		if err != nil {
			return err
		}
		relFiles = append(relFiles, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking prompts dir: %w", err)
	}
	sort.Strings(relFiles)

	for _, rel := range relFiles {
		id := taskID(rel)
		markerPath := filepath.Join(completedMarkerDir(), id+".done")
		if _, err := os.Stat(markerPath); err == nil {
			continue // already completed
		}

		content, err := os.ReadFile(filepath.Join(promptsDir(), rel))
		if err != nil {
			return nil, fmt.Errorf("reading task file %s: %w", rel, err)
		}

		return &Task{
			ID:     id,
			Path:   filepath.Join(promptsDir(), rel),
			Prompt: string(content),
			Branch: "auto/" + id,
		}, nil
	}

	return nil, nil // queue empty
}

// taskID turns "phase-1/07-basic-frontend.md" into "phase-1-07-basic-frontend".
func taskID(relPath string) string {
	noExt := strings.TrimSuffix(relPath, filepath.Ext(relPath))
	return strings.ReplaceAll(noExt, string(filepath.Separator), "-")
}

// MarkComplete writes a marker file so this task is skipped next time.
func MarkComplete(ctx context.Context, task Task) error {
	markerPath := filepath.Join(completedMarkerDir(), task.ID+".done")
	return os.WriteFile(markerPath, []byte(fmt.Sprintf("PR #%d merged\n", task.PR)), 0644)
}
