<script lang="ts">
	import { Search, X } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { ScrollArea } from '$lib/components/ui/scroll-area';
	import { SvelteSet } from 'svelte/reactivity';

	interface Props {
		conversations: DatabaseConversation[];
		messageCountMap?: Map<string, number>;
		mode: 'export' | 'import';
		onCancel: () => void;
		onConfirm: (已选择Conversations: DatabaseConversation[]) => void;
	}

	let { conversations, messageCountMap = new Map(), mode, onCancel, onConfirm }: Props = $props();

	let searchQuery = $state('');
	let 已选择Ids = $state.raw<SvelteSet<string>>(getInitialSelectedIds());
	let lastClickedId = $state<string | null>(null);

	function getInitialSelectedIds(): SvelteSet<string> {
		return new SvelteSet(conversations.map((c) => c.id));
	}

	let filteredConversations = $derived(
		conversations.filter((conv) => {
			const name = conv.name || '未命名对话';
			return name.toLowerCase().includes(searchQuery.toLowerCase());
		})
	);

	let allSelected = $derived(
		filteredConversations.length > 0 &&
			filteredConversations.every((conv) => 已选择Ids.has(conv.id))
	);

	let someSelected = $derived(
		filteredConversations.some((conv) => 已选择Ids.has(conv.id)) && !allSelected
	);

	function toggleConversation(id: string, shiftKey: boolean = false) {
		const newSet = new SvelteSet(已选择Ids);

		if (shiftKey && lastClickedId !== null) {
			const lastIndex = filteredConversations.findIndex((c) => c.id === lastClickedId);
			const currentIndex = filteredConversations.findIndex((c) => c.id === id);

			if (lastIndex !== -1 && currentIndex !== -1) {
				const start = Math.min(lastIndex, currentIndex);
				const end = Math.max(lastIndex, currentIndex);

				const shouldSelect = !newSet.has(id);

				for (let i = start; i <= end; i++) {
					if (shouldSelect) {
						newSet.add(filteredConversations[i].id);
					} else {
						newSet.delete(filteredConversations[i].id);
					}
				}

				已选择Ids = newSet;
				return;
			}
		}

		if (newSet.has(id)) {
			newSet.delete(id);
		} else {
			newSet.add(id);
		}

		已选择Ids = newSet;
		lastClickedId = id;
	}

	function toggleAll() {
		if (allSelected) {
			const newSet = new SvelteSet(已选择Ids);

			filteredConversations.forEach((conv) => newSet.delete(conv.id));
			已选择Ids = newSet;
		} else {
			const newSet = new SvelteSet(已选择Ids);

			filteredConversations.forEach((conv) => newSet.add(conv.id));
			已选择Ids = newSet;
		}
	}

	function handleConfirm() {
		const 已选择 = conversations.filter((conv) => selectedIds.has(conv.id));
		onConfirm(已选择);
	}

	function handleCancel() {
		已选择Ids = getInitialSelectedIds();
		searchQuery = '';
		lastClickedId = null;

		onCancel();
	}

	export function reset() {
		已选择Ids = getInitialSelectedIds();
		searchQuery = '';
		lastClickedId = null;
	}
</script>

<div class="space-y-4">
	<div class="relative">
		<Search class="absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" />

		<Input bind:value={searchQuery} placeholder="搜索对话..." class="pr-9 pl-9" />

		{#if searchQuery}
			<button
				class="absolute top-1/2 right-3 -translate-y-1/2 text-muted-foreground hover:text-foreground"
				onclick={() => (searchQuery = '')}
				type="button"
			>
				<X class="h-4 w-4" />
			</button>
		{/if}
	</div>

	<div class="flex items-center justify-between text-sm text-muted-foreground">
		<span>
			{已选择Ids.size} of {conversations.length} selected
			{#if searchQuery}
				({filteredConversations.length} 显示)
			{/if}
		</span>
	</div>

	<div class="overflow-hidden rounded-md border">
		<ScrollArea class="h-[400px]">
			<table class="w-full">
				<thead class="sticky top-0 z-10 bg-muted">
					<tr class="border-b">
						<th class="w-12 p-3 text-left">
							<Checkbox
								checked={allSelected}
								indeterminate={someSelected}
								onCheckedChange={toggleAll}
							/>
						</th>

						<th class="p-3 text-left text-sm font-medium">对话名称</th>

						<th class="w-32 p-3 text-left text-sm font-medium">消息数</th>
					</tr>
				</thead>
				<tbody>
					{#if filteredConversations.length === 0}
						<tr>
							<td colspan="3" class="p-8 text-center text-sm text-muted-foreground">
								{#if searchQuery}
									未找到匹配的对话" "{searchQuery}"
								{:else}
									无可用对话
								{/if}
							</td>
						</tr>
					{:else}
						{#each filteredConversations as conv (conv.id)}
							<tr
								class="cursor-pointer border-b transition-colors hover:bg-muted/50"
								onclick={(e) => toggleConversation(conv.id, e.shiftKey)}
							>
								<td class="p-3">
									<Checkbox
										checked={已选择Ids.has(conv.id)}
										onclick={(e) => {
											e.preventDefault();
											e.stopPropagation();
											toggleConversation(conv.id, e.shiftKey);
										}}
									/>
								</td>

								<td class="p-3 text-sm">
									<div class="max-w-[17rem] truncate" title={conv.name || '未命名对话'}>
										{conv.name || '未命名对话'}
									</div>
								</td>

								<td class="p-3 text-sm text-muted-foreground">
									{messageCountMap.get(conv.id) ?? 0}
								</td>
							</tr>
						{/each}
					{/if}
				</tbody>
			</table>
		</ScrollArea>
	</div>

	<div class="flex justify-end gap-2">
		<Button variant="outline" onclick={handleCancel}>取消</Button>

		<Button onclick={handleConfirm} disabled={已选择Ids.size === 0}>
			{mode === 'export' ? '导出' : '导入'} ({已选择Ids.size})
		</Button>
	</div>
</div>
