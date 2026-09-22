// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import type {RuntimeApproval} from '@/types/runtime';

const MAX_SUMMARY_LENGTH = 180;

export function runtimeApprovalSummary(approval: RuntimeApproval): string {
    const payload = asRecord(approval.requestPayload);
    const params = asRecord(payload.params);
    const command = firstString(params.command, payload.command, params.cmd, payload.cmd);
    if (command) {
        return truncateSummary(`Command: ${command}`);
    }

    const path = firstString(params.path, payload.path, params.filePath, payload.filePath, params.cwd, payload.cwd);
    const action = firstString(params.action, payload.action, params.operation, payload.operation);
    if (path && action) {
        return truncateSummary(`${action}: ${path}`);
    }
    if (path) {
        return truncateSummary(`File: ${path}`);
    }

    const tool = firstString(params.tool, payload.tool, params.toolName, payload.toolName);
    if (tool) {
        return truncateSummary(`Tool: ${tool}`);
    }

    const method = firstString(payload.method, payload.type);
    if (method) {
        return truncateSummary(`Request: ${method}`);
    }

    return approval.externalApprovalID || approval.id || 'Runtime approval request';
}

function asRecord(value: unknown): Record<string, unknown> {
    if (value && typeof value === 'object' && !Array.isArray(value)) {
        return value as Record<string, unknown>;
    }
    return {};
}

function firstString(...values: unknown[]): string {
    for (const value of values) {
        if (typeof value === 'string' && value.trim()) {
            return value.trim();
        }
    }
    return '';
}

function truncateSummary(value: string): string {
    if (value.length <= MAX_SUMMARY_LENGTH) {
        return value;
    }
    return `${value.slice(0, MAX_SUMMARY_LENGTH - 3)}...`;
}
