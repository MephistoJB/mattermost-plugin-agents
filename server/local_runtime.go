// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/bifrost"
	"github.com/mattermost/mattermost-plugin-agents/v2/config"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/mattermost/mattermost-plugin-agents/v2/llmruntime"
)

func runtimeProvidersFromServices(services []llm.ServiceConfig) (map[string]llm.LanguageModel, []error) {
	providers := map[string]llm.LanguageModel{}
	var errs []error
	for _, service := range services {
		if !isLocalRuntimeService(service) {
			continue
		}
		if !llm.IsValidService(service) {
			errs = append(errs, fmt.Errorf("local runtime service %q is not valid", service.ID))
			continue
		}
		model, err := bifrost.NewFromServiceConfig(service, llm.BotConfig{
			Name:      "local-runtime",
			ServiceID: service.ID,
		}, nil)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to create local runtime service %q: %w", service.ID, err))
			continue
		}
		providers[service.ID] = model
	}
	return providers, errs
}

func configuredRuntimeMap(cfg *config.Config) (map[agentruntime.RuntimeType]agentruntime.AgentRuntime, []error) {
	var services []llm.ServiceConfig
	var codexConfig config.CodexRuntimeConfig
	if cfg != nil {
		services = cfg.Services
		codexConfig = cfg.CodexRuntime
	}
	runtimes := map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
		agentruntime.RuntimeTypeCodex: configuredCodexRuntime(codexConfig),
	}
	providers, errs := runtimeProvidersFromServices(services)
	if len(providers) > 0 {
		runtimes[agentruntime.RuntimeTypeLocal] = llmruntime.New(llmruntime.Options{
			Providers:   providers,
			RuntimeType: agentruntime.RuntimeTypeLocal,
		})
	}
	return runtimes, errs
}

func (p *Plugin) refreshConfiguredRuntimes() {
	if p == nil || p.runtimeControl == nil || p.configuration.Config() == nil {
		return
	}
	runtimes, errs := configuredRuntimeMap(p.configuration.Config())
	for _, err := range errs {
		if p.pluginAPI != nil {
			p.pluginAPI.Log.Warn("Failed to configure local runtime service", "error", err)
		}
	}
	p.runtimeControl.SetRuntimes(runtimes)
	p.runtimeControl.SetCostRates(p.configuration.RuntimeCostRates())
}

func isLocalRuntimeService(service llm.ServiceConfig) bool {
	if service.Type != llm.ServiceTypeOpenAICompatible || service.ID == "" || service.APIURL == "" {
		return false
	}
	host := serviceHost(service.APIURL)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "host.docker.internal") {
		return true
	}
	if !strings.Contains(host, ".") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return strings.HasSuffix(strings.ToLower(host), ".local")
	}
	return ip.IsLoopback() || ip.IsPrivate() || isTailscaleIP(ip)
}

func serviceHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	if host == "" && !strings.Contains(rawURL, "://") {
		parsed, err = url.Parse("http://" + rawURL)
		if err != nil {
			return ""
		}
		host = parsed.Hostname()
	}
	return strings.Trim(host, "[]")
}

func isTailscaleIP(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
}
