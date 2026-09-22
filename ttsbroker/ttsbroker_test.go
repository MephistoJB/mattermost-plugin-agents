// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package ttsbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/require"
)

func TestIsLocalAPIURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "localhost", url: "http://localhost:8000", want: true},
		{name: "docker host", url: "http://host.docker.internal:8000", want: true},
		{name: "private ip", url: "http://192.168.1.10:8000", want: true},
		{name: "local mdns", url: "http://speech.local:8000", want: true},
		{name: "public", url: "https://api.openai.com", want: false},
		{name: "invalid", url: ":", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsLocalAPIURL(tt.url))
		})
	}
}

func TestOpenAISpeechSynthesizerSynthesize(t *testing.T) {
	var gotAuth string
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/base/v1/audio/speech", r.URL.Path)
		gotAuth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()

	synth, err := NewOpenAISpeechSynthesizer(OpenAISpeechOptions{
		APIURL: server.URL + "/base",
		APIKey: "secret",
		Model:  "tts-model",
		Voice:  "voice-a",
	})
	require.NoError(t, err)

	audio, err := synth.Synthesize(context.Background(), "hello")
	require.NoError(t, err)
	defer audio.Content.Close()

	body, err := io.ReadAll(audio.Content)
	require.NoError(t, err)
	require.Equal(t, "audio", string(body))
	require.Equal(t, "Bearer secret", gotAuth)
	require.Equal(t, map[string]string{
		"model":           "tts-model",
		"voice":           "voice-a",
		"input":           "hello",
		"response_format": "mp3",
	}, gotBody)
	require.Equal(t, "agent-response.mp3", audio.FileName)
}

func TestServiceAttachResponseAudio(t *testing.T) {
	client := &fakeFileClient{
		post: &model.Post{
			Id:        "post-id",
			ChannelId: "channel-id",
			Message:   "answer",
			FileIds:   []string{"existing-file"},
		},
	}
	service := New(Options{
		Files:       client,
		Synthesizer: fakeSynthesizer{audio: []byte("audio")},
	})

	err := service.AttachResponseAudio(context.Background(), "post-id", "channel-id", "answer")
	require.NoError(t, err)
	require.Equal(t, "channel-id", client.uploadChannelID)
	require.Equal(t, "agent-response.mp3", client.uploadFileName)
	require.ElementsMatch(t, []string{"existing-file", "tts-file"}, []string(client.updated.FileIds))
}

type fakeSynthesizer struct {
	audio []byte
}

func (f fakeSynthesizer) IsLocal() bool { return true }

func (f fakeSynthesizer) Synthesize(context.Context, string) (*Audio, error) {
	return &Audio{
		Content:     io.NopCloser(bytes.NewReader(f.audio)),
		ContentType: "audio/mpeg",
		FileName:    "agent-response.mp3",
	}, nil
}

type fakeFileClient struct {
	post            *model.Post
	updated         *model.Post
	uploadFileName  string
	uploadChannelID string
}

func (f *fakeFileClient) GetPost(string) (*model.Post, error) {
	post := *f.post
	post.FileIds = append([]string(nil), f.post.FileIds...)
	return &post, nil
}

func (f *fakeFileClient) UpdatePost(post *model.Post) error {
	f.updated = post
	return nil
}

func (f *fakeFileClient) UploadFile(content io.Reader, fileName, channelID string) (*model.FileInfo, error) {
	f.uploadFileName = fileName
	f.uploadChannelID = channelID
	_, _ = io.ReadAll(content)
	return &model.FileInfo{Id: "tts-file"}, nil
}

func (f *fakeFileClient) LogError(string, ...interface{}) {}
