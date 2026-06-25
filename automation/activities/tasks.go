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
	"sync"
)

// Task represents one prompt file to be worked on.
type Task struct {
	ID     string // e.g. "phase-1-07-basic-frontend"
	Path   string // full path to the .md file
	Prompt string // file contents
	Branch string // git branch name to use
	PR     int    // PR number, filled in later
}

// RepoRoot resolves the sky-radar repo root via `git rev-parse
// --show-toplevel` so it's correct regardless of where `go run` is launched
// from (the automation module lives in a subdirectory of the repo).
// Resolution happens lazily on first call rather than at package init, so a
// failure (e.g. no .git checkout present) surfaces as a normal activity
// error instead of crashing the whole worker process at startup.
var RepoRoot = sync.OnceValues(func() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("resolving repo root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
})

func promptsDir() (string, error) {
	root, err := RepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "prompts"), nil
}

func completedMarkerDir() (string, error) {
	dir, err := promptsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".completed"), nil
}

// GetNextTask walks the prompts/phase-*/*.md tree in natural order (phase-2
// before phase-10, 9-x before 10-x regardless of zero-padding) and returns
// the first task that hasn't been marked complete yet. Returns nil if the
// queue is empty.
func GetNextTask(ctx context.Context) (*Task, error) {
	pDir, err := promptsDir()
	if err != nil {
		return nil, err
	}
	markerDir, err := completedMarkerDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(markerDir, 0755); err != nil {
		return nil, err
	}

	var relFiles []string
	err = filepath.WalkDir(pDir, func(path string, d fs.DirEntry, err error) error {
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
		rel, err := filepath.Rel(pDir, path)
		if err != nil {
			return err
		}
		relFiles = append(relFiles, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking prompts dir: %w", err)
	}
	sort.Slice(relFiles, func(i, j int) bool { return naturalLess(relFiles[i], relFiles[j]) })

	for _, rel := range relFiles {
		id := taskID(rel)
		markerPath := filepath.Join(markerDir, id+".done")
		if _, err := os.Stat(markerPath); err == nil {
			continue // already completed
		}

		content, err := os.ReadFile(filepath.Join(pDir, rel))
		if err != nil {
			return nil, fmt.Errorf("reading task file %s: %w", rel, err)
		}

		return &Task{
			ID:     id,
			Path:   filepath.Join(pDir, rel),
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

// naturalLess reports whether a sorts before b under natural-order
// comparison: runs of digits compare numerically (so "2" < "10") instead of
// lexically, which keeps phase-2 before phase-10 and 9-x before 10-x
// regardless of zero-padding.
func naturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if isDigit(ca) && isDigit(cb) {
			ni, na := scanNumber(a, i)
			nj, nb := scanNumber(b, j)
			if na != nb {
				return na < nb
			}
			i, j = ni, nj
			continue
		}
		if ca != cb {
			return ca < cb
		}
		i++
		j++
	}
	return len(a)-i < len(b)-j
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func scanNumber(s string, i int) (next int, n int) {
	for i < len(s) && isDigit(s[i]) {
		n = n*10 + int(s[i]-'0')
		i++
	}
	return i, n
}

// MarkComplete writes a marker file so this task is skipped next time.
func MarkComplete(ctx context.Context, task Task) error {
	markerDir, err := completedMarkerDir()
	if err != nil {
		return err
	}
	markerPath := filepath.Join(markerDir, task.ID+".done")
	return os.WriteFile(markerPath, []byte(fmt.Sprintf("PR #%d merged\n", task.PR)), 0644)
}
