// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package openaicodex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/mmapi"
)

const (
	authBaseURL       = "https://auth.openai.com"
	deviceUserCodeURL = authBaseURL + "/api/accounts/deviceauth/usercode"
	deviceTokenURL    = authBaseURL + "/api/accounts/deviceauth/token"
	oauthTokenURL     = authBaseURL + "/oauth/token"
	deviceVerifyURL   = authBaseURL + "/codex/device"
	deviceRedirectURI = authBaseURL + "/deviceauth/callback"
	clientID          = "app_EMoamEEZ73f0CkXaXp7hrann"
	tokenVersion      = 1
	sessionVersion    = 1
	sessionTTL        = 15 * time.Minute
	refreshEarly      = 120 * time.Second
	refreshLeaseTTL   = 35 * time.Second
	refreshLeaseWait  = 10 * time.Second
)

// ProviderCredentialSubject is the single server-side credential slot for the
// globally configured Mattermost Agents provider.
const ProviderCredentialSubject = "provider"

type ManagerConfig struct {
	Store      mmapi.Client
	HTTPClient *http.Client
}

type Manager struct {
	store      mmapi.Client
	httpClient *http.Client
}

type DeviceStart struct {
	SessionID       string    `json:"sessionID"`
	UserCode        string    `json:"userCode"`
	VerificationURI string    `json:"verificationURI"`
	ExpiresAt       time.Time `json:"expiresAt"`
	IntervalSeconds int       `json:"intervalSeconds"`
}

type Status struct {
	Connected bool       `json:"connected"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	LastError string     `json:"lastError,omitempty"`
}

type tokenEnvelope struct {
	Version      int       `json:"version"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	TokenType    string    `json:"tokenType"`
	Expiry       time.Time `json:"expiry"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type deviceSession struct {
	Version      int       `json:"version"`
	UserID       string    `json:"userID"`
	SessionID    string    `json:"sessionID"`
	UserCode     string    `json:"userCode"`
	DeviceAuthID string    `json:"deviceAuthID"`
	Interval     int       `json:"interval"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type tokenEndpointError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *tokenEndpointError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("token endpoint returned %q (HTTP %d)", e.Code, e.StatusCode)
	}
	return fmt.Sprintf("token endpoint returned HTTP %d", e.StatusCode)
}

func NewManager(cfg ManagerConfig) *Manager {
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Manager{
		store:      cfg.Store,
		httpClient: client,
	}
}

func (m *Manager) Status(userID string) (Status, error) {
	return m.statusForSubject(userID)
}

func (m *Manager) ProviderStatus() (Status, error) {
	return m.statusForSubject(ProviderCredentialSubject)
}

func (m *Manager) statusForSubject(subject string) (Status, error) {
	env, _, err := m.loadToken(subject)
	if err != nil {
		return Status{}, err
	}
	if env == nil {
		return Status{}, nil
	}
	return Status{
		Connected: true,
		ExpiresAt: &env.Expiry,
	}, nil
}

func (m *Manager) StartDeviceFlow(ctx context.Context, userID string) (*DeviceStart, error) {
	if m.store == nil {
		return nil, fmt.Errorf("%w: token store unavailable", ErrNeedsOAuth)
	}
	sessionID := requestID()
	reqBody, _ := json.Marshal(map[string]string{"client_id": clientID})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceUserCodeURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := noRedirectClient(m.httpClient).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseTokenEndpointError(resp.StatusCode, body)
	}

	var parsed struct {
		UserCode     string `json:"user_code"`
		DeviceAuthID string `json:"device_auth_id"`
		Interval     int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.UserCode == "" || parsed.DeviceAuthID == "" {
		return nil, errors.New("device authorization response is incomplete")
	}
	if parsed.Interval < 3 {
		parsed.Interval = 3
	}

	now := time.Now()
	session := &deviceSession{
		Version:      sessionVersion,
		UserID:       userID,
		SessionID:    sessionID,
		UserCode:     parsed.UserCode,
		DeviceAuthID: parsed.DeviceAuthID,
		Interval:     parsed.Interval,
		CreatedAt:    now,
		ExpiresAt:    now.Add(sessionTTL),
	}
	if err := m.store.KVSetWithExpiry(sessionKey(userID, sessionID), session, sessionTTL); err != nil {
		return nil, err
	}

	return &DeviceStart{
		SessionID:       sessionID,
		UserCode:        parsed.UserCode,
		VerificationURI: deviceVerifyURL,
		ExpiresAt:       session.ExpiresAt,
		IntervalSeconds: parsed.Interval,
	}, nil
}

func (m *Manager) PollDeviceFlow(ctx context.Context, userID, sessionID string) (Status, error) {
	session, err := m.loadSession(userID, sessionID)
	if err != nil {
		return Status{}, err
	}
	if session == nil || time.Now().After(session.ExpiresAt) {
		return Status{LastError: "expired"}, nil
	}

	reqBody, _ := json.Marshal(map[string]string{
		"device_auth_id": session.DeviceAuthID,
		"user_code":      session.UserCode,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceTokenURL, bytes.NewReader(reqBody))
	if err != nil {
		return Status{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := noRedirectClient(m.httpClient).Do(req)
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Status{}, err
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return Status{Connected: false}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Status{}, parseTokenEndpointError(resp.StatusCode, body)
	}

	var authorized struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}
	if err := json.Unmarshal(body, &authorized); err != nil {
		return Status{}, err
	}
	if authorized.AuthorizationCode == "" || authorized.CodeVerifier == "" {
		return Status{}, errors.New("device token response is incomplete")
	}

	token, err := m.exchangeAuthorizationCode(ctx, authorized.AuthorizationCode, authorized.CodeVerifier)
	if err != nil {
		return Status{}, err
	}
	if err := m.storeToken(ProviderCredentialSubject, token); err != nil {
		return Status{}, err
	}
	_ = m.store.KVDelete(sessionKey(userID, sessionID))
	expiresAt := token.Expiry
	return Status{Connected: true, ExpiresAt: &expiresAt}, nil
}

func (m *Manager) AccessToken(ctx context.Context, userID string) (string, error) {
	env, raw, err := m.loadToken(userID)
	if err != nil {
		return "", err
	}
	if env == nil || env.AccessToken == "" {
		return "", ErrNeedsOAuth
	}
	if time.Until(env.Expiry) > refreshEarly {
		return env.AccessToken, nil
	}
	if env.RefreshToken == "" {
		_ = m.deleteToken(userID)
		return "", ErrNeedsOAuth
	}
	return m.refreshAccessToken(ctx, userID, env, raw)
}

func (m *Manager) Disconnect(ctx context.Context, userID string) error {
	return m.deleteToken(userID)
}

func (m *Manager) DisconnectProvider(ctx context.Context) error {
	return m.deleteToken(ProviderCredentialSubject)
}

func (m *Manager) exchangeAuthorizationCode(ctx context.Context, code, verifier string) (*tokenEnvelope, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {deviceRedirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}
	return m.doTokenRequest(ctx, form)
}

func (m *Manager) refreshAccessToken(ctx context.Context, userID string, env *tokenEnvelope, raw []byte) (string, error) {
	leaseID := requestID()
	acquired, err := m.store.KVCompareAndSetWithExpiry(refreshLeaseKey(userID), nil, leaseID, refreshLeaseTTL)
	if err != nil {
		return "", err
	}
	if !acquired {
		deadline := time.Now().Add(refreshLeaseWait)
		for time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
			current, _, loadErr := m.loadToken(userID)
			if loadErr != nil {
				return "", loadErr
			}
			if current != nil && current.AccessToken != "" && current.AccessToken != env.AccessToken && time.Until(current.Expiry) > refreshEarly {
				return current.AccessToken, nil
			}
		}
		return "", fmt.Errorf("token refresh is already in progress")
	}
	defer func() {
		_, _ = m.store.KVCompareAndSet(refreshLeaseKey(userID), leaseID, nil)
	}()

	latest, latestRaw, err := m.loadToken(userID)
	if err != nil {
		return "", err
	}
	if latest == nil {
		return "", ErrNeedsOAuth
	}
	if latest.AccessToken != env.AccessToken && time.Until(latest.Expiry) > refreshEarly {
		return latest.AccessToken, nil
	}
	if latestRaw != nil {
		raw = latestRaw
		env = latest
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {env.RefreshToken},
		"client_id":     {clientID},
	}
	refreshed, err := m.doTokenRequest(ctx, form)
	if err != nil {
		if isReauthError(err) {
			_, _ = m.store.KVCompareAndSet(tokenKey(userID), raw, nil)
			return "", ErrNeedsOAuth
		}
		return "", err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = env.RefreshToken
	}
	newRaw, err := json.Marshal(refreshed)
	if err != nil {
		return "", err
	}
	ok, err := m.store.KVCompareAndSet(tokenKey(userID), raw, newRaw)
	if err != nil {
		return "", err
	}
	if !ok {
		current, _, loadErr := m.loadToken(userID)
		if loadErr != nil {
			return "", loadErr
		}
		if current == nil {
			return "", ErrNeedsOAuth
		}
		return current.AccessToken, nil
	}
	return refreshed.AccessToken, nil
}

func (m *Manager) doTokenRequest(ctx context.Context, form url.Values) (*tokenEnvelope, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := noRedirectClient(m.httpClient).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseTokenEndpointError(resp.StatusCode, body)
	}

	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.AccessToken == "" {
		return nil, errors.New("token response is missing access_token")
	}
	expiry := expiryFromJWT(parsed.AccessToken)
	if expiry.IsZero() && parsed.ExpiresIn > 0 {
		expiry = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	if expiry.IsZero() {
		expiry = time.Now().Add(time.Hour)
	}
	return &tokenEnvelope{
		Version:      tokenVersion,
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		TokenType:    parsed.TokenType,
		Expiry:       expiry,
		UpdatedAt:    time.Now(),
	}, nil
}

func (m *Manager) loadToken(userID string) (*tokenEnvelope, []byte, error) {
	if m.store == nil {
		return nil, nil, fmt.Errorf("%w: token store unavailable", ErrNeedsOAuth)
	}
	var raw []byte
	err := m.store.KVGet(tokenKey(userID), &raw)
	if err != nil {
		if mmapi.IsKVNotFound(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var env tokenEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, raw, err
	}
	if env.Version != tokenVersion || env.AccessToken == "" {
		return nil, raw, nil
	}
	return &env, raw, nil
}

func (m *Manager) storeToken(userID string, env *tokenEnvelope) error {
	if env.RefreshToken == "" {
		return errors.New("token response is missing refresh_token")
	}
	return m.store.KVSet(tokenKey(userID), env)
}

func (m *Manager) deleteToken(userID string) error {
	if m.store == nil {
		return nil
	}
	return m.store.KVDelete(tokenKey(userID))
}

func (m *Manager) loadSession(userID, sessionID string) (*deviceSession, error) {
	var session deviceSession
	err := m.store.KVGet(sessionKey(userID, sessionID), &session)
	if err != nil {
		if mmapi.IsKVNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if session.Version != sessionVersion || session.UserID != userID || session.SessionID != sessionID {
		return nil, nil
	}
	return &session, nil
}

func parseTokenEndpointError(status int, body []byte) error {
	var parsed struct {
		Error any    `json:"error"`
		Desc  string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &parsed)
	code := ""
	message := parsed.Desc
	switch v := parsed.Error.(type) {
	case string:
		code = v
	case map[string]any:
		if raw, ok := v["code"].(string); ok {
			code = raw
		}
		if raw, ok := v["message"].(string); ok {
			message = raw
		}
	}
	return &tokenEndpointError{StatusCode: status, Code: code, Message: message}
}

func isReauthError(err error) bool {
	var tokenErr *tokenEndpointError
	if !errors.As(err, &tokenErr) {
		return false
	}
	if tokenErr.StatusCode == http.StatusUnauthorized || tokenErr.StatusCode == http.StatusForbidden {
		return true
	}
	switch tokenErr.Code {
	case "invalid_grant", "invalid_token", "invalid_request", "refresh_token_reused":
		return true
	default:
		return false
	}
}

func expiryFromJWT(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}

func tokenKey(userID string) string {
	return "openai_codex_token_v1_" + userID
}

func sessionKey(userID, sessionID string) string {
	return "openai_codex_device_session_v1_" + userID + "_" + sessionID
}

func refreshLeaseKey(userID string) string {
	return "openai_codex_refresh_lease_v1_" + userID
}

func noRedirectClient(base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	copy := *base
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &copy
}
