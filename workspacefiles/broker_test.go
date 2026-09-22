// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package workspacefiles

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/require"
)

func TestUploadRuntimeFileFromWorkspacePath(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "answer.txt")
	require.NoError(t, os.WriteFile(path, []byte("answer"), 0o600))
	client := &fakeUploadClient{}
	service := New(Options{UploadClient: client})

	fileID, err := service.UploadRuntimeFile(context.Background(), "channel-id", workspace, agentruntime.RuntimeFileCreatedPayload{Path: path})

	require.NoError(t, err)
	require.Equal(t, "file-id", fileID)
	require.Equal(t, "answer.txt", client.fileName)
	require.Equal(t, "channel-id", client.channelID)
	require.Equal(t, "answer", client.content)
}

func TestUploadRuntimeFileBlocksPathOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	service := New(Options{UploadClient: &fakeUploadClient{}})

	_, err := service.UploadRuntimeFile(context.Background(), "channel-id", workspace, agentruntime.RuntimeFileCreatedPayload{Path: outside})

	require.ErrorIs(t, err, ErrFileNotAllowed)
}

func TestUploadRuntimeFileBlocksSymlinkOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	link := filepath.Join(workspace, "link.txt")
	require.NoError(t, os.Symlink(outside, link))
	service := New(Options{UploadClient: &fakeUploadClient{}})

	_, err := service.UploadRuntimeFile(context.Background(), "channel-id", workspace, agentruntime.RuntimeFileCreatedPayload{Path: link})

	require.ErrorIs(t, err, ErrFileNotAllowed)
}

func TestUploadRuntimeFileAllowsSymlinkInsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("inside"), 0o600))
	link := filepath.Join(workspace, "link.txt")
	require.NoError(t, os.Symlink(target, link))
	client := &fakeUploadClient{}
	service := New(Options{UploadClient: client})

	fileID, err := service.UploadRuntimeFile(context.Background(), "channel-id", workspace, agentruntime.RuntimeFileCreatedPayload{Path: link})

	require.NoError(t, err)
	require.Equal(t, "file-id", fileID)
	require.Equal(t, "target.txt", client.fileName)
	require.Equal(t, "inside", client.content)
}

func TestUploadRuntimeFileFromInlineContent(t *testing.T) {
	client := &fakeUploadClient{}
	service := New(Options{UploadClient: client})

	fileID, err := service.UploadRuntimeFile(context.Background(), "channel-id", "", agentruntime.RuntimeFileCreatedPayload{
		FileName:      "../report.txt",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("report")),
	})

	require.NoError(t, err)
	require.Equal(t, "file-id", fileID)
	require.Equal(t, "report.txt", client.fileName)
	require.Equal(t, "report", client.content)
}

func TestImportRuntimeAttachments(t *testing.T) {
	workspace := t.TempDir()
	client := &fakeAttachmentClient{
		info: &model.FileInfo{
			Id:       "file-id",
			Name:     "../input.txt",
			MimeType: "text/plain",
		},
		content: "input",
	}
	service := New(Options{AttachmentClient: client})

	attachments, err := service.ImportRuntimeAttachments(context.Background(), agentruntime.RuntimeSession{
		ID:            "session-id",
		WorkspacePath: workspace,
	}, []agentruntime.RuntimeAttachment{{FileID: "file-id"}})

	require.NoError(t, err)
	require.Len(t, attachments, 1)
	require.Equal(t, "input.txt", attachments[0].Name)
	require.Equal(t, "text/plain", attachments[0].MimeType)
	require.Contains(t, attachments[0].LocalPath, filepath.Join(workspace, ".mattermost-agent-attachments", "session-id"))
	body, err := os.ReadFile(attachments[0].LocalPath)
	require.NoError(t, err)
	require.Equal(t, "input", string(body))
}

func TestImportRuntimeAttachmentsRejectsFileInfoOverLimit(t *testing.T) {
	workspace := t.TempDir()
	client := &fakeAttachmentClient{
		info: &model.FileInfo{
			Id:       "file-id",
			Name:     "input.txt",
			MimeType: "text/plain",
			Size:     6,
		},
		content: "input",
	}
	service := New(Options{AttachmentClient: client, MaxImportBytes: 5})

	_, err := service.ImportRuntimeAttachments(context.Background(), agentruntime.RuntimeSession{
		ID:            "session-id",
		WorkspacePath: workspace,
	}, []agentruntime.RuntimeAttachment{{FileID: "file-id"}})

	require.ErrorIs(t, err, ErrAttachmentNotAllowed)
	require.Equal(t, 0, client.getFileCalls)
}

func TestImportRuntimeAttachmentsRejectsDisallowedMIMEType(t *testing.T) {
	workspace := t.TempDir()
	client := &fakeAttachmentClient{
		info: &model.FileInfo{
			Id:       "file-id",
			Name:     "payload.exe",
			MimeType: "application/x-msdownload",
		},
		content: "payload",
	}
	service := New(Options{AttachmentClient: client})

	_, err := service.ImportRuntimeAttachments(context.Background(), agentruntime.RuntimeSession{
		ID:            "session-id",
		WorkspacePath: workspace,
	}, []agentruntime.RuntimeAttachment{{FileID: "file-id"}})

	require.ErrorIs(t, err, ErrAttachmentNotAllowed)
	require.Equal(t, 0, client.getFileCalls)
}

func TestImportRuntimeAttachmentsRejectsStreamOverLimit(t *testing.T) {
	workspace := t.TempDir()
	client := &fakeAttachmentClient{
		info: &model.FileInfo{
			Id:       "file-id",
			Name:     "input.txt",
			MimeType: "text/plain",
		},
		content: "123456",
	}
	service := New(Options{AttachmentClient: client, MaxImportBytes: 5})

	_, err := service.ImportRuntimeAttachments(context.Background(), agentruntime.RuntimeSession{
		ID:            "session-id",
		WorkspacePath: workspace,
	}, []agentruntime.RuntimeAttachment{{FileID: "file-id"}})

	require.ErrorIs(t, err, ErrAttachmentNotAllowed)
}

func TestImportRuntimeAttachmentFromDownloadURL(t *testing.T) {
	workspace := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("download"))
	}))
	defer server.Close()
	service := New(Options{AllowPrivateDownloadURLs: true, MaxImportBytes: 20})

	attachments, err := service.ImportRuntimeAttachments(context.Background(), agentruntime.RuntimeSession{
		ID:            "session-id",
		WorkspacePath: workspace,
	}, []agentruntime.RuntimeAttachment{{DownloadURL: server.URL + "/input.txt"}})

	require.NoError(t, err)
	require.Len(t, attachments, 1)
	require.Equal(t, "input.txt", attachments[0].Name)
	require.Equal(t, "text/plain", attachments[0].MimeType)
	body, err := os.ReadFile(attachments[0].LocalPath)
	require.NoError(t, err)
	require.Equal(t, "download", string(body))
}

func TestImportRuntimeAttachmentDownloadURLBlocksPrivateHostsByDefault(t *testing.T) {
	workspace := t.TempDir()
	service := New(Options{})

	_, err := service.ImportRuntimeAttachments(context.Background(), agentruntime.RuntimeSession{
		ID:            "session-id",
		WorkspacePath: workspace,
	}, []agentruntime.RuntimeAttachment{{DownloadURL: "http://127.0.0.1/input.txt"}})

	require.ErrorIs(t, err, ErrAttachmentNotAllowed)
}

func TestPayloadFromEvent(t *testing.T) {
	raw, err := json.Marshal(agentruntime.RuntimeFileCreatedPayload{FileName: "report.txt", Content: "report"})
	require.NoError(t, err)

	payload, err := PayloadFromEvent(agentruntime.RuntimeEvent{Payload: raw})

	require.NoError(t, err)
	require.Equal(t, "report.txt", payload.FileName)
	require.Equal(t, "report", payload.Content)
}

type fakeUploadClient struct {
	fileName  string
	channelID string
	content   string
}

func (f *fakeUploadClient) UploadFile(content io.Reader, fileName, channelID string) (*model.FileInfo, error) {
	body, err := io.ReadAll(content)
	if err != nil {
		return nil, err
	}
	f.fileName = fileName
	f.channelID = channelID
	f.content = string(body)
	return &model.FileInfo{Id: "file-id"}, nil
}

type fakeAttachmentClient struct {
	info         *model.FileInfo
	content      string
	getFileCalls int
}

func (f *fakeAttachmentClient) GetFileInfo(string) (*model.FileInfo, error) {
	return f.info, nil
}

func (f *fakeAttachmentClient) GetFile(string) (io.ReadCloser, error) {
	f.getFileCalls++
	return io.NopCloser(strings.NewReader(f.content)), nil
}
