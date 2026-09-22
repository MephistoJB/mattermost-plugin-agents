// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/mattermost/mattermost-plugin-agents/v2/config"
	"github.com/mattermost/mattermost-plugin-agents/v2/mmapi"
	"github.com/mattermost/mattermost-plugin-agents/v2/ttsbroker"
)

const (
	ttsAPIURLEnv = "MM_AGENTS_TTS_API_URL"
	ttsAPIKeyEnv = "MM_AGENTS_TTS_API_KEY"
	ttsModelEnv  = "MM_AGENTS_TTS_MODEL"
	ttsVoiceEnv  = "MM_AGENTS_TTS_VOICE"
	ttsFormatEnv = "MM_AGENTS_TTS_FORMAT"
	ttsLocalEnv  = "MM_AGENTS_TTS_LOCAL"
)

func textToSpeechFromEnv(client mmapi.Client) (*ttsbroker.Service, error) {
	return textToSpeechFromConfigOrEnv(client, nil)
}

func textToSpeechFromConfigOrEnv(client mmapi.Client, cfg *config.Config) (*ttsbroker.Service, error) {
	if cfg != nil && cfg.TextToSpeech.Enabled {
		return textToSpeechFromOptions(client, ttsbroker.OpenAISpeechOptions{
			APIURL: cfg.TextToSpeech.APIURL,
			APIKey: cfg.TextToSpeech.APIKey,
			Model:  cfg.TextToSpeech.Model,
			Voice:  cfg.TextToSpeech.Voice,
			Format: cfg.TextToSpeech.Format,
			Local:  cfg.TextToSpeech.Local,
		})
	}

	apiURL := strings.TrimSpace(os.Getenv(ttsAPIURLEnv))
	if apiURL == "" {
		return nil, nil
	}

	local, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(ttsLocalEnv)))
	if err != nil && strings.TrimSpace(os.Getenv(ttsLocalEnv)) != "" {
		return nil, err
	}
	return textToSpeechFromOptions(client, ttsbroker.OpenAISpeechOptions{
		APIURL: apiURL,
		APIKey: os.Getenv(ttsAPIKeyEnv),
		Model:  os.Getenv(ttsModelEnv),
		Voice:  os.Getenv(ttsVoiceEnv),
		Format: os.Getenv(ttsFormatEnv),
		Local:  local,
	})
}

func textToSpeechFromOptions(client mmapi.Client, options ttsbroker.OpenAISpeechOptions) (*ttsbroker.Service, error) {
	options.HTTPClient = http.DefaultClient
	synthesizer, err := ttsbroker.NewOpenAISpeechSynthesizer(options)
	if err != nil {
		return nil, err
	}
	return ttsbroker.New(ttsbroker.Options{
		Files:       client,
		Synthesizer: synthesizer,
	}), nil
}
