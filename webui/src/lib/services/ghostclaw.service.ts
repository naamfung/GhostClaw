/**
 * GhostClaw WebSocket Service
 *
 * Handles WebSocket communication with the GhostClaw backend server.
 * This service replaces the HTTP-based ChatService for GhostClaw integration.
 */

export interface GhostClawWSMessage {
        content?: string;
}

export interface GhostClawWSChunk {
        session_id?: string;
        content?: string;
        reasoning_content?: string;
        tool_calls?: Array<{
                id?: string;
                type?: string;
                function?: {
                        name?: string;
                        arguments?: string;
                };
        }>;
        done?: boolean;
        error?: string;
        task_running?: boolean;
}

export type ConnectionStatus = 'connecting' | 'connected' | 'disconnected' | 'error';

export interface GhostClawWSOptions {
        onChunk?: (chunk: string) => void;
        onReasoningChunk?: (chunk: string) => void;
        onToolCallChunk?: (chunk: string) => void;
        onComplete?: (content: string) => void;
        onError?: (error: Error) => void;
        onStatusChange?: (status: ConnectionStatus) => void;
        onSessionId?: (sessionId: string) => void;
        onTaskRunning?: (running: boolean) => void;
}

class GhostClawWebSocketService {
        private ws: WebSocket | null = null;
        private status: ConnectionStatus = 'disconnected';
        private sessionId: string = '';
        private options: GhostClawWSOptions = {};
        private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
        private pendingMessage: string | null = null;

        connect(options: GhostClawWSOptions = {}): void {
                this.options = options;
                this.setStatus('connecting');

                const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
                const wsUrl = this.sessionId
                        ? `${wsProtocol}//${window.location.host}/ws?session=${this.sessionId}`
                        : `${wsProtocol}//${window.location.host}/ws`;

                try {
                        this.ws = new WebSocket(wsUrl);
                        this.setupEventHandlers();
                } catch (error) {
                        this.setStatus('error');
                        this.options.onError?.(error instanceof Error ? error : new Error('WebSocket connection failed'));
                        this.scheduleReconnect();
                }
        }

        private setupEventHandlers(): void {
                if (!this.ws) return;

                this.ws.onopen = () => {
                        this.setStatus('connected');
                        // Send pending message if any
                        if (this.pendingMessage) {
                                this.send(this.pendingMessage);
                                this.pendingMessage = null;
                        }
                };

                this.ws.onmessage = (event) => {
                        try {
                                const chunk: GhostClawWSChunk = JSON.parse(event.data);
                                this.handleChunk(chunk);
                        } catch (error) {
                                console.error('Failed to parse WebSocket message:', error);
                        }
                };

                this.ws.onerror = (error) => {
                        console.error('WebSocket error:', error);
                        this.setStatus('error');
                        this.options.onError?.(new Error('WebSocket connection error'));
                };

                this.ws.onclose = () => {
                        this.setStatus('disconnected');
                        this.scheduleReconnect();
                };
        }

        private handleChunk(chunk: GhostClawWSChunk): void {
                // Handle session ID
                if (chunk.session_id) {
                        this.sessionId = chunk.session_id;
                        this.options.onSessionId?.(chunk.session_id);
                }

                // Handle task running status
                if (chunk.task_running !== undefined) {
                        this.options.onTaskRunning?.(chunk.task_running);
                }

                // Handle error
                if (chunk.error) {
                        this.options.onError?.(new Error(chunk.error));
                        return;
                }

                // Handle content
                if (chunk.content) {
                        this.options.onChunk?.(chunk.content);
                }

                // Handle reasoning content
                if (chunk.reasoning_content) {
                        this.options.onReasoningChunk?.(chunk.reasoning_content);
                }

                // Handle tool calls
                if (chunk.tool_calls && chunk.tool_calls.length > 0) {
                        this.options.onToolCallChunk?.(JSON.stringify(chunk.tool_calls));
                }

                // Handle completion
                if (chunk.done) {
                        // Get accumulated content from the caller
                        this.options.onComplete?.('');
                }
        }

        private setStatus(status: ConnectionStatus): void {
                this.status = status;
                this.options.onStatusChange?.(status);
        }

        private scheduleReconnect(): void {
                if (this.reconnectTimer) {
                        clearTimeout(this.reconnectTimer);
                }
                this.reconnectTimer = setTimeout(() => {
                        if (this.status === 'disconnected' || this.status === 'error') {
                                this.connect(this.options);
                        }
                }, 3000);
        }

        send(message: string): void {
                if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
                        // Queue message for when connection is ready
                        this.pendingMessage = message;
                        return;
                }

                const msg: GhostClawWSMessage = { content: message };
                this.ws.send(JSON.stringify(msg));
        }

        /**
         * Send stop command to cancel current task without disconnecting
         * This allows the connection to remain open for future messages (e.g., wake notifications)
         */
        sendStop(): void {
                if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
                        return;
                }
                const msg: GhostClawWSMessage = { content: '/stop' };
                this.ws.send(JSON.stringify(msg));
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
                this.setStatus('disconnected');
        }

        getStatus(): ConnectionStatus {
                return this.status;
        }

        getSessionId(): string {
                return this.sessionId;
        }

        setSessionId(sessionId: string): void {
                this.sessionId = sessionId;
        }
}

// Singleton instance
export const ghostclawWS = new GhostClawWebSocketService();
