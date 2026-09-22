// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import {IntlProvider} from 'react-intl';
import {fireEvent, render, screen, waitFor} from '@testing-library/react';

import RuntimeSessionsPanel from './runtime_sessions_panel';

const mockGetAdminRuntimeSessions = jest.fn();
const mockSubmitAdminRuntimeSessionAction = jest.fn();

jest.mock('@/client', () => ({
    getAdminRuntimeSessions: (...args: unknown[]) => mockGetAdminRuntimeSessions(...args),
    submitAdminRuntimeSessionAction: (...args: unknown[]) => mockSubmitAdminRuntimeSessionAction(...args),
}));

function renderPanel() {
    return render(
        <IntlProvider locale='en'>
            <RuntimeSessionsPanel/>
        </IntlProvider>,
    );
}

describe('RuntimeSessionsPanel', () => {
    beforeEach(() => {
        mockGetAdminRuntimeSessions.mockReset();
        mockSubmitAdminRuntimeSessionAction.mockReset();
    });

    test('renders sessions with diagnostics and actions', async () => {
        mockGetAdminRuntimeSessions.mockResolvedValue([{
            id: 'session-1',
            mattermostConversationID: 'root-1',
            serverID: 'server-1',
            teamID: 'team-1',
            channelID: 'channel-123456789',
            rootPostID: 'root-123456789',
            userID: 'user-123456789',
            agentID: 'agent-1',
            runtimeType: 'codex',
            providerID: 'codex',
            model: 'gpt-5-codex',
            workspacePath: '/workspace/project',
            externalSessionID: 'codex-session-1',
            status: 'running',
            lastError: '',
            lastEventAt: 1,
            createdAt: 1,
            updatedAt: 1,
        }]);

        renderPanel();

        await screen.findByText('session-1');
        expect(screen.getByText('1 session')).not.toBeNull();
        expect(screen.getAllByText('running').length).toBeGreaterThan(0);
        expect(screen.getAllByText('codex').length).toBeGreaterThan(0);
        expect(screen.getByText('gpt-5-codex')).not.toBeNull();
        expect(screen.getByText('external codex-session-1')).not.toBeNull();
        expect(screen.getByText('workspace /workspace/project')).not.toBeNull();
        expect(screen.getByRole('button', {name: 'Stop'})).not.toBeNull();
    });

    test('applies session filters', async () => {
        mockGetAdminRuntimeSessions.mockResolvedValue([]);

        renderPanel();

        await screen.findByText('No runtime sessions found.');
        fireEvent.change(screen.getByLabelText('Session status filter'), {target: {value: 'failed'}});
        fireEvent.change(screen.getByLabelText('Channel ID filter'), {target: {value: 'channel-1'}});
        fireEvent.change(screen.getByLabelText('Thread root post ID filter'), {target: {value: 'root-1'}});
        fireEvent.change(screen.getByLabelText('User ID filter'), {target: {value: 'user-1'}});
        fireEvent.change(screen.getByLabelText('Agent ID filter'), {target: {value: 'agent-1'}});
        fireEvent.click(screen.getByRole('button', {name: 'Apply'}));

        await waitFor(() => expect(mockGetAdminRuntimeSessions).toHaveBeenLastCalledWith({
            channelId: 'channel-1',
            rootPostId: 'root-1',
            userId: 'user-1',
            agentId: 'agent-1',
            status: 'failed',
            limit: 50,
        }));
    });

    test('submits session action and reloads sessions', async () => {
        mockGetAdminRuntimeSessions.
            mockResolvedValueOnce([{
                id: 'session-1',
                runtimeType: 'local',
                providerID: 'local',
                model: 'llama',
                status: 'failed',
                lastError: 'runtime failed',
            }]).
            mockResolvedValueOnce([]);
        mockSubmitAdminRuntimeSessionAction.mockImplementation(() => Promise.resolve());

        renderPanel();

        await screen.findByText('session-1');
        expect(screen.getByText('runtime failed')).not.toBeNull();
        fireEvent.click(screen.getByRole('button', {name: 'Resume'}));

        await waitFor(() => expect(mockSubmitAdminRuntimeSessionAction).toHaveBeenCalledWith('session-1', {action: 'resume'}));
        await waitFor(() => expect(mockGetAdminRuntimeSessions).toHaveBeenCalledTimes(2));
    });

    test('renders load failure', async () => {
        mockGetAdminRuntimeSessions.mockRejectedValue(new Error('boom'));

        renderPanel();

        await screen.findByText('Failed to load runtime sessions.');
    });
});
