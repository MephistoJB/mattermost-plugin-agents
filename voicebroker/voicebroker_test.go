// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package voicebroker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/subtitles"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeFiles struct {
	infoByID map[string]*model.FileInfo
	dataByID map[string]string
}

func (f fakeFiles) GetFileInfo(fileID string) (*model.FileInfo, error) {
	info, ok := f.infoByID[fileID]
	if !ok {
		return nil, errors.New("missing info")
	}
	return info, nil
}

func (f fakeFiles) GetFile(fileID string) (io.ReadCloser, error) {
	data, ok := f.dataByID[fileID]
	if !ok {
		return nil, errors.New("missing data")
	}
	return io.NopCloser(strings.NewReader(data)), nil
}

type fakeTranscriber struct {
	seen string
}

func (t *fakeTranscriber) Transcribe(file io.Reader) (*subtitles.Subtitles, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	t.seen = string(data)
	return subtitles.NewSubtitlesFromVTT(strings.NewReader("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nhello from audio\n"))
}

func TestTranscribePostTranscribesAudioAttachmentsOnly(t *testing.T) {
	transcriber := &fakeTranscriber{}
	service := New(Options{
		Files: fakeFiles{
			infoByID: map[string]*model.FileInfo{
				"audio-1": {Id: "audio-1", ChannelId: "channel-1", Name: "voice.ogg", MimeType: "audio/ogg"},
				"text-1":  {Id: "text-1", ChannelId: "channel-1", Name: "notes.txt", MimeType: "text/plain"},
			},
			dataByID: map[string]string{"audio-1": "raw audio"},
		},
		TranscriberProvider: func() Transcriber { return transcriber },
	})

	result, err := service.TranscribePost(context.Background(), &model.Post{
		ChannelId: "channel-1",
		FileIds:   []string{"audio-1", "text-1"},
	})

	require.NoError(t, err)
	require.Len(t, result.Transcripts, 1)
	assert.Equal(t, "audio-1", result.Transcripts[0].FileID)
	assert.Equal(t, "voice.ogg", result.Transcripts[0].Name)
	assert.Equal(t, "hello from audio", result.Transcripts[0].Text)
	assert.Equal(t, "raw audio", transcriber.seen)
}

func TestTranscribePostRequiresTranscriberOnlyForAudio(t *testing.T) {
	service := New(Options{
		Files: fakeFiles{
			infoByID: map[string]*model.FileInfo{
				"text-1": {Id: "text-1", ChannelId: "channel-1", Name: "notes.txt", MimeType: "text/plain"},
			},
		},
	})

	result, err := service.TranscribePost(context.Background(), &model.Post{
		ChannelId: "channel-1",
		FileIds:   []string{"text-1"},
	})

	require.NoError(t, err)
	assert.Empty(t, result.Transcripts)
}

func TestTranscribePostRejectsCrossChannelAudio(t *testing.T) {
	service := New(Options{
		Files: fakeFiles{
			infoByID: map[string]*model.FileInfo{
				"audio-1": {Id: "audio-1", ChannelId: "other-channel", Name: "voice.ogg", MimeType: "audio/ogg"},
			},
		},
		TranscriberProvider: func() Transcriber { return &fakeTranscriber{} },
	})

	_, err := service.TranscribePost(context.Background(), &model.Post{
		ChannelId: "channel-1",
		FileIds:   []string{"audio-1"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong")
}

func TestIsAudioFileFallsBackToExtension(t *testing.T) {
	assert.True(t, IsAudioFile(&model.FileInfo{Name: "voice.m4a"}))
	assert.True(t, IsAudioFile(&model.FileInfo{Name: "voice.bin", MimeType: "audio/webm"}))
	assert.False(t, IsAudioFile(&model.FileInfo{Name: "notes.txt", MimeType: "text/plain"}))
}

func TestComposePrompt(t *testing.T) {
	prompt := ComposePrompt("please answer this", &Result{Transcripts: []Transcript{
		{FileID: "audio-1", Name: "voice.ogg", Text: "hello from audio"},
	}})

	assert.Contains(t, prompt, "Transcribed voice message:")
	assert.Contains(t, prompt, "[voice.ogg]")
	assert.Contains(t, prompt, "hello from audio")
	assert.Contains(t, prompt, "User text:")
	assert.Contains(t, prompt, "please answer this")
}
