// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import type {ChannelSearchOpts, ChannelWithTeamData} from '@mattermost/types/channels';
import type {OptsSignalExt} from '@mattermost/types/client4';

import type {ConversationResponse, Turn} from '@/types/conversation';

import manifest from './manifest';

import {
    doLoopInAgent,
    createWorkspacePolicy,
    getAdminRuntimeApprovals,
    getAdminRuntimeSessions,
    getHermesOffChecklist,
    getAdminRuntimeTaskRuns,
    getAdminRuntimeTasks,
    getConversation,
    getConversationContext,
    getRuntimeHealth,
    getSupervisorRuns,
    getWorkspacePolicies,
    normalizeConversationResponse,
    searchAllChannels,
    setSiteURL,
    submitAdminRuntimeApproval,
    submitAdminRuntimeTaskAction,
    submitAdminRuntimeSessionAction,
    submitRuntimeSessionAction,
    updateRead,
    updateHermesOffChecklistItem,
    updateWorkspacePolicy,
} from './client';

type SearchAllChannelsOpts = Omit<ChannelSearchOpts, 'page' | 'per_page'> & OptsSignalExt;

jest.mock('@mattermost/client', () => {
    const mockSearchAllChannels = jest.fn<
        Promise<ChannelWithTeamData[]>,
        [string, SearchAllChannelsOpts | undefined]
    >();
    const mockUpdateThreadReadForUser = jest.fn();

    return {

        // client.tsx constructs `new Client4()`; the mocked class exposes instance methods.
        Client4: class Client4 {
            url = '';
            searchAllChannels = mockSearchAllChannels;
            updateThreadReadForUser = mockUpdateThreadReadForUser;

            setUrl(url: string) {
                this.url = url;
            }

            getOptions(options: Record<string, unknown>) {
                return {...options, headers: {'X-Requested-With': 'XMLHttpRequest'}};
            }
        },
        ClientError: class extends Error {},
        mockSearchAllChannels,
        mockUpdateThreadReadForUser,
    };
});

const {mockSearchAllChannels} = jest.requireMock('@mattermost/client') as {
    mockSearchAllChannels: jest.MockedFunction<
        (term: string, opts?: SearchAllChannelsOpts) => Promise<ChannelWithTeamData[]>
    >;
};

const {mockUpdateThreadReadForUser} = jest.requireMock('@mattermost/client') as {
    mockUpdateThreadReadForUser: jest.MockedFunction<
        (userId: string, teamId: string, postId: string, timestamp: number) => Promise<void>
    >;
};

const mockFetch = jest.fn<Promise<Response>, [string, RequestInit]>();
global.fetch = mockFetch as unknown as typeof fetch;

const siteURL = 'http://localhost:8065';

function okResponse(): Response {
    return {ok: true, status: 200, json: () => Promise.resolve({})} as unknown as Response;
}

// Mattermost IDs are 26 characters of lowercase letters and digits.
const WELL_FORMED_ID = 'c7f2m9xq4v1b8n3k6t5w0hzjd2';

// These ids reach the client straight off free-form post props, so a caller can hand them anything.
const NOT_WELL_FORMED_IDS: Array<{name: string; id: string}> = [
    {name: 'empty', id: ''},
    {name: 'relative path segments', id: '../../some/other/route'},
    {name: 'right length but contains a separator', id: 'abcdefghijklmnopqrstuvwxy/'},
    {name: 'well-formed id with leading whitespace', id: ` ${WELL_FORMED_ID}`},
];

function makeTurn(overrides: Partial<Turn> = {}): Turn {
    return {
        id: 't',
        post_id: 'p',
        role: 'assistant',
        content: [],
        tokens_in: 0,
        tokens_out: 0,
        sequence: 1,
        ...overrides,
    };
}

function makeConv(overrides: Partial<ConversationResponse> = {}): ConversationResponse {
    return {
        id: 'c',
        user_id: 'u',
        bot_id: 'b',
        channel_id: null,
        root_post_id: null,
        title: '',
        operation: 'conversation',
        turns: [],
        ...overrides,
    };
}

beforeAll(() => {
    setSiteURL(siteURL);
});

beforeEach(() => {
    mockFetch.mockReset();
    mockFetch.mockResolvedValue(okResponse());
});

describe('normalizeConversationResponse', () => {
    beforeEach(() => {
        mockSearchAllChannels.mockReset();
    });

    test('replaces null turn content with an empty array', () => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const raw = makeConv({turns: [makeTurn({content: null as any})]});
        const normalized = normalizeConversationResponse(raw);
        expect(normalized.turns[0].content).toEqual([]);
    });

    test('preserves populated content blocks', () => {
        const raw = makeConv({
            turns: [makeTurn({content: [{type: 'text', text: 'hi'}]})],
        });
        const normalized = normalizeConversationResponse(raw);
        expect(normalized.turns[0].content).toEqual([{type: 'text', text: 'hi'}]);
    });

    test('handles a missing turns array', () => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any, no-undefined
        const raw = makeConv({turns: undefined as any});
        const normalized = normalizeConversationResponse(raw);
        expect(normalized.turns).toEqual([]);
    });

    test('normalizes every turn independently', () => {
        const raw = makeConv({
            turns: [
                makeTurn({id: 't1', sequence: 1, content: [{type: 'text', text: 'a'}]}),
                // eslint-disable-next-line @typescript-eslint/no-explicit-any
                makeTurn({id: 't2', sequence: 2, content: null as any}),
                makeTurn({id: 't3', sequence: 3, content: []}),
            ],
        });
        const normalized = normalizeConversationResponse(raw);
        expect(normalized.turns[0].content).toHaveLength(1);
        expect(normalized.turns[1].content).toEqual([]);
        expect(normalized.turns[2].content).toEqual([]);
    });
});

describe('searchAllChannels', () => {
    beforeEach(() => {
        mockSearchAllChannels.mockReset();
    });

    test('uses the non-admin search path for channel scoping', async () => {
        const channels = [{id: 'channel-id'} as ChannelWithTeamData];
        mockSearchAllChannels.mockResolvedValue(channels);

        await expect(searchAllChannels('town')).resolves.toEqual(channels);
        expect(mockSearchAllChannels).toHaveBeenCalledWith('town', {
            nonAdminSearch: true,
            public: true,
            private: true,
            include_deleted: false,
            deleted: false,
        });
    });
});

describe('updateRead', () => {
    beforeEach(() => {
        mockUpdateThreadReadForUser.mockReset();
    });

    test('returns the updateThreadReadForUser promise', async () => {
        const readPromise = Promise.resolve();
        mockUpdateThreadReadForUser.mockReturnValue(readPromise);

        const result = updateRead('user-id', 'team-id', 'post-id', 123);

        expect(result).toBe(readPromise);
        await expect(result).resolves.toBeUndefined();
        expect(mockUpdateThreadReadForUser).toHaveBeenCalledWith('user-id', 'team-id', 'post-id', 123);
    });

    test('propagates updateThreadReadForUser rejection', async () => {
        const error = new Error('User thread membership doesn\'t exist');
        mockUpdateThreadReadForUser.mockRejectedValue(error);

        await expect(updateRead('user-id', 'team-id', 'post-id', 123)).rejects.toBe(error);
    });
});

describe('doLoopInAgent', () => {
    test('posts to the loop-in route for a well-formed post id', async () => {
        await expect(doLoopInAgent(WELL_FORMED_ID, 'matty')).resolves.toBeUndefined();

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/post/${WELL_FORMED_ID}/loop_in_agent?botUsername=matty`);
        expect(options).toEqual(expect.objectContaining({method: 'POST'}));
    });

    test('percent-encodes the bot username in the query string', async () => {
        await doLoopInAgent(WELL_FORMED_ID, 'agent bot&x=1');

        const [url] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/post/${WELL_FORMED_ID}/loop_in_agent?botUsername=agent%20bot%26x%3D1`);
    });

    test.each(NOT_WELL_FORMED_IDS)('does not issue a request when the post id is not well-formed: $name', async ({id}) => {
        await expect(doLoopInAgent(id, 'matty')).rejects.toThrow();

        expect(mockFetch).not.toHaveBeenCalled();
    });
});

describe('getConversation', () => {
    test('requests the conversation route for a well-formed id', async () => {
        await expect(getConversation(WELL_FORMED_ID)).resolves.toEqual({turns: []});

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/conversations/${WELL_FORMED_ID}`);
        expect(options).toEqual(expect.objectContaining({method: 'GET'}));
    });

    test.each(NOT_WELL_FORMED_IDS)('does not issue a request when the conversation id is not well-formed: $name', async ({id}) => {
        await expect(getConversation(id)).rejects.toThrow();

        expect(mockFetch).not.toHaveBeenCalled();
    });
});

describe('getConversationContext', () => {
    test('requests the conversation context route for a well-formed id', async () => {
        await expect(getConversationContext(WELL_FORMED_ID)).resolves.toEqual({});

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/conversations/${WELL_FORMED_ID}/context`);
        expect(options).toEqual(expect.objectContaining({method: 'GET'}));
    });

    test.each(NOT_WELL_FORMED_IDS)('does not issue a request when the conversation id is not well-formed: $name', async ({id}) => {
        await expect(getConversationContext(id)).rejects.toThrow();

        expect(mockFetch).not.toHaveBeenCalled();
    });
});

describe('getWorkspacePolicies', () => {
    test('requests admin workspace runtime policies', async () => {
        const policies = [{id: 'workspace-policy-1', name: 'Project workspace'}];
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 200,
            json: () => Promise.resolve(policies),
        } as unknown as Response);

        await expect(getWorkspacePolicies()).resolves.toEqual(policies);

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/workspace-policies`);
        expect(options).toEqual(expect.objectContaining({method: 'GET'}));
    });
});

describe('getRuntimeHealth', () => {
    test('requests admin runtime health', async () => {
        const health = {enabled: true, healthy: true, activeSessions: 1};
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 200,
            json: () => Promise.resolve(health),
        } as unknown as Response);

        await expect(getRuntimeHealth()).resolves.toEqual(health);

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/health`);
        expect(options).toEqual(expect.objectContaining({method: 'GET'}));
    });
});

describe('getHermesOffChecklist', () => {
    test('requests admin Hermes-off checklist', async () => {
        const checklist = {ready: false, generatedAt: 1, groups: []};
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 200,
            json: () => Promise.resolve(checklist),
        } as unknown as Response);

        await expect(getHermesOffChecklist()).resolves.toEqual(checklist);

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/hermes-off-checklist`);
        expect(options).toEqual(expect.objectContaining({method: 'GET'}));
    });
});

describe('updateHermesOffChecklistItem', () => {
    test('updates a manual Hermes-off checklist item', async () => {
        const state = {key: 'test_channel_soak', status: 'ok'};
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 200,
            json: () => Promise.resolve(state),
        } as unknown as Response);

        await expect(updateHermesOffChecklistItem('test_channel_soak', {status: 'ok', detail: '7 days passed'})).resolves.toEqual(state);

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/hermes-off-checklist/test_channel_soak`);
        expect(options).toEqual(expect.objectContaining({
            method: 'PUT',
            body: JSON.stringify({status: 'ok', detail: '7 days passed'}),
        }));
    });
});

describe('getAdminRuntimeTasks', () => {
    test('requests admin runtime tasks with filters', async () => {
        const tasks = [{id: 'task-1', status: 'paused'}];
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 200,
            json: () => Promise.resolve(tasks),
        } as unknown as Response);

        await expect(getAdminRuntimeTasks({
            channelId: 'channel-1',
            rootPostId: 'root-1',
            status: 'paused',
            limit: 25,
        })).resolves.toEqual(tasks);

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/tasks?channel_id=channel-1&root_post_id=root-1&status=paused&limit=25`);
        expect(options).toEqual(expect.objectContaining({method: 'GET'}));
    });
});

describe('getAdminRuntimeTaskRuns', () => {
    test('requests admin runtime task runs', async () => {
        const runs = [{id: 'run-1', taskID: 'task-1', status: 'completed'}];
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 200,
            json: () => Promise.resolve(runs),
        } as unknown as Response);

        await expect(getAdminRuntimeTaskRuns('task-1')).resolves.toEqual(runs);

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/tasks/task-1/runs`);
        expect(options).toEqual(expect.objectContaining({method: 'GET'}));
    });
});

describe('getAdminRuntimeSessions', () => {
    test('requests admin runtime sessions with filters', async () => {
        const sessions = [{id: 'session-1', status: 'failed'}];
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 200,
            json: () => Promise.resolve(sessions),
        } as unknown as Response);

        await expect(getAdminRuntimeSessions({
            channelId: 'channel-1',
            rootPostId: 'root-1',
            userId: 'user-1',
            agentId: 'agent-1',
            status: 'failed',
            limit: 25,
        })).resolves.toEqual(sessions);

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/sessions?channel_id=channel-1&root_post_id=root-1&user_id=user-1&agent_id=agent-1&status=failed&limit=25`);
        expect(options).toEqual(expect.objectContaining({method: 'GET'}));
    });
});

describe('getAdminRuntimeApprovals', () => {
    test('requests admin runtime approvals with filters', async () => {
        const approvals = [{id: 'approval-1', status: 'pending'}];
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 200,
            json: () => Promise.resolve(approvals),
        } as unknown as Response);

        await expect(getAdminRuntimeApprovals({
            runtimeSessionId: 'session-1',
            requestedBy: 'user-1',
            status: 'pending',
            limit: 25,
        })).resolves.toEqual(approvals);

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/approvals?runtime_session_id=session-1&requested_by=user-1&status=pending&limit=25`);
        expect(options).toEqual(expect.objectContaining({method: 'GET'}));
    });
});

describe('workspace policy admin client', () => {
    const request = {
        name: 'Project',
        allowedRoots: ['/workspace/project'],
        defaultWorkspacePath: '/workspace/project',
        mode: 'ask_write',
        networkMode: 'ask',
        shellMode: 'ask',
        metadata: {},
    };

    test('creates a workspace policy', async () => {
        const policy = {id: 'workspace-policy-1', ...request};
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 201,
            json: () => Promise.resolve(policy),
        } as unknown as Response);

        await expect(createWorkspacePolicy(request)).resolves.toEqual(policy);

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/workspace-policies`);
        expect(options).toEqual(expect.objectContaining({
            method: 'POST',
            body: JSON.stringify(request),
        }));
    });

    test('updates a workspace policy', async () => {
        const policy = {id: 'workspace-policy-1', ...request};
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 200,
            json: () => Promise.resolve(policy),
        } as unknown as Response);

        await expect(updateWorkspacePolicy('workspace-policy-1', request)).resolves.toEqual(policy);

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/workspace-policies/workspace-policy-1`);
        expect(options).toEqual(expect.objectContaining({
            method: 'PUT',
            body: JSON.stringify(request),
        }));
    });
});

describe('submitAdminRuntimeTaskAction', () => {
    test('posts an admin runtime task action and returns the updated task', async () => {
        const task = {id: 'task-1', status: 'queued'};
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 200,
            json: () => Promise.resolve(task),
        } as unknown as Response);

        await expect(submitAdminRuntimeTaskAction('task-1', {action: 'snooze', snoozeMs: 300000})).resolves.toEqual(task);

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/tasks/task-1/action`);
        expect(options).toEqual(expect.objectContaining({
            method: 'POST',
            body: JSON.stringify({action: 'snooze', snoozeMs: 300000}),
        }));
    });

    test('returns null when an admin runtime task action deletes the task', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 204,
        } as unknown as Response);

        await expect(submitAdminRuntimeTaskAction('task-1', {action: 'delete'})).resolves.toBeNull();
    });
});

describe('submitAdminRuntimeApproval', () => {
    test('posts an admin runtime approval decision', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 204,
        } as unknown as Response);

        await expect(submitAdminRuntimeApproval('approval-1', 'deny', 'too risky')).resolves.toBeUndefined();

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/approvals/approval-1/decision`);
        expect(options).toEqual(expect.objectContaining({
            method: 'POST',
            body: JSON.stringify({decision: 'deny', reason: 'too risky'}),
        }));
    });
});

describe('submitAdminRuntimeSessionAction', () => {
    test('posts an admin runtime session action', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 204,
        } as unknown as Response);

        await expect(submitAdminRuntimeSessionAction('session-1', {action: 'stop'})).resolves.toBeUndefined();

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/admin/runtime/sessions/session-1/action`);
        expect(options).toEqual(expect.objectContaining({
            method: 'POST',
            body: JSON.stringify({action: 'stop'}),
        }));
    });
});

describe('submitRuntimeSessionAction', () => {
    test('posts a user runtime session action', async () => {
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 204,
        } as unknown as Response);

        await expect(submitRuntimeSessionAction('session-1', {action: 'resume'})).resolves.toBeUndefined();

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/runtime/sessions/session-1/action`);
        expect(options).toEqual(expect.objectContaining({
            method: 'POST',
            body: JSON.stringify({action: 'resume'}),
        }));
    });
});

describe('getSupervisorRuns', () => {
    test('requests scoped supervisor runs', async () => {
        const runs = [{id: 'supervisor-1', subagents: []}];
        mockFetch.mockResolvedValueOnce({
            ok: true,
            status: 200,
            json: () => Promise.resolve(runs),
        } as unknown as Response);

        await expect(getSupervisorRuns({
            conversationId: 'conversation-1',
            rootTaskId: 'root-1',
            limit: 5,
        })).resolves.toEqual(runs);

        expect(mockFetch).toHaveBeenCalledTimes(1);
        const [url, options] = mockFetch.mock.calls[0];
        expect(url).toBe(`${siteURL}/plugins/${manifest.id}/runtime/supervisor-runs?conversation_id=conversation-1&root_task_id=root-1&limit=5`);
        expect(options).toEqual(expect.objectContaining({method: 'GET'}));
    });
});
