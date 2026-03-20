package prompts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
)

func TestCollectAFFilesSkipsSymlinks(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "root.af"), []byte(".title Root\nHello"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(root, "nested"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "nested", "child.af"), []byte(".title Child\nHello"), 0644))
	require.NoError(t, os.Symlink(filepath.Join(root, "nested", "child.af"), filepath.Join(root, "linked.af")))

	files, err := collectAFFiles(root)
	require.NoError(t, err)

	if len(files) != 2 {
		t.Fatalf("expected 2 real .af files, got %d: %#v", len(files), files)
	}
}
