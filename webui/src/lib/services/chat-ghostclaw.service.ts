/**
 * GhostClaw Chat Service
 *
 * A WebSocket-based chat service that replaces the HTTP-based ChatService
 * for integration with the GhostClaw backend.
 *
 * This service maintains the same interface as ChatService.sendMessage
 * for compatibility with the existing chat store.
 */

import { ghostclawWS, type ConnectionStatus } from './ghostclaw.service';
import type { ApiChatMessageData } from '$lib/types/api';
import type { DatabaseMessage, DatabaseMessageExtra } from '$lib/types';
import { MessageRole } from '$lib/enums';

export interface GhostClawChatOptions {
        stream?: boolean;
        onChunk?: (chunk: string) => void;
        onComplete?: (content: string, reasoningContent?: string, timings?: unknown, toolCalls?: string) => void;
        onError?: (error: Error) => void;
        onReasoningChunk?: (chunk: string) => void;
        onToolCallChunk?: (chunk: string) => void;
        onModel?: (model: string) => void;
        onTaskRunning?: (running: boolean) => void;
        signal?: AbortSignal;
}

export class GhostClawChatService {
        private static accumulatedContent: string = '';
        private static accumulatedReasoning: string = '';

        /**
         * Send a message via WebSocket.
         * Note: GhostClaw backend handles conversation history internally,
         * so we only send the latest user message.
         */
        static async sendMessage(
                messages: ApiChatMessageData[] | (DatabaseMessage & { extra?: DatabaseMessageExtra[] })[],
                options: GhostClawChatOptions = {},
                _conversationId?: string,
                signal?: AbortSignal
        ): Promise<string | void> {
                const {
                        stream = true,
                        onChunk,
                        onComplete,
                        onError,
                        onReasoningChunk,
                        onToolCallChunk,
                        onTaskRunning
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
                        // Extract text from content parts
                        const textParts = lastMessage.content.filter(
                                part => part.type === 'text'
                        );
                        content = textParts.map(part => (part as { text: string }).text).join('\n');
                } else {
                        content = '';
                }

                if (!content.trim()) {
                        onError?.(new Error('Empty message content'));
                        return;
                }

                // Reset accumulated content
                this.accumulatedContent = '';
                this.accumulatedReasoning = '';

                // Handle abort signal
                // Instead of disconnecting, send stop command to cancel task
                // This keeps the WebSocket open for wake notifications
                if (signal) {
                        signal.addEventListener('abort', () => {
                                ghostclawWS.sendStop();
                                const abortError = new Error('Request aborted');
                                abortError.name = 'AbortError';
                                onError?.(abortError);
                        });
                }

                return new Promise((resolve, reject) => {
                        // Connect to WebSocket if not connected
                        ghostclawWS.connect({
                                onChunk: (chunk) => {
                                        this.accumulatedContent += chunk;
                                        onChunk?.(chunk);
                                },
                                onReasoningChunk: (chunk) => {
                                        this.accumulatedReasoning += chunk;
                                        onReasoningChunk?.(chunk);
                                },
                                onToolCallChunk: (chunk) => {
                                        onToolCallChunk?.(chunk);
                                },
                                onComplete: () => {
                                        onComplete?.(
                                                this.accumulatedContent,
                                                this.accumulatedReasoning || undefined,
                                                undefined,
                                                undefined
                                        );
                                        resolve();
                                },
                                onError: (error) => {
                                        onError?.(error);
                                        reject(error);
                                },
                                onSessionId: (sessionId) => {
                                        // Store session ID for reconnection
                                        localStorage.setItem('ghostclaw_session_id', sessionId);
                                },
                                onStatusChange: (status: ConnectionStatus) => {
                                        if (status === 'connected') {
                                                // Send the message once connected
                                                ghostclawWS.send(content);
                                        }
                                },
                                onTaskRunning: (running) => {
                                        onTaskRunning?.(running);
                                }
                        });

                        // If already connected, send immediately
                        if (ghostclawWS.getStatus() === 'connected') {
                                ghostclawWS.send(content);
                        }
                });
        }

        /**
         * Check if WebSocket is connected
         */
        static isConnected(): boolean {
                return ghostclawWS.getStatus() === 'connected';
        }

        /**
         * Get current session ID
         */
        static getSessionId(): string {
                return ghostclawWS.getSessionId();
        }

        /**
         * Disconnect WebSocket
         */
        static disconnect(): void {
                ghostclawWS.disconnect();
        }
}
