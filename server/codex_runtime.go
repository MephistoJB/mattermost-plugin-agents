// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"os"
	"strings"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/codexruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/config"
)

const (
	codexRuntimeCommandEnv   = "MM_AGENTS_CODEX_RUNTIME_COMMAND"
	codexRuntimeExtraArgsEnv = "MM_AGENTS_CODEX_RUNTIME_EXTRA_ARGS"
	codexRuntimeTransportEnv = "MM_AGENTS_CODEX_RUNTIME_TRANSPORT"
)

func codexRuntimeOptionsFromEnv() codexruntime.Options {
	return codexruntime.Options{
		CommandPath: os.Getenv(codexRuntimeCommandEnv),
		ExtraArgs:   strings.Fields(os.Getenv(codexRuntimeExtraArgsEnv)),
	}
}

func codexRuntimeOptionsFromConfig(cfg config.CodexRuntimeConfig) codexruntime.Options {
	opts := codexRuntimeOptionsFromEnv()
	if cfg.CommandPath != "" {
		opts.CommandPath = cfg.CommandPath
	}
	if cfg.ExtraArgs != "" {
		opts.ExtraArgs = strings.Fields(cfg.ExtraArgs)
	}
	if cfg.Home != "" {
		opts.Env = append(opts.Env, "CODEX_HOME="+cfg.Home)
	}
	return opts
}

func configuredCodexRuntimeFromEnv() agentruntime.AgentRuntime {
	opts := codexRuntimeOptionsFromEnv()
	if strings.EqualFold(os.Getenv(codexRuntimeTransportEnv), "app-server") {
		return codexruntime.NewAppServer(opts)
	}
	return codexruntime.New(opts)
}

func configuredCodexRuntime(cfg config.CodexRuntimeConfig) agentruntime.AgentRuntime {
	opts := codexRuntimeOptionsFromConfig(cfg)
	transport := cfg.Transport
	if transport == "" {
		transport = os.Getenv(codexRuntimeTransportEnv)
	}
	if strings.EqualFold(transport, "app-server") {
		return codexruntime.NewAppServer(opts)
	}
	return codexruntime.New(opts)
}
