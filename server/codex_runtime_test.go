// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/codexruntime"
	"github.com/stretchr/testify/assert"
)

func TestCodexRuntimeOptionsFromEnv(t *testing.T) {
	t.Setenv(codexRuntimeCommandEnv, "/usr/local/bin/codex")
	t.Setenv(codexRuntimeExtraArgsEnv, "--skip-git-repo-check --search")

	opts := codexRuntimeOptionsFromEnv()

	assert.Equal(t, "/usr/local/bin/codex", opts.CommandPath)
	assert.Equal(t, []string{"--skip-git-repo-check", "--search"}, opts.ExtraArgs)
}

func TestConfiguredCodexRuntimeFromEnvUsesAppServerTransport(t *testing.T) {
	t.Setenv(codexRuntimeTransportEnv, "app-server")

	runtime := configuredCodexRuntimeFromEnv()

	assert.IsType(t, &codexruntime.AppServerRuntime{}, runtime)
}
