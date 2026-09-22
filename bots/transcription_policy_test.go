// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package bots

import (
	"net/http"
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/enterprise"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
)

type transcribeConfig struct {
	transcriptGenerator string
}

func (c transcribeConfig) GetBots() []llm.BotConfig {
	return nil
}

func (c transcribeConfig) GetServiceByID(string) (llm.ServiceConfig, bool) {
	return llm.ServiceConfig{}, false
}

func (c transcribeConfig) GetDefaultBotName() string {
	return ""
}

func (c transcribeConfig) EnableTokenUsageLogging() bool {
	return false
}

func (c transcribeConfig) EnableTokenUsageLogToPlugin() bool {
	return false
}

func (c transcribeConfig) EnableTokenUsageLogToFile() bool {
	return false
}

func (c transcribeConfig) GetTranscriptGenerator() string {
	return c.transcriptGenerator
}

func TestIsLocalTranscriptionService(t *testing.T) {
	tests := []struct {
		name    string
		service llm.ServiceConfig
		want    bool
	}{
		{
			name: "localhost openai compatible",
			service: llm.ServiceConfig{
				Type:   llm.ServiceTypeOpenAICompatible,
				APIURL: "http://localhost:11434/v1",
			},
			want: true,
		},
		{
			name: "private ip openai compatible",
			service: llm.ServiceConfig{
				Type:   llm.ServiceTypeOpenAICompatible,
				APIURL: "http://192.168.1.10:8080/v1",
			},
			want: true,
		},
		{
			name: "local dns suffix openai compatible",
			service: llm.ServiceConfig{
				Type:   llm.ServiceTypeOpenAICompatible,
				APIURL: "http://whisper.local:8080/v1",
			},
			want: true,
		},
		{
			name: "cloud openai is not local",
			service: llm.ServiceConfig{
				Type:   llm.ServiceTypeOpenAI,
				APIURL: "https://api.openai.com/v1",
			},
			want: false,
		},
		{
			name: "public openai compatible is not local",
			service: llm.ServiceConfig{
				Type:   llm.ServiceTypeOpenAICompatible,
				APIURL: "https://api.example.com/v1",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isLocalTranscriptionService(tt.service))
		})
	}
}

func TestTranscriptionReadiness(t *testing.T) {
	service := llm.ServiceConfig{
		Type:   llm.ServiceTypeOpenAICompatible,
		APIURL: "http://whisper.local:8080/v1",
	}
	bot := NewBot(
		llm.BotConfig{Name: "transcriber"},
		service,
		&model.Bot{UserId: "bot-user-id", Username: "transcriber"},
		nil,
	)
	mmBots := New(nil, nil, enterprise.NewLicenseChecker(nil), transcribeConfig{transcriptGenerator: "transcriber"}, nil, &http.Client{}, nil)
	mmBots.SetBotsForTesting([]*Bot{bot})

	assert.True(t, mmBots.HasTranscribe())
	assert.True(t, mmBots.HasLocalTranscribe())
}

func TestTranscriptionReadinessRejectsCloudForLocal(t *testing.T) {
	service := llm.ServiceConfig{
		Type:   llm.ServiceTypeOpenAI,
		APIURL: "https://api.openai.com/v1",
	}
	bot := NewBot(
		llm.BotConfig{Name: "transcriber"},
		service,
		&model.Bot{UserId: "bot-user-id", Username: "transcriber"},
		nil,
	)
	mmBots := New(nil, nil, enterprise.NewLicenseChecker(nil), transcribeConfig{transcriptGenerator: "transcriber"}, nil, &http.Client{}, nil)
	mmBots.SetBotsForTesting([]*Bot{bot})

	assert.True(t, mmBots.HasTranscribe())
	assert.False(t, mmBots.HasLocalTranscribe())
}
