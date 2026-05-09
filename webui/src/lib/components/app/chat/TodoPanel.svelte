<script lang="ts">
        import { chatStore } from '$lib/stores/chat.svelte';
        import type { TodoItem } from '$lib/services';
        import { ChevronDown, ChevronRight, ListTodo } from '@lucide/svelte';

        let collapsed = $state(false);

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

        function toggleCollapse() {
                collapsed = !collapsed;
        }
</script>

{#if visibleTodos}
        <div class="todo-panel" class:has-active={hasInProgress}>
                <!-- Header -->
                <button class="todo-header" onclick={toggleCollapse} type="button">
                        <div class="todo-header-left">
                                {#if collapsed}
                                        <ChevronRight class="h-3.5 w-3.5" />
                                {:else}
                                        <ChevronDown class="h-3.5 w-3.5" />
                                {/if}
                                <ListTodo class="h-3.5 w-3.5" />
                                <span class="todo-title">Tasks</span>
                                <span class="todo-badge">{doneCount}/{visibleTodos.length}</span>
                        </div>
                </button>

                <!-- Body (collapsible) -->
                {#if !collapsed}
                        <div class="todo-list">
                                {#each visibleTodos as todo (todo.id)}
                                        <div class="todo-item {statusClass(todo.status)}">
                                                <span class="todo-icon">{statusIcon(todo.status)}</span>
                                                <span class="todo-text">{todo.text}</span>
                                        </div>
                                {/each}
                        </div>
                {/if}
        </div>
{/if}

<style>
        .todo-panel {
                margin: 0 0.75rem 0.25rem 0.75rem;
                border: 1px solid hsl(var(--border));
                border-radius: 0.5rem;
                background: hsl(var(--muted) / 0.3);
                overflow: hidden;
                transition: border-color 0.2s;
                max-width: 100%;
        }
        .todo-panel.has-active {
                border-color: hsl(var(--accent) / 0.5);
        }

        .todo-header {
                display: flex;
                align-items: center;
                width: 100%;
                padding: 6px 10px;
                background: hsl(var(--muted) / 0.5);
                border: none;
                cursor: pointer;
                color: inherit;
                font: inherit;
        }
        .todo-header:hover {
                background: hsl(var(--muted) / 0.7);
        }
        .todo-header-left {
                display: flex;
                align-items: center;
                gap: 6px;
        }

        .todo-title {
                font-size: 12px;
                font-weight: 600;
                color: hsl(var(--muted-foreground));
                text-transform: uppercase;
                letter-spacing: 0.3px;
        }
        .todo-badge {
                font-size: 11px;
                font-family: monospace;
                color: hsl(var(--muted-foreground));
                background: hsl(var(--muted) / 0.8);
                padding: 1px 6px;
                border-radius: 3px;
        }

        .todo-list {
                max-height: 180px;
                overflow-y: auto;
                padding: 4px 0;
                border-top: 1px solid hsl(var(--border));
        }

        .todo-item {
                display: flex;
                align-items: center;
                gap: 8px;
                padding: 4px 12px;
                font-size: 13px;
                line-height: 1.4;
                transition: background 0.1s;
        }
        .todo-item:hover {
                background: hsl(var(--muted) / 0.4);
        }

        .todo-icon {
                flex-shrink: 0;
                width: 16px;
                text-align: center;
                font-size: 11px;
        }

        .todo-text {
                flex: 1;
                color: hsl(var(--foreground));
                overflow: hidden;
                text-overflow: ellipsis;
                white-space: nowrap;
        }

        /* Status styles */
        .todo-pending .todo-icon { color: hsl(var(--muted-foreground)); }
        .todo-pending .todo-text { color: hsl(var(--foreground) / 0.8); }

        .todo-active .todo-icon { color: hsl(var(--accent)); }
        .todo-active .todo-text { color: hsl(var(--foreground)); font-weight: 500; }

        .todo-done .todo-icon { color: hsl(142, 71%, 45%); }
        .todo-done .todo-text { color: hsl(var(--muted-foreground)); text-decoration: line-through; }

        .todo-waiting .todo-icon { color: hsl(32, 95%, 44%); }
        .todo-waiting .todo-text { color: hsl(var(--foreground) / 0.7); font-style: italic; }

        .todo-cancelled .todo-icon { color: hsl(var(--destructive)); }
        .todo-cancelled .todo-text { color: hsl(var(--muted-foreground)); text-decoration: line-through; }
</style>
