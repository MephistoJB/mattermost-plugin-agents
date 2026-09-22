// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/config"
	"github.com/stretchr/testify/require"
)

func TestTextToSpeechFromEnvDisabled(t *testing.T) {
	t.Setenv(ttsAPIURLEnv, "")

	service, err := textToSpeechFromEnv(nil)
	require.NoError(t, err)
	require.Nil(t, service)
}

func TestTextToSpeechFromEnvLocal(t *testing.T) {
	t.Setenv(ttsAPIURLEnv, "http://localhost:8080")
	t.Setenv(ttsLocalEnv, "true")

	service, err := textToSpeechFromEnv(nil)
	require.NoError(t, err)
	require.NotNil(t, service)
	require.True(t, service.IsLocal())
}

func TestTextToSpeechFromConfig(t *testing.T) {
	t.Setenv(ttsAPIURLEnv, "")

	service, err := textToSpeechFromConfigOrEnv(nil, &config.Config{
		TextToSpeech: config.TextToSpeechConfig{
			Enabled: true,
			APIURL:  "http://host.docker.internal:8092",
			Model:   "local-flite",
			Voice:   "slt",
			Format:  "mp3",
			Local:   true,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, service)
	require.True(t, service.IsLocal())
}
