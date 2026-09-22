// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import {act, fireEvent, render, screen, waitFor} from '@testing-library/react';

import Rhs from './rhs';

const mockGetAIThreads = jest.fn();
const mockGetUserMCPTools = jest.fn();
const mockGetUserToolPreferences = jest.fn();
const mockUpdateRead = jest.fn();
const mockGetScopedRuntimePolicy = jest.fn();
const mockGetRuntimeSessions = jest.fn();
const mockGetRuntimeTasks = jest.fn();
const mockGetRuntimeApprovals = jest.fn();
const mockGetWorkspacePolicies = jest.fn();
const mockGetSupervisorRuns = jest.fn();
const mockSubmitRuntimeApproval = jest.fn();
const mockSubmitRuntimeSessionAction = jest.fn();
const mockUpsertScopedRuntimePolicy = jest.fn();

jest.mock('@/client', () => ({
    getAIThreads: () => mockGetAIThreads(),
    getRuntimeApprovals: () => mockGetRuntimeApprovals(),
    getRuntimeSessions: (...args: unknown[]) => mockGetRuntimeSessions(...args),
    getRuntimeTasks: (...args: unknown[]) => mockGetRuntimeTasks(...args),
    getScopedRuntimePolicy: (...args: unknown[]) => mockGetScopedRuntimePolicy(...args),
    getSupervisorRuns: (...args: unknown[]) => mockGetSupervisorRuns(...args),
    getUserMCPTools: () => mockGetUserMCPTools(),
    getUserToolPreferences: () => mockGetUserToolPreferences(),
    getWorkspacePolicies: () => mockGetWorkspacePolicies(),
    submitRuntimeApproval: (...args: unknown[]) => mockSubmitRuntimeApproval(...args),
    submitRuntimeSessionAction: (...args: unknown[]) => mockSubmitRuntimeSessionAction(...args),
    updateRead: (userId: string, teamId: string, postId: string, timestamp: number) => (
        mockUpdateRead(userId, teamId, postId, timestamp)
    ),
    upsertScopedRuntimePolicy: (...args: unknown[]) => mockUpsertScopedRuntimePolicy(...args),
}));

type SelectorFn = (state: unknown) => unknown;
const mockUseSelector = jest.fn<unknown, [SelectorFn]>();
const mockDispatch = jest.fn();

jest.mock('react-redux', () => ({
    useSelector: (selector: SelectorFn) => mockUseSelector(selector),
    useDispatch: () => mockDispatch,
}));

const mockUseBotlist = jest.fn();

jest.mock('@/bots', () => ({
    useBotlist: () => mockUseBotlist(),
}));

jest.mock('react-intl', () => {
    const ReactActual = jest.requireActual<typeof import('react')>('react');

    return {
        IntlProvider: ({children}: {children: React.ReactNode}) => ReactActual.createElement(ReactActual.Fragment, null, children),
        FormattedMessage: ({defaultMessage}: {defaultMessage: string}) => ReactActual.createElement(ReactActual.Fragment, null, defaultMessage),
        useIntl: () => ({
            formatMessage: ({defaultMessage}: {defaultMessage: string}) => defaultMessage,
        }),
    };
});

const mockThreadViewer = jest.fn();

jest.mock('@/mm_webapp', () => {
    const ReactActual = jest.requireActual<typeof import('react')>('react');

    return {
        ThreadViewer: (props: Record<string, unknown>) => {
            mockThreadViewer(props);
            return ReactActual.createElement('div', {'data-testid': 'rhs-thread-viewer'});
        },
    };
});

jest.mock('./rhs_header', () => {
    const ReactActual = jest.requireActual<typeof import('react')>('react');

    return {
        __esModule: true,
        default: () => ReactActual.createElement('div', {'data-testid': 'rhs-header'}),
    };
});

jest.mock('./rhs_new_tab', () => {
    const ReactActual = jest.requireActual<typeof import('react')>('react');

    return {
        __esModule: true,
        default: () => ReactActual.createElement('div', {'data-testid': 'rhs-new-tab'}),
    };
});

jest.mock('./thread_item', () => {
    const ReactActual = jest.requireActual<typeof import('react')>('react');

    return {
        __esModule: true,
        default: () => ReactActual.createElement('div', {'data-testid': 'rhs-thread-item'}),
    };
});

const activeBot = {
    id: 'bot-id',
    displayName: 'Agents',
    username: 'ai',
    lastIconUpdate: 0,
    dmChannelID: 'dm-channel-id',
    channelAccessLevel: 'all',
    channelIDs: [],
    userAccessLevel: 'all',
    userIDs: [],
    teamIDs: [],
    enabledMCPTools: [],
    autoEnableNewMCPTools: false,
};

const baseState = {
    'plugins-mattermost-ai': {
        selectedPostId: 'post-id',
    },
    entities: {
        users: {
            currentUserId: 'user-id',
        },
        teams: {
            currentTeamId: 'team-id',
        },
        channels: {
            currentChannelId: 'channel-id',
        },
        posts: {
            posts: {
                'post-id': {
                    id: 'post-id',
                    props: {
                        conversation_id: 'conversation-id',
                    },
                },
            },
            postsInThread: {
                'post-id': [],
            },
        },
    },
};

function renderRHS() {
    return render(
        <Rhs/>,
    );
}

describe('RHS', () => {
    beforeEach(() => {
        jest.clearAllMocks();
        mockGetUserMCPTools.mockResolvedValue({servers: []});
        mockGetUserToolPreferences.mockResolvedValue({disabled_servers: []});
        mockGetScopedRuntimePolicy.mockResolvedValue({
            id: 'policy-id',
            runtimeType: 'codex',
            providerID: 'codex',
            model: 'gpt-5-codex',
            workspacePolicyID: '',
            cloudBudgetCents: 250,
            cloudBudgetWindow: 'monthly',
        });
        mockGetRuntimeSessions.mockResolvedValue([]);
        mockGetRuntimeTasks.mockResolvedValue([]);
        mockGetRuntimeApprovals.mockResolvedValue([]);
        mockGetWorkspacePolicies.mockResolvedValue([]);
        mockGetSupervisorRuns.mockResolvedValue([]);
        mockSubmitRuntimeApproval.mockImplementation(() => Promise.resolve());
        mockSubmitRuntimeSessionAction.mockImplementation(() => Promise.resolve());
        mockUpsertScopedRuntimePolicy.mockResolvedValue({
            id: 'policy-id',
            runtimeType: 'local',
            providerID: 'local',
            model: 'gpt-5-codex',
            workspacePolicyID: '',
            cloudBudgetCents: 250,
            cloudBudgetWindow: 'monthly',
        });
        mockUpdateRead.mockImplementation(() => Promise.resolve());
        mockUseBotlist.mockReturnValue({
            bots: [activeBot],
            activeBot,
            setActiveBot: jest.fn(),
        });
        mockUseSelector.mockImplementation((selector) => selector(baseState));
    });

    test('renders the thread viewer when the read marker rejects for missing thread membership', async () => {
        const error = new Error('User thread membership doesn\'t exist');
        const warn = jest.spyOn(console, 'warn').mockImplementation(() => null);
        mockUpdateRead.mockRejectedValue(error);

        const {unmount} = renderRHS();

        expect(screen.getByTestId('rhs-thread-viewer')).toBeTruthy();
        await waitFor(() => {
            expect(mockUpdateRead).toHaveBeenCalledWith('user-id', 'team-id', 'post-id', expect.any(Number));
        });
        await waitFor(() => {
            expect(warn).toHaveBeenCalledWith(
                'Skipping AI thread read marker because thread membership is missing.',
                error,
            );
        });
        expect(mockThreadViewer).toHaveBeenCalledWith(expect.objectContaining({rootPostId: 'post-id'}));

        await act(async () => {
            unmount();
        });
        await act(async () => {
            await Promise.resolve();
        });
        warn.mockRestore();
    });

    test('logs generic read marker rejections', async () => {
        const error = new Error('Unable to update read marker');
        const errorLog = jest.spyOn(console, 'error').mockImplementation(() => null);
        mockUpdateRead.mockRejectedValue(error);

        const {unmount} = renderRHS();

        expect(screen.getByTestId('rhs-thread-viewer')).toBeTruthy();
        await waitFor(() => {
            expect(mockUpdateRead).toHaveBeenCalledWith('user-id', 'team-id', 'post-id', expect.any(Number));
        });
        await waitFor(() => {
            expect(errorLog).toHaveBeenCalledWith(
                'Failed to update AI thread read marker:',
                error,
            );
        });
        expect(mockThreadViewer).toHaveBeenCalledWith(expect.objectContaining({rootPostId: 'post-id'}));

        await act(async () => {
            unmount();
        });
        await act(async () => {
            await Promise.resolve();
        });
        errorLog.mockRestore();
    });

    test('renders runtime controls and updates scoped runtime policy', async () => {
        mockGetRuntimeSessions.mockResolvedValue([{
            id: 'session-1',
            runtimeType: 'codex',
            status: 'running',
            model: 'gpt-5-codex',
            providerID: 'codex',
            externalSessionID: 'codex-session-1',
            workspacePath: '/workspace/project',
        }]);
        mockGetRuntimeTasks.mockResolvedValue([{
            id: 'task-1',
            taskType: 'reminder',
            status: 'queued',
            title: 'Follow up',
            prompt: 'Follow up',
        }]);
        mockGetRuntimeApprovals.mockResolvedValue([{
            id: 'approval-1',
            externalApprovalID: 'provider-approval-1',
            runtimeSessionID: 'session-1',
            requestPayload: {
                method: 'item/commandExecution/requestApproval',
                params: {
                    command: 'make test',
                },
            },
        }]);
        mockGetSupervisorRuns.mockResolvedValue([{
            id: 'supervisor-1',
            status: 'running',
            objective: 'Replace Hermes',
            subagents: [{
                id: 'subagent-1',
                role: 'reviewer',
                title: 'Review plan',
                status: 'running',
            }],
        }]);

        renderRHS();

        await waitFor(() => {
            expect(mockGetScopedRuntimePolicy).toHaveBeenCalledWith('thread', 'post-id');
        });
        expect(await screen.findByText('codex / running')).toBeTruthy();
        expect(screen.getByText('gpt-5-codex / external codex-session-1 / workspace /workspace/project')).toBeTruthy();
        expect(screen.getByText('running / 1 subagents')).toBeTruthy();
        expect(screen.getByText('reviewer: running')).toBeTruthy();
        expect(screen.getByText('reminder / queued')).toBeTruthy();
        expect(screen.getByText('Command: make test')).toBeTruthy();
        expect(screen.getByText('approval-1')).toBeTruthy();
        expect((screen.getByLabelText('Cloud budget cents') as HTMLInputElement).value).toBe('250');

        fireEvent.click(screen.getByLabelText('Stop session session-1'));
        await waitFor(() => {
            expect(mockSubmitRuntimeSessionAction).toHaveBeenCalledWith('session-1', {action: 'stop'});
        });

        fireEvent.click(screen.getByText('Local'));
        await waitFor(() => {
            expect(mockUpsertScopedRuntimePolicy).toHaveBeenCalledWith('thread', 'post-id', expect.objectContaining({
                runtimeType: 'local',
                providerID: 'local',
                allowCloud: false,
                allowLocal: true,
                cloudBudgetCents: 250,
                cloudBudgetWindow: 'monthly',
            }));
        });

        fireEvent.click(screen.getByText('Accept'));
        await waitFor(() => {
            expect(mockSubmitRuntimeApproval).toHaveBeenCalledWith('approval-1', 'accept');
        });
    });

    test('updates scoped workspace policy when workspace policies are available', async () => {
        mockGetScopedRuntimePolicy.mockResolvedValue({
            id: 'policy-id',
            runtimeType: 'codex',
            providerID: 'codex',
            model: 'gpt-5-codex',
            workspacePolicyID: '',
            approvalPolicyID: '',
            allowCloud: true,
            allowLocal: true,
            cloudBudgetCents: 125,
            cloudBudgetWindow: 'weekly',
        });
        mockGetWorkspacePolicies.mockResolvedValue([{
            id: 'workspace-policy-1',
            name: 'Project workspace',
        }]);
        mockUpsertScopedRuntimePolicy.mockResolvedValue({
            id: 'policy-id',
            runtimeType: 'codex',
            providerID: 'codex',
            model: 'gpt-5-codex',
            workspacePolicyID: 'workspace-policy-1',
            cloudBudgetCents: 125,
            cloudBudgetWindow: 'weekly',
        });

        renderRHS();

        const select = await screen.findByLabelText('Workspace policy');
        fireEvent.change(select, {target: {value: 'workspace-policy-1'}});

        await waitFor(() => {
            expect(mockUpsertScopedRuntimePolicy).toHaveBeenCalledWith('thread', 'post-id', expect.objectContaining({
                runtimeType: 'codex',
                providerID: 'codex',
                model: 'gpt-5-codex',
                workspacePolicyID: 'workspace-policy-1',
                cloudBudgetCents: 125,
                cloudBudgetWindow: 'weekly',
            }));
        });
    });

    test('updates scoped cloud budget', async () => {
        mockGetScopedRuntimePolicy.mockResolvedValue({
            id: 'policy-id',
            runtimeType: 'codex',
            providerID: 'codex',
            model: 'gpt-5-codex',
            workspacePolicyID: '',
            approvalPolicyID: '',
            allowCloud: true,
            allowLocal: true,
            cloudBudgetCents: 125,
            cloudBudgetWindow: 'daily',
        });
        mockUpsertScopedRuntimePolicy.mockResolvedValue({
            id: 'policy-id',
            runtimeType: 'codex',
            providerID: 'codex',
            model: 'gpt-5-codex',
            workspacePolicyID: '',
            approvalPolicyID: '',
            allowCloud: true,
            allowLocal: true,
            cloudBudgetCents: 500,
            cloudBudgetWindow: 'daily',
        });

        renderRHS();

        const budget = await screen.findByLabelText('Cloud budget cents');
        fireEvent.change(budget, {target: {value: '500'}});

        await waitFor(() => {
            expect(mockUpsertScopedRuntimePolicy).toHaveBeenCalledWith('thread', 'post-id', expect.objectContaining({
                runtimeType: 'codex',
                providerID: 'codex',
                cloudBudgetCents: 500,
                cloudBudgetWindow: 'daily',
            }));
        });
    });

    test('updates scoped cloud budget window', async () => {
        mockGetScopedRuntimePolicy.mockResolvedValue({
            id: 'policy-id',
            runtimeType: 'codex',
            providerID: 'codex',
            model: 'gpt-5-codex',
            workspacePolicyID: '',
            approvalPolicyID: '',
            allowCloud: true,
            allowLocal: true,
            cloudBudgetCents: 125,
            cloudBudgetWindow: '',
        });
        mockUpsertScopedRuntimePolicy.mockResolvedValue({
            id: 'policy-id',
            runtimeType: 'codex',
            providerID: 'codex',
            model: 'gpt-5-codex',
            workspacePolicyID: '',
            approvalPolicyID: '',
            allowCloud: true,
            allowLocal: true,
            cloudBudgetCents: 125,
            cloudBudgetWindow: 'monthly',
        });

        renderRHS();

        const windowSelect = await screen.findByLabelText('Cloud budget window');
        fireEvent.change(windowSelect, {target: {value: 'monthly'}});

        await waitFor(() => {
            expect(mockUpsertScopedRuntimePolicy).toHaveBeenCalledWith('thread', 'post-id', expect.objectContaining({
                runtimeType: 'codex',
                providerID: 'codex',
                cloudBudgetCents: 125,
                cloudBudgetWindow: 'monthly',
            }));
        });
    });
});
