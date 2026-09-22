// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import {IntlProvider} from 'react-intl';
import {fireEvent, render, screen, waitFor} from '@testing-library/react';

import RuntimeTasksPanel from './runtime_tasks_panel';

const mockGetAdminRuntimeTasks = jest.fn();
const mockGetAdminRuntimeTaskRuns = jest.fn();
const mockSubmitAdminRuntimeTaskAction = jest.fn();

jest.mock('@/client', () => ({
    getAdminRuntimeTaskRuns: (...args: unknown[]) => mockGetAdminRuntimeTaskRuns(...args),
    getAdminRuntimeTasks: (...args: unknown[]) => mockGetAdminRuntimeTasks(...args),
    submitAdminRuntimeTaskAction: (...args: unknown[]) => mockSubmitAdminRuntimeTaskAction(...args),
}));

function renderPanel() {
    return render(
        <IntlProvider locale='en'>
            <RuntimeTasksPanel/>
        </IntlProvider>,
    );
}

describe('RuntimeTasksPanel', () => {
    beforeEach(() => {
        mockGetAdminRuntimeTasks.mockReset();
        mockGetAdminRuntimeTaskRuns.mockReset();
        mockSubmitAdminRuntimeTaskAction.mockReset();
    });

    test('renders runtime tasks and controls', async () => {
        mockGetAdminRuntimeTasks.mockResolvedValue([{
            id: 'task-1',
            title: 'Weekly check',
            prompt: 'Check backups',
            taskType: 'recurring',
            status: 'queued',
            channelID: 'channel-123456789',
            rootPostID: '',
            userID: 'user-1',
            agentID: 'agent-1',
            workspacePath: '',
            scheduleSpec: '',
            nextRunAt: 1893456000000,
            lastRunAt: 0,
            createdBy: 'user-1',
            createdAt: 1,
            updatedAt: 1,
            lastRun: {
                id: 'run-1',
                taskID: 'task-1',
                runtimeSessionID: 'session-1',
                status: 'failed',
                startedAt: 2,
                finishedAt: 3,
                resultPostID: 'post-123456789',
                error: 'runtime failed',
                usage: {},
            },
        }]);

        renderPanel();

        await screen.findByText('Weekly check');
        expect(screen.getByText('1 task')).not.toBeNull();
        expect(screen.getAllByText('queued').length).toBeGreaterThan(0);
        expect(screen.getByText('recurring')).not.toBeNull();
        expect(screen.getByText('Last run: failed')).not.toBeNull();
        expect(screen.getByText('runtime failed')).not.toBeNull();
        expect(screen.getByRole('button', {name: 'Runs'})).not.toBeNull();
        expect(screen.getByRole('button', {name: 'Run'})).not.toBeNull();
        expect(screen.getByRole('button', {name: 'Pause'})).not.toBeNull();
        expect(screen.getByRole('button', {name: 'Snooze'})).not.toBeNull();
        expect(screen.getByRole('button', {name: 'Stop'})).not.toBeNull();
    });

    test('applies task filters', async () => {
        mockGetAdminRuntimeTasks.mockResolvedValue([]);

        renderPanel();

        await screen.findByText('No runtime tasks found.');
        fireEvent.change(screen.getByLabelText('Task status filter'), {target: {value: 'paused'}});
        fireEvent.change(screen.getByLabelText('Channel ID filter'), {target: {value: 'channel-1'}});
        fireEvent.change(screen.getByLabelText('Thread root post ID filter'), {target: {value: 'root-1'}});
        fireEvent.click(screen.getByRole('button', {name: 'Apply'}));

        await waitFor(() => expect(mockGetAdminRuntimeTasks).toHaveBeenLastCalledWith({
            channelId: 'channel-1',
            rootPostId: 'root-1',
            status: 'paused',
            limit: 50,
        }));
    });

    test('loads task run history', async () => {
        mockGetAdminRuntimeTasks.mockResolvedValue([{
            id: 'task-1',
            title: 'Weekly check',
            prompt: '',
            taskType: 'recurring',
            status: 'queued',
            channelID: '',
            rootPostID: '',
            userID: '',
            agentID: '',
            workspacePath: '',
            scheduleSpec: '',
            nextRunAt: 0,
            lastRunAt: 0,
            createdBy: '',
            createdAt: 1,
            updatedAt: 1,
        }]);
        mockGetAdminRuntimeTaskRuns.mockResolvedValue([{
            id: 'run-1',
            taskID: 'task-1',
            runtimeSessionID: 'session-123456789',
            status: 'failed',
            startedAt: 1893456000000,
            finishedAt: 1893456001000,
            resultPostID: 'post-123456789',
            error: 'runtime failed',
            usage: {},
        }]);

        renderPanel();

        await screen.findByText('Weekly check');
        fireEvent.click(screen.getByRole('button', {name: 'Runs'}));

        await waitFor(() => expect(mockGetAdminRuntimeTaskRuns).toHaveBeenCalledWith('task-1'));
        await screen.findByText('session-');
        expect(screen.getByText('runtime failed')).not.toBeNull();
    });

    test('submits task action and reloads tasks', async () => {
        mockGetAdminRuntimeTasks.
            mockResolvedValueOnce([{
                id: 'task-1',
                title: 'Paused check',
                prompt: '',
                taskType: 'watcher',
                status: 'paused',
                channelID: '',
                rootPostID: '',
                userID: '',
                agentID: '',
                workspacePath: '',
                scheduleSpec: '',
                nextRunAt: 0,
                lastRunAt: 0,
                createdBy: '',
                createdAt: 1,
                updatedAt: 1,
            }]).
            mockResolvedValueOnce([]);
        mockSubmitAdminRuntimeTaskAction.mockResolvedValue({id: 'task-1', status: 'queued'});

        renderPanel();

        await screen.findByText('Paused check');
        fireEvent.click(screen.getByRole('button', {name: 'Resume'}));

        await waitFor(() => expect(mockSubmitAdminRuntimeTaskAction).toHaveBeenCalledWith('task-1', {action: 'resume'}));
        await waitFor(() => expect(mockGetAdminRuntimeTasks).toHaveBeenCalledTimes(2));
        await screen.findByText('No runtime tasks found.');
    });

    test('snoozes for one hour', async () => {
        mockGetAdminRuntimeTasks.mockResolvedValue([{
            id: 'task-1',
            title: 'Reminder',
            prompt: '',
            taskType: 'reminder',
            status: 'queued',
            channelID: '',
            rootPostID: '',
            userID: '',
            agentID: '',
            workspacePath: '',
            scheduleSpec: '',
            nextRunAt: 0,
            lastRunAt: 0,
            createdBy: '',
            createdAt: 1,
            updatedAt: 1,
        }]);
        mockSubmitAdminRuntimeTaskAction.mockResolvedValue({id: 'task-1', status: 'queued'});

        renderPanel();

        await screen.findByText('Reminder');
        fireEvent.click(screen.getByRole('button', {name: 'Snooze'}));

        await waitFor(() => expect(mockSubmitAdminRuntimeTaskAction).toHaveBeenCalledWith('task-1', {action: 'snooze', snoozeMs: 3600000}));
    });

    test('renders load failure', async () => {
        mockGetAdminRuntimeTasks.mockRejectedValue(new Error('boom'));

        renderPanel();

        await screen.findByText('Failed to load runtime tasks.');
    });
});
