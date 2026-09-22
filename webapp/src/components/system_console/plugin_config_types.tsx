// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {LLMBotConfig} from './bot';
import {EmbeddingSearchConfig} from './embedding_search/types';
import {MCPConfig} from './mcp_servers';
import {LLMService} from './service';
import {WebSearchConfig as WebSearchSettings} from './web_search/web_search_panel';

export type RuntimeCostRate = {
    runtimeType?: 'codex' | 'openai' | 'local' | 'inherit',
    providerID?: string,
    model: string,
    inputPerMillion?: number,
    cachedReadPerMillion?: number,
    cachedWritePerMillion?: number,
    outputPerMillion?: number,
};

export type CodexRuntimeConfig = {
    commandPath: string,
    transport: 'exec' | 'app-server' | '',
    extraArgs: string,
    home: string,
};

// services/bots: server sends nil Go slices as JSON null.
export type PluginConfig = {
    services: LLMService[] | null,
    bots: LLMBotConfig[] | null,
    defaultBotName: string,
    transcriptBackend: string,
    telemetryOutput: 'off' | 'logs' | 'otlp' | '',
    openTelemetryEndpoint: string,
    enableTokenUsageLogging: boolean,
    enableCallSummary: boolean,
    allowedUpstreamHostnames: string,
    allowUnsafeLinks: boolean,
    enableChannelMentionToolCalling: boolean,
    allowNativeWebSearchInChannels: boolean,
    enableAgentRuntimeControlPlane: boolean,
    codexRuntime: CodexRuntimeConfig,
    runtimeCostRates: RuntimeCostRate[] | null,
    embeddingSearchConfig: EmbeddingSearchConfig,
    mcp: MCPConfig,
    webSearch: WebSearchSettings,
}
