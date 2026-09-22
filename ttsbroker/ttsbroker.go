// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package ttsbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

const defaultTimeout = 60 * time.Second

type Audio struct {
	Content     io.ReadCloser
	ContentType string
	FileName    string
}

type Synthesizer interface {
	Synthesize(ctx context.Context, text string) (*Audio, error)
	IsLocal() bool
}

type FileClient interface {
	GetPost(postID string) (*model.Post, error)
	UpdatePost(post *model.Post) error
	UploadFile(content io.Reader, fileName, channelID string) (*model.FileInfo, error)
	LogError(msg string, keyValuePairs ...interface{})
}

type Service struct {
	files       FileClient
	synthesizer Synthesizer
}

type Options struct {
	Files       FileClient
	Synthesizer Synthesizer
}

func New(options Options) *Service {
	return &Service{
		files:       options.Files,
		synthesizer: options.Synthesizer,
	}
}

func (s *Service) Enabled() bool {
	return s != nil && s.files != nil && s.synthesizer != nil
}

func (s *Service) IsLocal() bool {
	return s != nil && s.synthesizer != nil && s.synthesizer.IsLocal()
}

func (s *Service) AttachResponseAudio(ctx context.Context, postID, channelID, text string) error {
	if !s.Enabled() || strings.TrimSpace(text) == "" {
		return nil
	}

	audio, err := s.synthesizer.Synthesize(ctx, text)
	if err != nil {
		return err
	}
	if audio == nil || audio.Content == nil {
		return fmt.Errorf("text-to-speech returned no audio")
	}
	defer audio.Content.Close()

	fileName := strings.TrimSpace(audio.FileName)
	if fileName == "" {
		fileName = "agent-response.mp3"
	}
	fileInfo, err := s.files.UploadFile(audio.Content, fileName, channelID)
	if err != nil {
		return err
	}
	if fileInfo == nil || strings.TrimSpace(fileInfo.Id) == "" {
		return fmt.Errorf("uploaded text-to-speech audio has no file id")
	}

	post, err := s.files.GetPost(postID)
	if err != nil {
		return err
	}
	if post == nil {
		return fmt.Errorf("post %q not found", postID)
	}
	if !contains(post.FileIds, fileInfo.Id) {
		post.FileIds = append(post.FileIds, fileInfo.Id)
	}
	return s.files.UpdatePost(post)
}

type OpenAISpeechSynthesizer struct {
	apiURL     string
	apiKey     string
	model      string
	voice      string
	format     string
	httpClient *http.Client
	local      bool
}

type OpenAISpeechOptions struct {
	APIURL     string
	APIKey     string
	Model      string
	Voice      string
	Format     string
	HTTPClient *http.Client
	Local      bool
}

func NewOpenAISpeechSynthesizer(options OpenAISpeechOptions) (*OpenAISpeechSynthesizer, error) {
	apiURL := strings.TrimSpace(options.APIURL)
	if apiURL == "" {
		return nil, fmt.Errorf("api url is required")
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = "gpt-4o-mini-tts"
	}
	voice := strings.TrimSpace(options.Voice)
	if voice == "" {
		voice = "alloy"
	}
	format := strings.TrimSpace(options.Format)
	if format == "" {
		format = "mp3"
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	return &OpenAISpeechSynthesizer{
		apiURL:     strings.TrimRight(apiURL, "/"),
		apiKey:     strings.TrimSpace(options.APIKey),
		model:      model,
		voice:      voice,
		format:     format,
		httpClient: httpClient,
		local:      options.Local || IsLocalAPIURL(apiURL),
	}, nil
}

func (s *OpenAISpeechSynthesizer) IsLocal() bool {
	return s != nil && s.local
}

func (s *OpenAISpeechSynthesizer) Synthesize(ctx context.Context, text string) (*Audio, error) {
	if s == nil {
		return nil, fmt.Errorf("text-to-speech synthesizer is nil")
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}

	body, err := json.Marshal(map[string]string{
		"model":           s.model,
		"voice":           s.voice,
		"input":           trimmed,
		"response_format": s.format,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURLPath(s.apiURL, "/v1/audio/speech"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("text-to-speech request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = contentTypeForFormat(s.format)
	}
	return &Audio{
		Content:     resp.Body,
		ContentType: contentType,
		FileName:    "agent-response." + extensionForFormat(s.format),
	}, nil
}

func IsLocalAPIURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "host.docker.internal") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return strings.HasSuffix(strings.ToLower(host), ".local")
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

func joinURLPath(base, suffix string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return strings.TrimRight(base, "/") + suffix
	}
	parsed.Path = path.Join(parsed.Path, suffix)
	return parsed.String()
}

func extensionForFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return "wav"
	case "opus":
		return "opus"
	case "aac":
		return "aac"
	case "flac":
		return "flac"
	default:
		return "mp3"
	}
}

func contentTypeForFormat(format string) string {
	switch extensionForFormat(format) {
	case "wav":
		return "audio/wav"
	case "opus":
		return "audio/opus"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	default:
		return "audio/mpeg"
	}
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
