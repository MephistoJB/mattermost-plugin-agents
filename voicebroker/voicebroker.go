// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package voicebroker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mattermost/mattermost-plugin-agents/v2/subtitles"
	"github.com/mattermost/mattermost/server/public/model"
)

type FileClient interface {
	GetFileInfo(fileID string) (*model.FileInfo, error)
	GetFile(fileID string) (io.ReadCloser, error)
}

type Transcriber interface {
	Transcribe(file io.Reader) (*subtitles.Subtitles, error)
}

type TranscriberProvider func() Transcriber

type Service struct {
	files               FileClient
	transcriberProvider TranscriberProvider
}

type Options struct {
	Files               FileClient
	TranscriberProvider TranscriberProvider
}

type Transcript struct {
	FileID string
	Name   string
	Text   string
}

type Result struct {
	Transcripts []Transcript
}

func New(opts Options) *Service {
	return &Service{
		files:               opts.Files,
		transcriberProvider: opts.TranscriberProvider,
	}
}

func (s *Service) TranscribePost(ctx context.Context, post *model.Post) (*Result, error) {
	if s == nil || s.files == nil {
		return &Result{}, nil
	}
	if post == nil || len(post.FileIds) == 0 {
		return &Result{}, nil
	}

	result := &Result{Transcripts: make([]Transcript, 0, len(post.FileIds))}
	for _, fileID := range post.FileIds {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		info, err := s.files.GetFileInfo(fileID)
		if err != nil {
			return nil, fmt.Errorf("failed to get audio file info %s: %w", fileID, err)
		}
		if info == nil || !IsAudioFile(info) {
			continue
		}
		if info.ChannelId != "" && post.ChannelId != "" && info.ChannelId != post.ChannelId {
			return nil, fmt.Errorf("audio file %s does not belong to post channel", fileID)
		}

		transcriber := s.transcriber()
		if transcriber == nil {
			return nil, errors.New("voice transcription is not configured")
		}

		reader, err := s.files.GetFile(fileID)
		if err != nil {
			return nil, fmt.Errorf("failed to read audio file %s: %w", fileID, err)
		}
		transcript, transcribeErr := transcriber.Transcribe(reader)
		closeErr := reader.Close()
		if transcribeErr != nil {
			return nil, fmt.Errorf("failed to transcribe audio file %s: %w", fileID, transcribeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("failed to close audio file %s: %w", fileID, closeErr)
		}
		if transcript == nil || transcript.IsEmpty() {
			continue
		}

		result.Transcripts = append(result.Transcripts, Transcript{
			FileID: fileID,
			Name:   info.Name,
			Text:   transcript.FormatTextOnly(),
		})
	}

	return result, nil
}

func (s *Service) transcriber() Transcriber {
	if s.transcriberProvider == nil {
		return nil
	}
	return s.transcriberProvider()
}

func IsAudioFile(info *model.FileInfo) bool {
	if info == nil {
		return false
	}
	if strings.HasPrefix(strings.ToLower(info.MimeType), "audio/") {
		return true
	}

	switch strings.ToLower(filepath.Ext(info.Name)) {
	case ".aac", ".aiff", ".flac", ".m4a", ".mp3", ".oga", ".ogg", ".opus", ".wav", ".webm", ".wma":
		return true
	default:
		return false
	}
}

func ComposePrompt(message string, result *Result) string {
	message = strings.TrimSpace(message)
	if result == nil || len(result.Transcripts) == 0 {
		return message
	}

	var b strings.Builder
	b.WriteString("Transcribed voice message")
	if len(result.Transcripts) > 1 {
		b.WriteString("s")
	}
	b.WriteString(":\n")
	for i, transcript := range result.Transcripts {
		if i > 0 {
			b.WriteString("\n")
		}
		name := strings.TrimSpace(transcript.Name)
		if name == "" {
			name = transcript.FileID
		}
		b.WriteString("[")
		b.WriteString(name)
		b.WriteString("]\n")
		b.WriteString(strings.TrimSpace(transcript.Text))
		b.WriteString("\n")
	}
	if message != "" {
		b.WriteString("\nUser text:\n")
		b.WriteString(message)
	}
	return strings.TrimSpace(b.String())
}
