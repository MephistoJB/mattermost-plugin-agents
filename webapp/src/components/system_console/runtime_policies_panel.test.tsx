// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import {IntlProvider} from 'react-intl';
import {fireEvent, render, screen, waitFor} from '@testing-library/react';

import RuntimePoliciesPanel from './runtime_policies_panel';

const mockGetRuntimePolicies = jest.fn();
const mockUpsertRuntimePolicy = jest.fn();

jest.mock('@/client', () => ({
    getRuntimePolicies: () => mockGetRuntimePolicies(),
    upsertRuntimePolicy: (...args: unknown[]) => mockUpsertRuntimePolicy(...args),
}));

function renderPanel() {
    return render(
        <IntlProvider locale='en'>
            <RuntimePoliciesPanel/>
        </IntlProvider>,
    );
}

describe('RuntimePoliciesPanel', () => {
    beforeEach(() => {
        mockGetRuntimePolicies.mockReset();
        mockUpsertRuntimePolicy.mockReset();
    });

    test('renders runtime policies with budget controls', async () => {
        mockGetRuntimePolicies.mockResolvedValue([{
            id: 'policy-1',
            scopeType: 'team',
            scopeID: 'team-1',
            runtimeType: 'local',
            providerID: 'ollama',
            model: 'gpt-oss:20b',
            workspacePolicyID: '',
            approvalPolicyID: '',
            allowCloud: false,
            allowLocal: true,
            cloudBudgetCents: 250,
            cloudBudgetWindow: 'monthly',
            createdBy: 'admin-1',
            updatedBy: 'admin-1',
            createdAt: 1,
            updatedAt: 1,
        }]);

        renderPanel();

        await screen.findByText('1 policy');
        expect(screen.getByDisplayValue('team-1')).not.toBeNull();
        expect(screen.getByDisplayValue('gpt-oss:20b')).not.toBeNull();
        expect(screen.getByDisplayValue('ollama')).not.toBeNull();
        expect((screen.getByLabelText('Cloud budget cents') as HTMLInputElement).value).toBe('250');
        expect((screen.getByLabelText('Budget window') as HTMLSelectElement).value).toBe('monthly');
    });

    test('updates an existing runtime policy', async () => {
        mockGetRuntimePolicies.
            mockResolvedValueOnce([{
                id: 'policy-1',
                scopeType: 'server',
                scopeID: 'server-1',
                runtimeType: 'codex',
                providerID: 'codex',
                model: 'gpt-5-codex',
                workspacePolicyID: '',
                approvalPolicyID: '',
                allowCloud: true,
                allowLocal: false,
                cloudBudgetCents: 1000,
                cloudBudgetWindow: '',
                createdBy: 'admin-1',
                updatedBy: 'admin-1',
                createdAt: 1,
                updatedAt: 1,
            }]).
            mockResolvedValueOnce([]);
        mockUpsertRuntimePolicy.mockResolvedValue({id: 'policy-1'});

        renderPanel();

        await screen.findByDisplayValue('server-1');
        fireEvent.change(screen.getByLabelText('Cloud budget cents'), {target: {value: '1250'}});
        fireEvent.change(screen.getByLabelText('Budget window'), {target: {value: 'weekly'}});
        fireEvent.click(screen.getByLabelText('Confirm policy cloud execution'));
        fireEvent.click(screen.getByRole('button', {name: 'Save'}));

        await waitFor(() => expect(mockUpsertRuntimePolicy).toHaveBeenCalledWith('server', 'server-1', {
            runtimeType: 'codex',
            providerID: 'codex',
            model: 'gpt-5-codex',
            allowCloud: true,
            allowLocal: false,
            cloudBudgetCents: 1250,
            cloudBudgetWindow: 'weekly',
            cloudEscalationConfirmed: true,
        }));
        await waitFor(() => expect(mockGetRuntimePolicies).toHaveBeenCalledTimes(2));
    });

    test('requires cloud confirmation before saving cloud runtime policy', async () => {
        mockGetRuntimePolicies.mockResolvedValue([{
            id: 'policy-1',
            scopeType: 'server',
            scopeID: 'server-1',
            runtimeType: 'codex',
            providerID: 'codex',
            model: 'gpt-5-codex',
            workspacePolicyID: '',
            approvalPolicyID: '',
            allowCloud: true,
            allowLocal: false,
            cloudBudgetCents: 1000,
            cloudBudgetWindow: '',
            createdBy: 'admin-1',
            updatedBy: 'admin-1',
            createdAt: 1,
            updatedAt: 1,
        }]);

        renderPanel();

        await screen.findByDisplayValue('server-1');
        fireEvent.click(screen.getByRole('button', {name: 'Save'}));

        await screen.findByText('Confirm cloud execution before saving this policy.');
        expect(mockUpsertRuntimePolicy).not.toHaveBeenCalled();
    });

    test('adds a runtime policy', async () => {
        mockGetRuntimePolicies.
            mockResolvedValueOnce([]).
            mockResolvedValueOnce([{
                id: 'policy-2',
                scopeType: 'user',
                scopeID: 'user-1',
                runtimeType: 'local',
                providerID: 'local',
                model: 'llama',
                workspacePolicyID: '',
                approvalPolicyID: '',
                allowCloud: false,
                allowLocal: true,
                cloudBudgetCents: 0,
                cloudBudgetWindow: 'daily',
                createdBy: 'admin-1',
                updatedBy: 'admin-1',
                createdAt: 1,
                updatedAt: 1,
            }]);
        mockUpsertRuntimePolicy.mockResolvedValue({id: 'policy-2'});

        renderPanel();

        await screen.findByText('No runtime policies found.');
        fireEvent.change(screen.getByLabelText('New policy scope type'), {target: {value: 'user'}});
        fireEvent.change(screen.getByLabelText('New policy scope ID'), {target: {value: 'user-1'}});
        fireEvent.change(screen.getByLabelText('New policy runtime type'), {target: {value: 'local'}});
        fireEvent.change(screen.getByLabelText('New policy provider ID'), {target: {value: 'local'}});
        fireEvent.change(screen.getByLabelText('New policy model'), {target: {value: 'llama'}});
        fireEvent.change(screen.getByLabelText('New policy budget window'), {target: {value: 'daily'}});
        fireEvent.click(screen.getByLabelText('Cloud'));
        fireEvent.click(screen.getByLabelText('Local'));
        fireEvent.click(screen.getByRole('button', {name: 'Add'}));

        await waitFor(() => expect(mockUpsertRuntimePolicy).toHaveBeenCalledWith('user', 'user-1', {
            runtimeType: 'local',
            providerID: 'local',
            model: 'llama',
            allowCloud: false,
            allowLocal: true,
            cloudBudgetCents: 0,
            cloudBudgetWindow: 'daily',
            cloudEscalationConfirmed: false,
        }));
        await screen.findByDisplayValue('user-1');
    });

    test('renders load failure', async () => {
        mockGetRuntimePolicies.mockRejectedValue(new Error('boom'));

        renderPanel();

        await screen.findByText('Failed to load runtime policies.');
    });
});
