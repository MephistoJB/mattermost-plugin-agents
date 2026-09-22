// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package agentruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ResolveWorkspacePath(requested string, policy *WorkspacePolicy) (string, error) {
	if policy == nil {
		return requested, nil
	}

	workspacePath := requested
	if workspacePath == "" {
		workspacePath = policy.DefaultWorkspacePath
	}

	roots, err := workspaceAllowedRoots(policy.AllowedRoots)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrWorkspaceNotAllowed, err.Error())
	}
	if len(roots) == 0 {
		return workspacePath, nil
	}
	if workspacePath == "" {
		return "", fmt.Errorf("%w: workspace path is required", ErrWorkspaceNotAllowed)
	}

	cleanWorkspace, err := cleanPhysicalAbsPath(workspacePath)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrWorkspaceNotAllowed, err.Error())
	}
	for _, root := range roots {
		cleanRoot, err := cleanPhysicalAbsPath(root)
		if err != nil {
			return "", fmt.Errorf("%w: invalid allowed root %q", ErrWorkspaceNotAllowed, root)
		}
		if pathWithinRoot(cleanWorkspace, cleanRoot) {
			return cleanWorkspace, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrWorkspaceNotAllowed, cleanWorkspace)
}

func workspaceAllowedRoots(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var roots []string
	if err := json.Unmarshal(raw, &roots); err != nil {
		return nil, fmt.Errorf("failed to decode workspace allowed roots: %w", err)
	}
	return roots, nil
}

func cleanAbsPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is empty")
	}
	cleaned, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(cleaned), nil
}

func cleanPhysicalAbsPath(path string) (string, error) {
	cleaned, err := cleanAbsPath(path)
	if err != nil {
		return "", err
	}
	return resolvePhysicalPath(cleaned)
}

func resolvePhysicalPath(cleaned string) (string, error) {
	real, err := filepath.EvalSymlinks(cleaned)
	if err == nil {
		return filepath.Clean(real), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	current := cleaned
	var missing []string
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent

		realParent, parentErr := filepath.EvalSymlinks(current)
		if parentErr == nil {
			resolved := filepath.Clean(realParent)
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(parentErr) {
			return "", parentErr
		}
	}
}

func pathWithinRoot(path, root string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}
