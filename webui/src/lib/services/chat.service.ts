/**
 * GhostClaw Chat Service
 *
 * A WebSocket-based chat service for the GhostClaw backend.
 * Maintains the same interface as the original llama.cpp ChatService
 * for compatibility with the existing chat store.
 */

import { isAbortError } from '$lib/utils';
import {
        AGENTIC_REGEX,
        ATTACHMENT_LABEL_PDF_FILE,
        ATTACHMENT_LABEL_MCP_PROMPT,
        ATTACHMENT_LABEL_MCP_RESOURCE
} from '$lib/constants';
import {
        AttachmentType,
        ContentPartType,
        MessageRole,
        ReasoningFormat
} from '$lib/enums';
import type { ApiChatMessageContentPart, ApiChatCompletionToolCall } from '$lib/types/api';
import type { DatabaseMessageExtraMcpPrompt, DatabaseMessageExtraMcpResource, DatabaseMessageExtraTextFile, DatabaseMessageExtraPdfFile, DatabaseMessageExtraImageFile } from '$lib/types';
import { modelsStore } from '$lib/stores/models.svelte';

// WebSocket connection manager
class WebSocketManager {
        private ws: WebSocket | null = null;
        private status: 'connecting' | 'connected' | 'disconnected' | 'error' = 'disconnected';
        private sessionId: string = '';
        private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
        private messageCallbacks: Map<string, {
                onChunk?: (chunk: string) => void;
                onReasoningChunk?: (chunk: string) => void;
                onToolCallChunk?: (chunk: string) => void;
                onComplete?: (content: string, reasoning?: string, timings?: unknown, toolCalls?: string) => void;
                onError?: (error: Error) => void;
                onTaskRunning?: (running: boolean) => void;
        }> = new Map();
        private accumulatedContent: string = '';
        private accumulatedReasoning: string = '';

        connect(): Promise<void> {
                return new Promise((resolve, reject) => {
                        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                                resolve();
                                return;
                        }

                        this.status = 'connecting';
                        const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
                        const wsUrl = this.sessionId
                                ? `${wsProtocol}//${window.location.host}/ws?session=${this.sessionId}`
                                : `${wsProtocol}//${window.location.host}/ws`;

                        this.ws = new WebSocket(wsUrl);

                        this.ws.onopen = () => {
                                this.status = 'connected';
                                resolve();
                        };

                        this.ws.onmessage = (event) => {
                                try {
                                        const chunk = JSON.parse(event.data);
                                        this.handleChunk(chunk);
                                } catch (error) {
                                        console.error('Failed to parse WebSocket message:', error);
                                }
                        };

                        this.ws.onerror = (error) => {
                                console.error('WebSocket error:', error);
                                this.status = 'error';
                                reject(new Error('WebSocket connection error'));
                        };

                        this.ws.onclose = () => {
                                this.status = 'disconnected';
                                this.scheduleReconnect();
                        };
                });
        }

        private handleChunk(chunk: {
                session_id?: string;
                content?: string;
                reasoning_content?: string;
                tool_calls?: Array<{
                        id?: string;
                        type?: string;
                        function?: { name?: string; arguments?: string };
                }>;
                done?: boolean;
                error?: string;
                task_running?: boolean;
        }): void {
                if (chunk.session_id) {
                        this.sessionId = chunk.session_id;
                        localStorage.setItem('ghostclaw_session_id', chunk.session_id);
                }

                if (chunk.error) {
                        // 過濾用戶主動取消產生的 context canceled 錯誤
                        // 前端通過 AbortSignal 已處理取消，不需要再觸發 onError 彈窗
                        const isContextCancel = chunk.error === 'context canceled';
                        if (!isContextCancel) {
                                this.messageCallbacks.forEach(cb => cb.onError?.(new Error(chunk.error!)));
                        } else {
                                console.log('[WS] Context canceled by user, skipping error callback');
                        }
                        this.messageCallbacks.clear();
                        return;
                }

                // Handle task_running status (wake notifications from async tasks)
                if (chunk.task_running !== undefined) {
                        this.messageCallbacks.forEach(cb => cb.onTaskRunning?.(chunk.task_running!));
                }

                if (chunk.content) {
                        this.accumulatedContent += chunk.content;
                        this.messageCallbacks.forEach(cb => cb.onChunk?.(chunk.content!));
                }

                if (chunk.reasoning_content) {
                        this.accumulatedReasoning += chunk.reasoning_content;
                        this.messageCallbacks.forEach(cb => cb.onReasoningChunk?.(chunk.reasoning_content!));
                }

                if (chunk.tool_calls && chunk.tool_calls.length > 0) {
                        this.messageCallbacks.forEach(cb => cb.onToolCallChunk?.(JSON.stringify(chunk.tool_calls)));
                }

                if (chunk.done) {
                        this.messageCallbacks.forEach(cb => cb.onComplete?.(
                                this.accumulatedContent,
                                this.accumulatedReasoning || undefined,
                                undefined,
                                undefined
                        ));
                        this.accumulatedContent = '';
                        this.accumulatedReasoning = '';
                }
        }

        private scheduleReconnect(): void {
                if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
                this.reconnectTimer = setTimeout(() => {
                        if (this.status === 'disconnected' || this.status === 'error') {
                                this.connect().catch(console.error);
                        }
                }, 3000);
        }

        async sendMessage(
                content: string,
                callbacks: {
                        onChunk?: (chunk: string) => void;
                        onReasoningChunk?: (chunk: string) => void;
                        onToolCallChunk?: (chunk: string) => void;
                        onComplete?: (content: string, reasoning?: string, timings?: unknown, toolCalls?: string) => void;
                        onError?: (error: Error) => void;
                        onTaskRunning?: (running: boolean) => void;
                }
        ): Promise<void> {
                // Clear old callbacks before registering new ones.
                // Callbacks are NOT cleared on done=true — they survive for
                // async wake notifications until the next explicit sendMessage.
                this.messageCallbacks.clear();
                const callbackId = Date.now().toString();
                this.messageCallbacks.set(callbackId, callbacks);
                this.accumulatedContent = '';
                this.accumulatedReasoning = '';

                await this.connect();

                if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                        this.ws.send(JSON.stringify({ content }));
                } else {
                        callbacks.onError?.(new Error('WebSocket not connected'));
                        this.messageCallbacks.delete(callbackId);
                }
        }

        send(message: string): void {
                if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                        this.ws.send(JSON.stringify({ content: message }));
                }
        }

        disconnect(): void {
                if (this.reconnectTimer) {
                        clearTimeout(this.reconnectTimer);
                        this.reconnectTimer = null;
                }
                if (this.ws) {
                        this.ws.close();
                        this.ws = null;
                }
                this.status = 'disconnected';
        }

        getStatus(): string {
                return this.status;
        }

        getSessionId(): string {
                return this.sessionId;
        }

        setSessionId(id: string): void {
                this.sessionId = id;
        }
}

const wsManager = new WebSocketManager();

// Restore session ID from localStorage
if (typeof window !== 'undefined') {
        const savedSessionId = localStorage.getItem('ghostclaw_session_id');
        if (savedSessionId) {
                wsManager.setSessionId(savedSessionId);
        }
}

export class ChatService {
        private static stripReasoningContent(
                content: ApiChatMessageData['content'] | null | undefined
        ): ApiChatMessageData['content'] | null | undefined {
                if (!content) return content;

                if (typeof content === 'string') {
                        return content
                                .replace(AGENTIC_REGEX.REASONING_BLOCK, '')
                                .replace(AGENTIC_REGEX.REASONING_OPEN, '')
                                .replace(AGENTIC_REGEX.AGENTIC_TOOL_CALL_BLOCK, '')
                                .replace(AGENTIC_REGEX.AGENTIC_TOOL_CALL_OPEN, '');
                }

                if (!Array.isArray(content)) return content;

                return content.map((part: ApiChatMessageContentPart) => {
                        if (part.type !== ContentPartType.TEXT || !part.text) return part;
                        return {
                                ...part,
                                text: part.text
                                        .replace(AGENTIC_REGEX.REASONING_BLOCK, '')
                                        .replace(AGENTIC_REGEX.REASONING_OPEN, '')
                                        .replace(AGENTIC_REGEX.AGENTIC_TOOL_CALL_BLOCK, '')
                                        .replace(AGENTIC_REGEX.AGENTIC_TOOL_CALL_OPEN, '')
                        };
                });
        }

        static async sendMessage(
                messages: ApiChatMessageData[] | (DatabaseMessage & { extra?: DatabaseMessageExtra[] })[],
                options: SettingsChatServiceOptions = {},
                conversationId?: string,
                signal?: AbortSignal
        ): Promise<string | void> {
                const {
                        stream,
                        onChunk,
                        onComplete,
                        onError,
                        onReasoningChunk,
                        onToolCallChunk,
                        onModel,
                        onTaskRunning,
                } = options;

                // Get the last user message
                const lastMessage = messages[messages.length - 1];
                if (!lastMessage || lastMessage.role !== MessageRole.USER) {
                        onError?.(new Error('No user message to send'));
                        return;
                }

                // Extract content from the message
                let content: string;
                if (typeof lastMessage.content === 'string') {
                        content = lastMessage.content;
                } else if (Array.isArray(lastMessage.content)) {
                        const textParts = lastMessage.content.filter(part => part.type === 'text');
                        content = textParts.map(part => (part as { text: string }).text).join('\n');
                } else {
                        content = '';
                }

                // Handle attachments (images, files, etc.)
                // For GhostClaw backend, we upload files first and then reference them by path
                if ('extra' in lastMessage && lastMessage.extra && lastMessage.extra.length > 0) {
                        for (const extra of lastMessage.extra) {
                                if (extra.type === AttachmentType.IMAGE) {
                                        // For images, include base64 data if available
                                        const imgExtra = extra as DatabaseMessageExtraImageFile;
                                        if (imgExtra.base64Url) {
                                                content += `\n\n[Image: ${imgExtra.name}]\n(data:image included in base64)`;
                                        } else {
                                                content += '\n[Image attached]';
                                        }
                                } else if (extra.type === AttachmentType.TEXT) {
                                        const textExtra = extra as DatabaseMessageExtraTextFile;
                                        // For text files, upload to server first
                                        try {
                                                const uploadResult = await this.uploadTextContent(textExtra.name, textExtra.content);
                                                if (uploadResult.success) {
                                                        content += `\n\n[文件已上传: ${textExtra.name}]\n服务器路径: ${uploadResult.path}\n使用 /path ${uploadResult.path} 让模型读取此文件。`;
                                                } else {
                                                        // Fallback to embedding content
                                                        content += `\n\n[File: ${textExtra.name}]\n${textExtra.content}`;
                                                }
                                        } catch {
                                                // Fallback to embedding content
                                                content += `\n\n[File: ${textExtra.name}]\n${textExtra.content}`;
                                        }
                                } else if (extra.type === AttachmentType.PDF) {
                                        const pdfExtra = extra as DatabaseMessageExtraPdfFile;
                                        if (pdfExtra.content) {
                                                content += `\n\n[PDF: ${pdfExtra.name}]\n${pdfExtra.content}`;
                                        }
                                }
                        }
                }

                if (!content.trim()) {
                        onError?.(new Error('Empty message content'));
                        return;
                }

                // Handle stop signal - send /stop command to terminate model operation
                // Note: AbortSignal is Web standard API, but semantically it's a "stop" action
                if (signal) {
                        signal.addEventListener('abort', () => {
                                // 发送 /stop 终止模型当前操作，但不断开连接
                                // 用户仍可继续与模型交流
                                wsManager.send('/stop');
                                const stopError = new Error('Request stopped');
                                stopError.name = 'AbortError'; // Keep standard error name for compatibility
                                onError?.(stopError);
                        });
                }

                return new Promise((resolve, reject) => {
                        wsManager.sendMessage(content, {
                                onChunk: (chunk) => {
                                        onChunk?.(chunk);
                                },
                                onReasoningChunk: (chunk) => {
                                        onReasoningChunk?.(chunk);
                                },
                                onToolCallChunk: (chunk) => {
                                        onToolCallChunk?.(chunk);
                                },
                                onComplete: (content, reasoning, timings, toolCalls) => {
                                        onComplete?.(content, reasoning, timings as ChatMessageTimings, toolCalls);
                                        resolve();
                                },
                                onError: (error) => {
                                        onError?.(error);
                                        reject(error);
                                },
                                onTaskRunning: (running) => {
                                        onTaskRunning?.(running);
                                }
                        });
                });
        }

        static convertDbMessageToApiChatMessageData(
                message: DatabaseMessage & { extra?: DatabaseMessageExtra[] }
        ): ApiChatMessageData {
                if (message.role === MessageRole.TOOL && message.toolCallId) {
                        return {
                                role: MessageRole.TOOL,
                                content: message.content,
                                tool_call_id: message.toolCallId
                        };
                }

                let toolCalls: ApiChatCompletionToolCall[] | undefined;
                if (message.toolCalls) {
                        try {
                                toolCalls = JSON.parse(message.toolCalls);
                        } catch { /* ignore */ }
                }

                if (!message.extra || message.extra.length === 0) {
                        const result: ApiChatMessageData = {
                                role: message.role as MessageRole,
                                content: message.content
                        };
                        if (toolCalls && toolCalls.length > 0) {
                                result.tool_calls = toolCalls;
                        }
                        return result;
                }

                const contentParts: ApiChatMessageContentPart[] = [];

                if (message.content) {
                        contentParts.push({
                                type: ContentPartType.TEXT,
                                text: message.content
                        });
                }

                const imageFiles = message.extra.filter(
                        (extra): extra is DatabaseMessageExtraImageFile => extra.type === AttachmentType.IMAGE
                );
                for (const image of imageFiles) {
                        contentParts.push({
                                type: ContentPartType.IMAGE_URL,
                                image_url: { url: image.base64Url }
                        });
                }

                const textFiles = message.extra.filter(
                        (extra): extra is DatabaseMessageExtraTextFile => extra.type === AttachmentType.TEXT
                );
                for (const textFile of textFiles) {
                        contentParts.push({
                                type: ContentPartType.TEXT,
                                text: `[File: ${textFile.name}]\n${textFile.content}`
                        });
                }

                const result: ApiChatMessageData = {
                        role: message.role as MessageRole,
                        content: contentParts.length === 1 && contentParts[0].type === ContentPartType.TEXT
                                ? contentParts[0].text
                                : contentParts
                };
                if (toolCalls && toolCalls.length > 0) {
                        result.tool_calls = toolCalls;
                }
                return result;
        }

        /**
         * Upload text content to the server
         * This creates a temporary file on the server that the model can read
         */
        private static async uploadTextContent(filename: string, content: string): Promise<{ success: boolean; path: string }> {
                try {
                        // Create a blob from the text content
                        const blob = new Blob([content], { type: 'text/plain' });
                        const file = new File([blob], filename, { type: 'text/plain' });

                        // Upload to server
                        const formData = new FormData();
                        formData.append('file', file);

                        const response = await fetch('/upload', {
                                method: 'POST',
                                body: formData
                        });

                        if (!response.ok) {
                                throw new Error(`Upload failed: ${response.status}`);
                        }

                        const result = await response.json();
                        return {
                                success: result.success,
                                path: result.path
                        };
                } catch (error) {
                        console.error('Failed to upload text content:', error);
                        return { success: false, path: '' };
                }
        }

        static isConnected(): boolean {
                return wsManager.getStatus() === 'connected';
        }

        static getSessionId(): string {
                return wsManager.getSessionId();
        }

        static disconnect(): void {
                wsManager.disconnect();
        }
}

// Re-export types
export type { DatabaseMessage, DatabaseMessageExtra } from '$lib/types';
