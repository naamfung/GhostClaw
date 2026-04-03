<script lang="ts">
        import * as AlertDialog from '$lib/components/ui/alert-dialog';
        import { Button } from '$lib/components/ui/button';

        interface Props {
                open: boolean;
                currentTitle: string;
                newTitle: string;
                onConfirm: () => void;
                onCancel: () => void;
        }

        let { open = $bindable(), currentTitle, newTitle, onConfirm, onCancel }: Props = $props();
</script>

<AlertDialog.Root bind:open>
        <AlertDialog.Content>
                <AlertDialog.Header>
                        <AlertDialog.Title>更新对话标题？</AlertDialog.Title>

                        <AlertDialog.Description>
                                是否要将对话标题更新为与第一条消息内容相匹配？
                        </AlertDialog.Description>
                </AlertDialog.Header>

                <div class="space-y-4 pt-2 pb-6">
                        <div class="space-y-2">
                                <p class="text-sm font-medium text-muted-foreground">当前标题：</p>

                                <p class="rounded-md bg-muted/50 p-3 text-sm font-medium">{currentTitle}</p>
                        </div>

                        <div class="space-y-2">
                                <p class="text-sm font-medium text-muted-foreground">新标题：</p>

                                <p class="rounded-md bg-muted/50 p-3 text-sm font-medium">{newTitle}</p>
                        </div>
                </div>

                <AlertDialog.Footer>
                        <Button variant="outline" onclick={onCancel}>保持当前标题</Button>

                        <Button onclick={onConfirm}>更新标题</Button>
                </AlertDialog.Footer>
        </AlertDialog.Content>
</AlertDialog.Root>
