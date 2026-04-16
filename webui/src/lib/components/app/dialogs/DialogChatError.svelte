<script lang="ts">
        import * as AlertDialog from '$lib/components/ui/alert-dialog';
        import { AlertTriangle, TimerOff, Copy } from '@lucide/svelte';
        import { ErrorDialogType } from '$lib/enums';
        import { copyToClipboard } from '$lib/utils';

        interface Props {
                open: boolean;
                type: ErrorDialogType;
                message: string;
                contextInfo?: { n_prompt_tokens: number; n_ctx: number };
                onOpenChange?: (open: boolean) => void;
        }

        let { open = $bindable(), type, message, contextInfo, onOpenChange }: Props = $props();

        const isTimeout = $derived(type === ErrorDialogType.TIMEOUT);
        const title = $derived(isTimeout ? 'TCP 超时' : '服务器错误');
        const description = $derived(
                isTimeout
                        ? '请求在超时前未收到服务器响应。'
                        : '服务器返回了错误消息，请查看以下详情。'
        );
        const iconClass = $derived(isTimeout ? 'text-destructive' : 'text-amber-500');
        const badgeClass = $derived(
                isTimeout
                        ? 'border-destructive/40 bg-destructive/10 text-destructive'
                        : 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400'
        );

        function handleOpenChange(newOpen: boolean) {
                open = newOpen;
                onOpenChange?.(newOpen);
        }

        function copyError() {
                let errorText = message;
                if (contextInfo) {
                        errorText += `\n提示词令牌数：${contextInfo.n_prompt_tokens.toLocaleString()}`;
                        if (contextInfo.n_ctx) {
                                errorText += `\n上下文大小：${contextInfo.n_ctx.toLocaleString()}`;
                        }
                }
                copyToClipboard(errorText);
        }
</script>

<AlertDialog.Root {open} onOpenChange={handleOpenChange}>
        <AlertDialog.Content style="overflow: hidden;">
                <AlertDialog.Header>
                        <AlertDialog.Title class="flex items-center gap-2">
                                {#if isTimeout}
                                        <TimerOff class={`h-5 w-5 ${iconClass}`} />
                                {:else}
                                        <AlertTriangle class={`h-5 w-5 ${iconClass}`} />
                                {/if}

                                {title}
                        </AlertDialog.Title>

                        <AlertDialog.Description>
                                {description}
                        </AlertDialog.Description>
                </AlertDialog.Header>

                <div class={`rounded-lg border px-4 py-3 text-sm ${badgeClass}`} style="overflow: hidden; white-space: normal; word-wrap: break-word;">
                        <p class="font-medium">{message}</p>
                        {#if contextInfo}
                                <div class="mt-2 space-y-1 text-xs opacity-80">
                                        <p>
                                                <span class="font-medium">提示词令牌数：</span>
                                                {contextInfo.n_prompt_tokens.toLocaleString()}
                                        </p>
                                        {#if contextInfo.n_ctx}
                                                <p>
                                                        <span class="font-medium">上下文大小：</span>
                                                        {contextInfo.n_ctx.toLocaleString()}
                                                </p>
                                        {/if}
                                </div>
                        {/if}
                </div>

                <AlertDialog.Footer>
                        <!-- 使用普通 button 避免點擊後 AlertDialog.Action 自動關閉對話框 -->
                        <button
                                type="button"
                                onclick={copyError}
                                class="inline-flex items-center justify-center gap-2 rounded-md bg-transparent px-4 py-2 text-sm font-medium outline-none transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50"
                        >
                                <Copy class="h-4 w-4" />
                                复制
                        </button>
                        <AlertDialog.Action onclick={() => handleOpenChange(false)}>关闭</AlertDialog.Action>
                </AlertDialog.Footer>
        </AlertDialog.Content>
</AlertDialog.Root>
