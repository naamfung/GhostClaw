<script lang="ts">
        import * as AlertDialog from '$lib/components/ui/alert-dialog';
        import { AlertTriangle } from '@lucide/svelte';

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
</script>

<AlertDialog.Root {open} onOpenChange={handleOpenChange}>
        <AlertDialog.Content>
                <AlertDialog.Header>
                        <AlertDialog.Title class="flex items-center gap-2">
                                <AlertTriangle class="h-5 w-5 text-destructive" />
                                {title}
                        </AlertDialog.Title>
                </AlertDialog.Header>

                <div class="rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                        <p>{message}</p>
                </div>

                <AlertDialog.Footer>
                        <AlertDialog.Action onclick={() => handleOpenChange(false)}>确定</AlertDialog.Action>
                </AlertDialog.Footer>
        </AlertDialog.Content>
</AlertDialog.Root>
