// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mattermost/mattermost-plugin-agents/v2/openaicodex"
)

type openAICodexPollRequest struct {
	SessionID string `json:"sessionID" binding:"required"`
}

func (a *API) openAICodexManager() *openaicodex.Manager {
	return openaicodex.NewManager(openaicodex.ManagerConfig{
		Store:      a.mmClient,
		HTTPClient: a.llmUpstreamHTTPClient,
	})
}

func (a *API) handleOpenAICodexStatus(c *gin.Context) {
	status, err := a.openAICodexManager().ProviderStatus()
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to load provider status: %w", err))
		return
	}
	c.JSON(http.StatusOK, status)
}

func (a *API) handleOpenAICodexStart(c *gin.Context) {
	if err := a.enforceEmptyBody(c); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	start, err := a.openAICodexManager().StartDeviceFlow(c.Request.Context(), c.GetHeader("Mattermost-User-Id"))
	if err != nil {
		c.AbortWithError(http.StatusBadGateway, fmt.Errorf("failed to start provider login: %w", err))
		return
	}
	c.JSON(http.StatusOK, start)
}

func (a *API) handleOpenAICodexPoll(c *gin.Context) {
	var req openAICodexPollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}

	status, err := a.openAICodexManager().PollDeviceFlow(c.Request.Context(), c.GetHeader("Mattermost-User-Id"), req.SessionID)
	if err != nil {
		if errors.Is(err, openaicodex.ErrNeedsOAuth) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "reauthentication required"})
			return
		}
		c.AbortWithError(http.StatusBadGateway, fmt.Errorf("failed to complete provider login: %w", err))
		return
	}
	c.JSON(http.StatusOK, status)
}

func (a *API) handleOpenAICodexDisconnect(c *gin.Context) {
	if err := a.enforceEmptyBody(c); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	if err := a.openAICodexManager().DisconnectProvider(c.Request.Context()); err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to disconnect provider login: %w", err))
		return
	}
	c.Status(http.StatusNoContent)
}
