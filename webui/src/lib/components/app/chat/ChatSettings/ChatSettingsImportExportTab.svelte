<script lang="ts">
        import { Download, Upload, Trash2 } from '@lucide/svelte';
        import { Button } from '$lib/components/ui/button';
        import { DialogConversationSelection, DialogConfirmation } from '$lib/components/app';
        import { createMessageCountMap } from '$lib/utils';
        import { ISO_DATE_TIME_SEPARATOR } from '$lib/constants';
        import { conversationsStore, conversations } from '$lib/stores/conversations.svelte';
        import { toast } from 'svelte-sonner';

        let exportedConversations = $state<DatabaseConversation[]>([]);
        let importedConversations = $state<DatabaseConversation[]>([]);
        let showExportSummary = $state(false);
        let showImportSummary = $state(false);

        let showExportDialog = $state(false);
        let showImportDialog = $state(false);
        let availableConversations = $state<DatabaseConversation[]>([]);
        let messageCountMap = $state<Map<string, number>>(new Map());
        let fullImportData = $state<Array<{ conv: DatabaseConversation; messages: DatabaseMessage[] }>>(
                []
        );

        // Delete functionality state
        let showDeleteDialog = $state(false);

        async function handleExportClick() {
                try {
                        const allConversations = conversations();
                        if (allConversations.length === 0) {
                                toast.info('No conversations to export');
                                return;
                        }

                        const conversationsWithMessages = await Promise.all(
                                allConversations.map(async (conv: DatabaseConversation) => {
                                        const messages = await conversationsStore.getConversationMessages(conv.id);
                                        return { conv, messages };
                                })
                        );

                        messageCountMap = createMessageCountMap(conversationsWithMessages);
                        availableConversations = allConversations;
                        showExportDialog = true;
                } catch (err) {
                        console.error('Failed to load conversations:', err);
                        alert('Failed to load conversations');
                }
        }

        async function handleExportConfirm(selectedConversations: DatabaseConversation[]) {
                try {
                        const allData: 已导出Conversations = await Promise.all(
                                selectedConversations.map(async (conv) => {
                                        const messages = await conversationsStore.getConversationMessages(conv.id);
                                        return { conv: $state.snapshot(conv), messages: $state.snapshot(messages) };
                                })
                        );

                        conversationsStore.downloadConversationFile(
                                allData,
                                `${new Date().toISOString().split(ISO_DATE_TIME_SEPARATOR)[0]}_conversations.json`
                        );

                        exportedConversations = selectedConversations;
                        showExportSummary = true;
                        showImportSummary = false;
                        showExportDialog = false;
                } catch (err) {
                        console.error('Export failed:', err);
                        alert('Failed to export conversations');
                }
        }

        async function handleImportClick() {
                try {
                        const input = document.createElement('input');

                        input.type = 'file';
                        input.accept = '.json';

                        input.onchange = async (e) => {
                                const file = (e.target as HTMLInputElement)?.files?.[0];
                                if (!file) return;

                                try {
                                        const text = await file.text();
                                        const parsedData = JSON.parse(text);
                                        let importedData: 已导出Conversations;

                                        if (Array.isArray(parsedData)) {
                                                importedData = parsedData;
                                        } else if (
                                                parsedData &&
                                                typeof parsedData === 'object' &&
                                                'conv' in parsedData &&
                                                'messages' in parsedData
                                        ) {
                                                // Single conversation object
                                                importedData = [parsedData];
                                        } else {
                                                throw new Error(
                                                        'Invalid file format: expected array of conversations or single conversation object'
                                                );
                                        }

                                        fullImportData = importedData;
                                        availableConversations = importedData.map(
                                                (item: { conv: DatabaseConversation; messages: DatabaseMessage[] }) => item.conv
                                        );
                                        messageCountMap = createMessageCountMap(importedData);
                                        showImportDialog = true;
                                } catch (err: unknown) {
                                        const message = err instanceof Error ? err.message : 'Unknown error';

                                        console.error('Failed to parse file:', err);
                                        alert(`Failed to parse file: ${message}`);
                                }
                        };

                        input.click();
                } catch (err) {
                        console.error('Import failed:', err);
                        alert('Failed to import conversations');
                }
        }

        async function handleImportConfirm(selectedConversations: DatabaseConversation[]) {
                try {
                        const selectedIds = new Set(selectedConversations.map((c) => c.id));
                        const selectedData = $state
                                .snapshot(fullImportData)
                                .filter((item) => selectedIds.has(item.conv.id));

                        await conversationsStore.importConversationsData(selectedData);

                        importedConversations = selectedConversations;
                        showImportSummary = true;
                        showExportSummary = false;
                        showImportDialog = false;
                } catch (err) {
                        console.error('Import failed:', err);
                        alert('Failed to import conversations. Please check the file format.');
                }
        }

        async function handleDeleteAllClick() {
                try {
                        const allConversations = conversations();

                        if (allConversations.length === 0) {
                                toast.info('No conversations to delete');
                                return;
                        }

                        showDeleteDialog = true;
                } catch (err) {
                        console.error('Failed to load conversations for deletion:', err);
                        toast.error('Failed to load conversations');
                }
        }

        async function handleDeleteAllConfirm() {
                try {
                        await conversationsStore.deleteAll();

                        showDeleteDialog = false;
                } catch (err) {
                        console.error('Failed to delete conversations:', err);
                }
        }

        function handleDeleteAllCancel() {
                showDeleteDialog = false;
        }
</script>

<div class="space-y-6">
        <div class="space-y-4">
                <div class="grid">
                        <h4 class="mb-2 text-sm font-medium">导出对话</h4>

                        <p class="mb-4 text-sm text-muted-foreground">
                                下载所有对话为 JSON 文件。包括所有消息、附件和
                                对话历史。
                        </p>

                        <Button
                                class="w-full justify-start justify-self-start md:w-auto"
                                onclick={handleExportClick}
                                variant="outline"
                        >
                                <Download class="mr-2 h-4 w-4" />

                                导出对话
                        </Button>

                        {#if showExportSummary && exportedConversations.length > 0}
                                <div class="mt-4 grid overflow-x-auto rounded-lg border border-border/50 bg-muted/30 p-4">
                                        <h5 class="mb-2 text-sm font-medium">
                                                已导出 {exportedConversations.length} conversation{exportedConversations.length === 1
                                                        ? ''
                                                        : 's'}
                                        </h5>

                                        <ul class="space-y-1 text-sm text-muted-foreground">
                                                {#each exportedConversations.slice(0, 10) as conv (conv.id)}
                                                        <li class="truncate">• {conv.name || '未命名对话'}</li>
                                                {/each}

                                                {#if exportedConversations.length > 10}
                                                        <li class="italic">
                                                                ... 还有 {exportedConversations.length - 10} 个
                                                        </li>
                                                {/if}
                                        </ul>
                                </div>
                        {/if}
                </div>

                <div class="grid border-t border-border/30 pt-4">
                        <h4 class="mb-2 text-sm font-medium">导入对话</h4>

                        <p class="mb-4 text-sm text-muted-foreground">
                                Import one or 个 conversations from a previously exported JSON file. This will merge with
                                你现有的对话合并。
                        </p>

                        <Button
                                class="w-full justify-start justify-self-start md:w-auto"
                                onclick={handleImportClick}
                                variant="outline"
                        >
                                <Upload class="mr-2 h-4 w-4" />
                                导入对话
                        </Button>

                        {#if showImportSummary && importedConversations.length > 0}
                                <div class="mt-4 grid overflow-x-auto rounded-lg border border-border/50 bg-muted/30 p-4">
                                        <h5 class="mb-2 text-sm font-medium">
                                                已导入 {importedConversations.length} conversation{importedConversations.length === 1
                                                        ? ''
                                                        : 's'}
                                        </h5>

                                        <ul class="space-y-1 text-sm text-muted-foreground">
                                                {#each importedConversations.slice(0, 10) as conv (conv.id)}
                                                        <li class="truncate">• {conv.name || '未命名对话'}</li>
                                                {/each}

                                                {#if importedConversations.length > 10}
                                                        <li class="italic">
                                                                ... 还有 {importedConversations.length - 10} 个
                                                        </li>
                                                {/if}
                                        </ul>
                                </div>
                        {/if}
                </div>

                <div class="grid border-t border-border/30 pt-4">
                        <h4 class="mb-2 text-sm font-medium text-destructive">删除所有对话</h4>

                        <p class="mb-4 text-sm text-muted-foreground">
                                Permanently delete all conversati... 还有 their messages. This action cannot be undone.
                                如果要保留备份，请先导出对话。
                        </p>

                        <Button
                                class="w-full justify-start justify-self-start bg-destructive text-white hover:bg-destructive/80 md:w-auto"
                                onclick={handleDeleteAllClick}
                                variant="destructive"
                        >
                                <Trash2 class="mr-2 h-4 w-4" />

                                删除所有对话
                        </Button>
                </div>
        </div>
</div>

<DialogConversationSelection
        conversations={availableConversations}
        {messageCountMap}
        mode="export"
        bind:open={showExportDialog}
        onCancel={() => (showExportDialog = false)}
        onConfirm={handleExportConfirm}
/>

<DialogConversationSelection
        conversations={availableConversations}
        {messageCountMap}
        mode="import"
        bind:open={showImportDialog}
        onCancel={() => (showImportDialog = false)}
        onConfirm={handleImportConfirm}
/>

<DialogConfirmation
        bind:open={showDeleteDialog}
        title="删除所有对话"
        description="Are you sure you want to delete all conversations? This action cannot be und... 还有 will permanently remove all your conversations and messages."
        confirmText="全部删除"
        cancelText="取消"
        variant="destructive"
        icon={Trash2}
        onConfirm={handleDeleteAllConfirm}
        onCancel={handleDeleteAllCancel}
/>
