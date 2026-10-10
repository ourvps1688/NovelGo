package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// GetCommitLog
// ---------------------------------------------------------------------------

func TestGetCommitLog(t *testing.T) {
	app := setupTestApp(t)

	// Use CreateNovel to get a real git repo with an initial commit.
	n, err := app.CreateNovel(CreateNovelInput{
		Title:       "History Test Novel",
		Description: "For testing git history",
		Genre:       "fantasy",
	})
	require.NoError(t, err, "create novel with git repo")

	logs, err := app.GetCommitLog(n.ID, 5, "")
	require.NoError(t, err)
	require.NotEmpty(t, logs, "expected at least one commit (initial commit)")

	commit := logs[0]
	require.NotEmpty(t, commit.Hash)
	require.NotEmpty(t, commit.ShortHash)
	require.NotEmpty(t, commit.Message)
	assert.False(t, commit.Time.IsZero())
}

// ---------------------------------------------------------------------------
// GetCommitFileList
// ---------------------------------------------------------------------------

func TestGetCommitFileList(t *testing.T) {
	app := setupTestApp(t)

	n, err := app.CreateNovel(CreateNovelInput{
		Title: "File List Test Novel",
		Genre: "fantasy",
	})
	require.NoError(t, err, "create novel with git repo")

	logs, err := app.GetCommitLog(n.ID, 5, "")
	require.NoError(t, err)
	require.NotEmpty(t, logs)

	firstHash := logs[0].Hash

	result, err := app.GetCommitFileList(n.ID, firstHash)
	require.NoError(t, err)
	require.NotNil(t, result.Commit)
	assert.Equal(t, firstHash, result.Commit.Hash)
	assert.NotEmpty(t, result.Files)
}

// ---------------------------------------------------------------------------
// GetFileDiff
// ---------------------------------------------------------------------------

func TestGetFileDiff(t *testing.T) {
	app := setupTestApp(t)

	n, err := app.CreateNovel(CreateNovelInput{
		Title: "Diff Test Novel",
		Genre: "fantasy",
	})
	require.NoError(t, err, "create novel with git repo")

	// The initial commit from CreateNovel should include novelgo.md.
	// Find a commit that contains a known file and get its diff.
	logs, err := app.GetCommitLog(n.ID, 5, "")
	require.NoError(t, err)
	require.NotEmpty(t, logs)

	var targetHash string
	var targetFile string
	for _, c := range logs {
		fileList, listErr := app.GetCommitFileList(n.ID, c.Hash)
		if listErr != nil {
			continue
		}
		for _, f := range fileList.Files {
			if f.Path == "novelgo.md" || f.Path == "chapters/.gitkeep" {
				targetHash = c.Hash
				targetFile = f.Path
				break
			}
		}
		if targetHash != "" {
			break
		}
	}

	if targetHash == "" {
		t.Skip("no commit with expected file found (git may not be available)")
		return
	}

	diff, err := app.GetFileDiff(n.ID, targetHash, targetFile)
	require.NoError(t, err)
	require.NotNil(t, diff)
	assert.Equal(t, targetFile, diff.Path)
}
