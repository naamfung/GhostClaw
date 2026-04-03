<script lang="ts">
        import * as AlertDialog from '$lib/components/ui/alert-dialog';
        import { FileX } from '@lucide/svelte';

        interface Props {
                open: boolean;
                emptyFiles: string[];
                onOpenChange?: (open: boolean) => void;
        }

        let { open = $bindable(), emptyFiles, onOpenChange }: Props = $props();

        function handleOpenChange(newOpen: boolean) {
                open = newOpen;
                onOpenChange?.(newOpen);
        }
</script>

<AlertDialog.Root {open} onOpenChange={handleOpenChange}>
        <AlertDialog.Content>
                <AlertDialog.Header>
                        <AlertDialog.Title class="flex items-center gap-2">
                                <FileX class="h-5 w-5 text-destructive" />

                                检测到空文件
                        </AlertDialog.Title>

                        <AlertDialog.Description>
                                以下文件为空，已从你的附件中移除：
                        </AlertDialog.Description>
                </AlertDialog.Header>

                <div class="space-y-3 text-sm">
                        <div class="rounded-lg bg-muted p-3">
                                <div class="mb-2 font-medium">空文件：</div>

                                <ul class="list-inside list-disc space-y-1 text-muted-foreground">
                                        {#each emptyFiles as fileName (fileName)}
                                                <li class="font-mono text-sm">{fileName}</li>
                                        {/each}
                                </ul>
                        </div>

                        <div>
                                <div class="mb-2 font-medium">说明：</div>

                                <ul class="list-inside list-disc space-y-1 text-muted-foreground">
                                        <li>空文件无法被处理或发送给 AI 模型</li>

                                        <li>这些文件已自动从你的附件中移除</li>

                                        <li>你可以尝试上传有内容的文件</li>
                                </ul>
                        </div>
                </div>

                <AlertDialog.Footer>
                        <AlertDialog.Action onclick={() => handleOpenChange(false)}>我知道了</AlertDialog.Action>
                </AlertDialog.Footer>
        </AlertDialog.Content>
</AlertDialog.Root>
