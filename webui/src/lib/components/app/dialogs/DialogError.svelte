<script lang="ts">
        import * as AlertDialog from '$lib/components/ui/alert-dialog';
        import { AlertTriangle, Copy } from '@lucide/svelte';
        import { copyToClipboard } from '$lib/utils';

        interface Props {
                open: boolean;
                title?: string;
                message: string;
                onOpenChange?: (open: boolean) => void;
        }

        let { open = $bindable(), title = '错误', message, onOpenChange }: Props = $props();

        function handleOpenChange(newOpen: boolean) {
                open = newOpen;
                onOpenChange?.(newOpen);
        }

        function copyError() {
                copyToClipboard(message);
        }
</script>

<AlertDialog.Root {open} onOpenChange={handleOpenChange}>
        <AlertDialog.Content style="overflow: hidden;">
                <AlertDialog.Header>
                        <AlertDialog.Title class="flex items-center gap-2">
                                <AlertTriangle class="h-5 w-5 text-destructive" />
                                {title}
                        </AlertDialog.Title>
                </AlertDialog.Header>

                <div class="rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive" style="overflow: hidden; white-space: normal; word-wrap: break-word;">
                        <p>{message}</p>
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
                        <AlertDialog.Action onclick={() => handleOpenChange(false)}>确定</AlertDialog.Action>
                </AlertDialog.Footer>
        </AlertDialog.Content>
</AlertDialog.Root>
