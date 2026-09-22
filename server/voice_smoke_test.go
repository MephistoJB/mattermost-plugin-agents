// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/bifrost"
	"github.com/mattermost/mattermost-plugin-agents/v2/ttsbroker"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/require"
)

func TestRealLocalTextToSpeechSmoke(t *testing.T) {
	if os.Getenv("MM_AGENTS_LOCAL_TTS_SMOKE") != "1" {
		t.Skip("set MM_AGENTS_LOCAL_TTS_SMOKE=1 to run against a real local OpenAI-compatible text-to-speech endpoint")
	}
	apiURL := os.Getenv(ttsAPIURLEnv)
	require.NotEmpty(t, apiURL, ttsAPIURLEnv+" is required")

	format := os.Getenv(ttsFormatEnv)
	if strings.TrimSpace(format) == "" {
		format = "mp3"
	}
	text := os.Getenv("MM_AGENTS_LOCAL_TTS_TEXT")
	if strings.TrimSpace(text) == "" {
		text = "Mattermost local text to speech smoke test."
	}

	synthesizer, err := ttsbroker.NewOpenAISpeechSynthesizer(ttsbroker.OpenAISpeechOptions{
		APIURL: apiURL,
		APIKey: os.Getenv(ttsAPIKeyEnv),
		Model:  os.Getenv(ttsModelEnv),
		Voice:  os.Getenv(ttsVoiceEnv),
		Format: format,
		Local:  true,
	})
	require.NoError(t, err)
	require.True(t, synthesizer.IsLocal(), "TTS smoke must use a local synthesizer")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	audio, err := synthesizer.Synthesize(ctx, text)
	require.NoError(t, err)
	require.NotNil(t, audio)
	require.NotNil(t, audio.Content)
	defer audio.Content.Close()

	body, err := io.ReadAll(io.LimitReader(audio.Content, 8*1024*1024))
	require.NoError(t, err)
	require.NotEmpty(t, body)
	require.Contains(t, audio.FileName, "."+format)
}

func TestRealLocalSpeechToTextSmoke(t *testing.T) {
	if os.Getenv("MM_AGENTS_LOCAL_STT_SMOKE") != "1" {
		t.Skip("set MM_AGENTS_LOCAL_STT_SMOKE=1 to run against a real local OpenAI-compatible transcription endpoint")
	}
	apiURL := os.Getenv("MM_AGENTS_LOCAL_STT_API_URL")
	audioPath := os.Getenv("MM_AGENTS_LOCAL_STT_AUDIO_FILE")
	require.NotEmpty(t, apiURL, "MM_AGENTS_LOCAL_STT_API_URL is required")
	require.NotEmpty(t, audioPath, "MM_AGENTS_LOCAL_STT_AUDIO_FILE is required")

	model := os.Getenv("MM_AGENTS_LOCAL_STT_MODEL")
	if strings.TrimSpace(model) == "" {
		model = "whisper-1"
	}
	expected := strings.TrimSpace(os.Getenv("MM_AGENTS_LOCAL_STT_EXPECTED"))

	file, err := os.Open(audioPath)
	require.NoError(t, err)
	defer file.Close()

	transcriber, err := bifrost.NewTranscriber(bifrost.TranscriptionConfig{
		Provider: schemas.OpenAI,
		APIKey:   os.Getenv("MM_AGENTS_LOCAL_STT_API_KEY"),
		APIURL:   apiURL,
		Model:    model,
	})
	require.NoError(t, err)
	defer transcriber.Shutdown()

	transcript, err := transcriber.Transcribe(file)
	require.NoError(t, err)
	require.NotNil(t, transcript)
	text := transcript.FormatTextOnly()
	require.NotEmpty(t, strings.TrimSpace(text))
	if expected != "" {
		require.Contains(t, strings.ToLower(text), strings.ToLower(expected))
	}
}
