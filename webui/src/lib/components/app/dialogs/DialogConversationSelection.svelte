<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { ConversationSelection } from '$lib/components/app';

	interface Props {
		conversations: DatabaseConversation[];
		messageCountMap?: Map<string, number>;
		mode: 'export' | 'import';
		onCancel: () => void;
		onConfirm: (selectedConversations: DatabaseConversation[]) => void;
		open?: boolean;
	}

	let {
		conversations,
		messageCountMap = new Map(),
		mode,
		onCancel,
		onConfirm,
		open = $bindable(false)
	}: Props = $props();

	let conversationSelectionRef: ConversationSelection | undefined = $state();

	let previousOpen = $state(false);

	$effect(() => {
		if (open && !previousOpen && conversationSelectionRef) {
			conversationSelectionRef.reset();
		} else if (!open && previousOpen) {
			onCancel();
		}

		previousOpen = open;
	});
</script>

<Dialog.Root bind:open>
	<Dialog.Portal>
		<Dialog.Overlay class="z-[1000000]" />

		<Dialog.Content class="z-[1000001] max-w-2xl">
			<Dialog.Header>
				<Dialog.Title>
					选择要 {mode === 'export' ? '导出' : '导入'}
				</Dialog.Title>
				<Dialog.Description>
					{#if mode === 'export'}
						选择要导出的对话。选中的对话将下载
						为 JSON 文件。
					{:else}
						选择要导入的对话。选中的对话将
						与你现有的对话合并。
					{/if}
				</Dialog.Description>
			</Dialog.Header>

			<ConversationSelection
				bind:this={conversationSelectionRef}
				{conversations}
				{messageCountMap}
				{mode}
				{onCancel}
				{onConfirm}
			/>
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>
