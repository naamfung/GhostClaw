<script lang="ts">
        import { chatStore } from '$lib/stores/chat.svelte';
        import type { TodoItem } from '$lib/services';

        const statusIcon = (status: string): string => {
                switch (status) {
                        case 'Pending':    return '○';
                        case 'InProgress': return '▶';
                        case 'Completed':  return '✓';
                        case 'Waiting':    return '⏳';
                        case 'Cancelled':  return '✕';
                        default:           return '?';
                }
        };

        const statusClass = (status: string): string => {
                switch (status) {
                        case 'Pending':    return 'todo-pending';
                        case 'InProgress': return 'todo-active';
                        case 'Completed':  return 'todo-done';
                        case 'Waiting':    return 'todo-waiting';
                        case 'Cancelled':  return 'todo-cancelled';
                        default:           return '';
                }
        };

        const visibleTodos = $derived(
                chatStore.currentTodos.length > 0 ? chatStore.currentTodos : null
        );

        const doneCount = $derived(
                chatStore.currentTodos.filter(t => t.status === 'Completed').length
        );

        const hasInProgress = $derived(
                chatStore.currentTodos.some(t => t.status === 'InProgress')
        );
</script>

{#if visibleTodos}
        <div class="todo-panel" class:has-active={hasInProgress}>
                <div class="todo-header">
                        <span class="todo-title">Tasks</span>
                        <span class="todo-count">{doneCount}/{visibleTodos.length}</span>
                </div>
                <div class="todo-list">
                        {#each visibleTodos as todo (todo.id)}
                                <div class="todo-item {statusClass(todo.status)}">
                                        <span class="todo-icon">{statusIcon(todo.status)}</span>
                                        <span class="todo-text">{todo.text}</span>
                                </div>
                        {/each}
                </div>
        </div>
{/if}

<style>
        .todo-panel {
                margin: 0 1rem 0.5rem 1rem;
                border: 1px solid var(--color-border, #333);
                border-radius: 8px;
                background: var(--color-bg-secondary, #1a1a2e);
                overflow: hidden;
                transition: border-color 0.3s;
        }
        .todo-panel.has-active {
                border-color: var(--color-accent, #4fc3f7);
        }

        .todo-header {
                display: flex;
                justify-content: space-between;
                align-items: center;
                padding: 6px 12px;
                background: var(--color-bg-tertiary, #16213e);
                border-bottom: 1px solid var(--color-border, #333);
                font-size: 12px;
        }
        .todo-title {
                font-weight: 600;
                color: var(--color-text-secondary, #aaa);
                text-transform: uppercase;
                letter-spacing: 0.5px;
        }
        .todo-count {
                font-family: monospace;
                color: var(--color-text-muted, #666);
        }

        .todo-list {
                max-height: 180px;
                overflow-y: auto;
                padding: 4px 0;
        }

        .todo-item {
                display: flex;
                align-items: center;
                gap: 8px;
                padding: 4px 12px;
                font-size: 13px;
                line-height: 1.4;
                transition: background 0.15s;
        }
        .todo-item:hover {
                background: var(--color-bg-hover, rgba(255,255,255,0.03));
        }

        .todo-icon {
                flex-shrink: 0;
                width: 16px;
                text-align: center;
                font-size: 12px;
        }

        .todo-text {
                flex: 1;
                color: var(--color-text, #ddd);
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
        }

        /* Status styles */
        .todo-pending .todo-icon { color: var(--color-text-muted, #666); }
        .todo-pending .todo-text { color: var(--color-text-secondary, #aaa); }

        .todo-active .todo-icon { color: var(--color-accent, #4fc3f7); }
        .todo-active .todo-text { color: var(--color-text, #eee); font-weight: 500; }

        .todo-done .todo-icon { color: #4caf50; }
        .todo-done .todo-text { color: var(--color-text-muted, #666); text-decoration: line-through; }

        .todo-waiting .todo-icon { color: #ff9800; }
        .todo-waiting .todo-text { color: var(--color-text-secondary, #aaa); font-style: italic; }

        .todo-cancelled .todo-icon { color: #f44336; }
        .todo-cancelled .todo-text { color: var(--color-text-muted, #666); text-decoration: line-through; }
</style>
