// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package workspacefiles

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost/server/public/model"
)

const DefaultMaxRuntimeAttachmentBytes int64 = 25 * 1024 * 1024

var (
	ErrFileNotAllowed       = errors.New("runtime file is not allowed by workspace policy")
	ErrAttachmentNotAllowed = errors.New("runtime attachment is not allowed")
)

var defaultAllowedRuntimeAttachmentMIMETypes = []string{
	"text/",
	"image/",
	"application/json",
	"application/xml",
	"application/pdf",
	"application/csv",
	"application/x-yaml",
	"application/yaml",
	"application/msword",
	"application/vnd.ms-",
	"application/vnd.openxmlformats-officedocument.",
}

type FileClient interface {
	UploadFile(content io.Reader, fileName, channelID string) (*model.FileInfo, error)
	GetFileInfo(fileID string) (*model.FileInfo, error)
	GetFile(fileID string) (io.ReadCloser, error)
}

type UploadClient interface {
	UploadFile(content io.Reader, fileName, channelID string) (*model.FileInfo, error)
}

type AttachmentClient interface {
	GetFileInfo(fileID string) (*model.FileInfo, error)
	GetFile(fileID string) (io.ReadCloser, error)
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Service struct {
	uploadClient     UploadClient
	attachmentClient AttachmentClient
	httpClient       HTTPClient
	maxImportBytes   int64
	allowedMIMETypes []string
	allowPrivateURLs bool
}

type Options struct {
	Client                   FileClient
	UploadClient             UploadClient
	AttachmentClient         AttachmentClient
	HTTPClient               HTTPClient
	MaxImportBytes           int64
	AllowedImportMIMETypes   []string
	AllowPrivateDownloadURLs bool
}

func New(options Options) *Service {
	uploadClient := options.UploadClient
	attachmentClient := options.AttachmentClient
	if options.Client != nil {
		if uploadClient == nil {
			uploadClient = options.Client
		}
		if attachmentClient == nil {
			attachmentClient = options.Client
		}
	}
	maxImportBytes := options.MaxImportBytes
	if maxImportBytes == 0 {
		maxImportBytes = DefaultMaxRuntimeAttachmentBytes
	}
	allowedMIMETypes := options.AllowedImportMIMETypes
	if allowedMIMETypes == nil {
		allowedMIMETypes = defaultAllowedRuntimeAttachmentMIMETypes
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	service := &Service{
		uploadClient:     uploadClient,
		attachmentClient: attachmentClient,
		httpClient:       httpClient,
		maxImportBytes:   maxImportBytes,
		allowedMIMETypes: normalizeMIMEPatterns(allowedMIMETypes),
		allowPrivateURLs: options.AllowPrivateDownloadURLs,
	}
	if client, ok := httpClient.(*http.Client); ok {
		client.CheckRedirect = service.checkDownloadRedirect
	}
	return service
}

func (s *Service) Enabled() bool {
	return s != nil && s.uploadClient != nil
}

func (s *Service) AttachmentImportEnabled() bool {
	return s != nil && (s.attachmentClient != nil || s.httpClient != nil)
}

func (s *Service) UploadRuntimeFile(ctx context.Context, channelID, workspacePath string, payload agentruntime.RuntimeFileCreatedPayload) (string, error) {
	if !s.Enabled() {
		return "", nil
	}
	reader, fileName, closeFn, err := runtimeFileReader(workspacePath, payload)
	if err != nil {
		return "", err
	}
	if closeFn != nil {
		defer closeFn()
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	fileInfo, err := s.uploadClient.UploadFile(reader, fileName, channelID)
	if err != nil {
		return "", err
	}
	if fileInfo == nil || strings.TrimSpace(fileInfo.Id) == "" {
		return "", fmt.Errorf("uploaded runtime file has no file id")
	}
	return fileInfo.Id, nil
}

func (s *Service) ImportRuntimeAttachments(ctx context.Context, session agentruntime.RuntimeSession, attachments []agentruntime.RuntimeAttachment) ([]agentruntime.RuntimeAttachment, error) {
	if len(attachments) == 0 || !s.AttachmentImportEnabled() || strings.TrimSpace(session.WorkspacePath) == "" {
		return attachments, nil
	}

	cleanWorkspace, err := filepath.Abs(session.WorkspacePath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFileNotAllowed, err.Error())
	}
	targetDir := filepath.Join(cleanWorkspace, ".mattermost-agent-attachments", safePathSegment(session.ID, "session"))
	cleanTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFileNotAllowed, err.Error())
	}
	if !pathWithinRoot(filepath.Clean(cleanTargetDir), filepath.Clean(cleanWorkspace)) {
		return nil, fmt.Errorf("%w: %s", ErrFileNotAllowed, cleanTargetDir)
	}
	if err := os.MkdirAll(cleanTargetDir, 0o700); err != nil {
		return nil, err
	}

	imported := make([]agentruntime.RuntimeAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.FileID) == "" && strings.TrimSpace(attachment.DownloadURL) == "" {
			imported = append(imported, attachment)
			continue
		}
		next, err := s.importRuntimeAttachment(ctx, cleanTargetDir, attachment)
		if err != nil {
			return nil, err
		}
		imported = append(imported, next)
	}
	return imported, nil
}

func (s *Service) importRuntimeAttachment(ctx context.Context, targetDir string, attachment agentruntime.RuntimeAttachment) (agentruntime.RuntimeAttachment, error) {
	select {
	case <-ctx.Done():
		return agentruntime.RuntimeAttachment{}, ctx.Err()
	default:
	}
	if strings.TrimSpace(attachment.DownloadURL) != "" && strings.TrimSpace(attachment.FileID) == "" {
		return s.importRuntimeAttachmentURL(ctx, targetDir, attachment)
	}
	if s.attachmentClient == nil {
		return agentruntime.RuntimeAttachment{}, fmt.Errorf("%w: attachment client is not configured", ErrAttachmentNotAllowed)
	}

	info, err := s.attachmentClient.GetFileInfo(attachment.FileID)
	if err != nil {
		return agentruntime.RuntimeAttachment{}, err
	}
	if info == nil {
		return agentruntime.RuntimeAttachment{}, fmt.Errorf("file %q not found", attachment.FileID)
	}
	if err := s.validateRuntimeAttachmentInfo(firstNonEmpty(attachment.MimeType, info.MimeType), info.Size); err != nil {
		return agentruntime.RuntimeAttachment{}, err
	}
	source, err := s.attachmentClient.GetFile(attachment.FileID)
	if err != nil {
		return agentruntime.RuntimeAttachment{}, err
	}
	defer source.Close()

	fileName := safeFileName(firstNonEmpty(attachment.Name, info.Name), attachment.FileID)
	targetPath := filepath.Join(targetDir, safePathSegment(attachment.FileID, "file")+"-"+fileName)
	cleanTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return agentruntime.RuntimeAttachment{}, fmt.Errorf("%w: %s", ErrFileNotAllowed, err.Error())
	}
	if !pathWithinRoot(filepath.Clean(cleanTargetPath), filepath.Clean(targetDir)) {
		return agentruntime.RuntimeAttachment{}, fmt.Errorf("%w: %s", ErrFileNotAllowed, cleanTargetPath)
	}

	target, err := os.OpenFile(cleanTargetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return agentruntime.RuntimeAttachment{}, err
	}
	defer target.Close()
	if _, err := io.Copy(target, limitedReader(source, s.maxImportBytes)); err != nil {
		_ = os.Remove(cleanTargetPath)
		return agentruntime.RuntimeAttachment{}, err
	}

	attachment.Name = fileName
	attachment.MimeType = firstNonEmpty(attachment.MimeType, info.MimeType)
	attachment.LocalPath = cleanTargetPath
	return attachment, nil
}

func (s *Service) importRuntimeAttachmentURL(ctx context.Context, targetDir string, attachment agentruntime.RuntimeAttachment) (agentruntime.RuntimeAttachment, error) {
	downloadURL := strings.TrimSpace(attachment.DownloadURL)
	if err := s.validateDownloadURL(downloadURL); err != nil {
		return agentruntime.RuntimeAttachment{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return agentruntime.RuntimeAttachment{}, fmt.Errorf("%w: %s", ErrAttachmentNotAllowed, err.Error())
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return agentruntime.RuntimeAttachment{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return agentruntime.RuntimeAttachment{}, fmt.Errorf("%w: download returned status %d", ErrAttachmentNotAllowed, resp.StatusCode)
	}
	mimeType := firstNonEmpty(attachment.MimeType, mediaType(resp.Header.Get("Content-Type")))
	if err := s.validateRuntimeAttachmentInfo(mimeType, resp.ContentLength); err != nil {
		return agentruntime.RuntimeAttachment{}, err
	}
	fileName := safeFileName(firstNonEmpty(attachment.Name, fileNameFromURL(downloadURL)), "download")
	targetPath := filepath.Join(targetDir, safePathSegment(fileName, "download"))
	cleanTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return agentruntime.RuntimeAttachment{}, fmt.Errorf("%w: %s", ErrFileNotAllowed, err.Error())
	}
	if !pathWithinRoot(filepath.Clean(cleanTargetPath), filepath.Clean(targetDir)) {
		return agentruntime.RuntimeAttachment{}, fmt.Errorf("%w: %s", ErrFileNotAllowed, cleanTargetPath)
	}

	target, err := os.OpenFile(cleanTargetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return agentruntime.RuntimeAttachment{}, err
	}
	defer target.Close()
	if _, err := io.Copy(target, limitedReader(resp.Body, s.maxImportBytes)); err != nil {
		_ = os.Remove(cleanTargetPath)
		return agentruntime.RuntimeAttachment{}, err
	}
	attachment.Name = fileName
	attachment.MimeType = mimeType
	attachment.LocalPath = cleanTargetPath
	return attachment, nil
}

func (s *Service) validateRuntimeAttachmentInfo(mimeType string, size int64) error {
	if s.maxImportBytes > 0 && size > s.maxImportBytes {
		return fmt.Errorf("%w: file exceeds %d bytes", ErrAttachmentNotAllowed, s.maxImportBytes)
	}
	if !mimeAllowed(mimeType, s.allowedMIMETypes) {
		return fmt.Errorf("%w: mime type %q is not allowed", ErrAttachmentNotAllowed, mimeType)
	}
	return nil
}

func (s *Service) validateDownloadURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: invalid download url", ErrAttachmentNotAllowed)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%w: unsupported download url scheme", ErrAttachmentNotAllowed)
	}
	if parsed.User != nil {
		return fmt.Errorf("%w: download url credentials are not allowed", ErrAttachmentNotAllowed)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("%w: download url host is required", ErrAttachmentNotAllowed)
	}
	if s.allowPrivateURLs {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("%w: download url host cannot be resolved", ErrAttachmentNotAllowed)
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return fmt.Errorf("%w: download url host resolves to a private address", ErrAttachmentNotAllowed)
		}
	}
	return nil
}

func (s *Service) checkDownloadRedirect(req *http.Request, _ []*http.Request) error {
	return s.validateDownloadURL(req.URL.String())
}

func limitedReader(reader io.Reader, maxBytes int64) io.Reader {
	if maxBytes <= 0 {
		return reader
	}
	return &maxBytesReader{reader: reader, remaining: maxBytes}
}

type maxBytesReader struct {
	reader          io.Reader
	remaining       int64
	exhaustionProbe bool
}

func (r *maxBytesReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		if r.exhaustionProbe {
			return 0, io.EOF
		}
		r.exhaustionProbe = true
		var probe [1]byte
		n, err := r.reader.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("%w: file exceeds configured byte limit", ErrAttachmentNotAllowed)
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func normalizeMIMEPatterns(patterns []string) []string {
	normalized := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern != "" {
			normalized = append(normalized, pattern)
		}
	}
	return normalized
}

func mimeAllowed(value string, patterns []string) bool {
	media := mediaType(value)
	if media == "" || len(patterns) == 0 {
		return false
	}
	for _, pattern := range patterns {
		if strings.HasSuffix(pattern, "/") || strings.HasSuffix(pattern, ".") {
			if strings.HasPrefix(media, pattern) {
				return true
			}
			continue
		}
		if media == pattern {
			return true
		}
	}
	return false
}

func mediaType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, _, err := mime.ParseMediaType(value)
	if err != nil {
		return strings.ToLower(value)
	}
	return strings.ToLower(parsed)
}

func fileNameFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return filepath.Base(parsed.EscapedPath())
}

func publicIP(ip net.IP) bool {
	return ip != nil &&
		ip.IsGlobalUnicast() &&
		!ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsMulticast() &&
		!ip.IsUnspecified()
}

func PayloadFromEvent(event agentruntime.RuntimeEvent) (agentruntime.RuntimeFileCreatedPayload, error) {
	if len(event.Payload) == 0 {
		return agentruntime.RuntimeFileCreatedPayload{}, errors.New("runtime file payload is required")
	}
	var payload agentruntime.RuntimeFileCreatedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return agentruntime.RuntimeFileCreatedPayload{}, fmt.Errorf("failed to decode runtime file payload: %w", err)
	}
	return payload, nil
}

func runtimeFileReader(workspacePath string, payload agentruntime.RuntimeFileCreatedPayload) (io.Reader, string, func(), error) {
	fileName := strings.TrimSpace(payload.FileName)
	if payload.ContentBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(payload.ContentBase64)
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to decode runtime file content: %w", err)
		}
		if fileName == "" {
			fileName = "runtime-file"
		}
		fileName = safeFileName(fileName, "runtime-file")
		return bytes.NewReader(decoded), fileName, nil, nil
	}
	if payload.Content != "" {
		if fileName == "" {
			fileName = "runtime-file.txt"
		}
		fileName = safeFileName(fileName, "runtime-file.txt")
		return strings.NewReader(payload.Content), fileName, nil, nil
	}

	filePath := strings.TrimSpace(payload.Path)
	if filePath == "" {
		return nil, "", nil, errors.New("runtime file path or inline content is required")
	}
	if strings.TrimSpace(workspacePath) == "" {
		return nil, "", nil, fmt.Errorf("%w: workspace path is required", ErrFileNotAllowed)
	}

	cleanWorkspace, err := filepath.Abs(workspacePath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("%w: %s", ErrFileNotAllowed, err.Error())
	}
	cleanFile, err := filepath.Abs(filePath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("%w: %s", ErrFileNotAllowed, err.Error())
	}
	if !pathWithinRoot(filepath.Clean(cleanFile), filepath.Clean(cleanWorkspace)) {
		return nil, "", nil, fmt.Errorf("%w: %s", ErrFileNotAllowed, cleanFile)
	}
	realWorkspace, err := filepath.EvalSymlinks(cleanWorkspace)
	if err != nil {
		return nil, "", nil, fmt.Errorf("%w: %s", ErrFileNotAllowed, err.Error())
	}
	realFile, err := filepath.EvalSymlinks(cleanFile)
	if err != nil {
		return nil, "", nil, fmt.Errorf("%w: %s", ErrFileNotAllowed, err.Error())
	}
	if !pathWithinRoot(filepath.Clean(realFile), filepath.Clean(realWorkspace)) {
		return nil, "", nil, fmt.Errorf("%w: %s", ErrFileNotAllowed, realFile)
	}

	file, err := os.Open(realFile)
	if err != nil {
		return nil, "", nil, err
	}
	if fileName == "" {
		fileName = filepath.Base(realFile)
	}
	fileName = safeFileName(fileName, "runtime-file")
	return file, fileName, func() { _ = file.Close() }, nil
}

func pathWithinRoot(candidate, root string) bool {
	if candidate == root {
		return true
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func safeFileName(name, fallback string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = fallback
	}
	return safePathSegment(base, "attachment")
}

func safePathSegment(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	value = filepath.Base(value)
	replacer := strings.NewReplacer("/", "_", "\\", "_", "\x00", "_")
	value = replacer.Replace(value)
	if value == "." || value == ".." || value == "" {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
