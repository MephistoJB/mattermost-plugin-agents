// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package agentruntime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveWorkspacePath(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "project")
	outside := t.TempDir()

	tests := []struct {
		name      string
		requested string
		policy    *WorkspacePolicy
		want      string
		wantErr   error
	}{
		{
			name:      "no policy keeps requested path",
			requested: inside,
			want:      inside,
		},
		{
			name: "uses policy default",
			policy: &WorkspacePolicy{
				AllowedRoots:         []byte(`[]`),
				DefaultWorkspacePath: inside,
			},
			want: inside,
		},
		{
			name:      "allows path inside configured root",
			requested: inside,
			policy: &WorkspacePolicy{
				AllowedRoots: []byte(`["` + filepath.ToSlash(root) + `"]`),
			},
			want: inside,
		},
		{
			name:      "blocks path outside configured root",
			requested: outside,
			policy: &WorkspacePolicy{
				AllowedRoots: []byte(`["` + filepath.ToSlash(root) + `"]`),
			},
			wantErr: ErrWorkspaceNotAllowed,
		},
		{
			name: "requires workspace when roots are configured",
			policy: &WorkspacePolicy{
				AllowedRoots: []byte(`["` + filepath.ToSlash(root) + `"]`),
			},
			wantErr: ErrWorkspaceNotAllowed,
		},
		{
			name: "rejects invalid roots json",
			policy: &WorkspacePolicy{
				AllowedRoots: []byte(`{}`),
			},
			wantErr: ErrWorkspaceNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveWorkspacePath(tt.requested, tt.policy)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr), err.Error())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, filepath.Clean(tt.want), got)
		})
	}
}

func TestResolveWorkspacePathBlocksSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600))

	link := filepath.Join(root, "linked-outside")
	require.NoError(t, os.Symlink(outside, link))

	_, err := ResolveWorkspacePath(filepath.Join(link, "secret.txt"), &WorkspacePolicy{
		AllowedRoots: []byte(`["` + filepath.ToSlash(root) + `"]`),
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkspaceNotAllowed), err.Error())
}

func TestResolveWorkspacePathAllowsMissingChildWithinRoot(t *testing.T) {
	root := t.TempDir()
	requested := filepath.Join(root, "new", "..", "project")

	got, err := ResolveWorkspacePath(requested, &WorkspacePolicy{
		AllowedRoots: []byte(`["` + filepath.ToSlash(root) + `"]`),
	})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "project"), got)
}
