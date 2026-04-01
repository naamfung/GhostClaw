<script lang="ts">
        import { Button } from '$lib/components/ui/button';
        import * as AlertDialog from '$lib/components/ui/alert-dialog';
        import { settingsStore } from '$lib/stores/settings.svelte';
        import { RotateCcw } from '@lucide/svelte';

        interface Props {
                onReset?: () => void;
                onSave?: () => void;
        }

        let { onReset, onSave }: Props = $props();

        let showResetDialog = $state(false);

        function handleResetClick() {
                showResetDialog = true;
        }

        function handleConfirmReset() {
                settingsStore.forceSyncWithServerDefaults();
                onReset?.();

                showResetDialog = false;
        }

        function handleSave() {
                onSave?.();
        }
</script>

<div class="flex justify-between border-t border-border/30 p-6">
        <div class="flex gap-2">
                <Button variant="outline" onclick={handleResetClick}>
                        <RotateCcw class="h-3 w-3" />

                        重置为默认
                </Button>
        </div>

        <Button onclick={handleSave}>保存设置</Button>
</div>

<AlertDialog.Root bind:open={showResetDialog}>
        <AlertDialog.Content>
                <AlertDialog.Header>
                        <AlertDialog.Title>重置设置为默认值</AlertDialog.Title>
                        <AlertDialog.Description>
                                确定要将所有设置重置为默认值吗？这将把所有参数重置为服务器提供的默认值，并移除你的所有自定义配置。
                        </AlertDialog.Description>
                </AlertDialog.Header>
                <AlertDialog.Footer>
                        <AlertDialog.Cancel>取消</AlertDialog.Cancel>
                        <AlertDialog.Action onclick={handleConfirmReset}>重置为默认</AlertDialog.Action>
                </AlertDialog.Footer>
        </AlertDialog.Content>
</AlertDialog.Root>
