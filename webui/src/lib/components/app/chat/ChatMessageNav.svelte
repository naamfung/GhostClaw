<script lang="ts">
	import { ChevronLeft, ChevronRight, MessageSquare } from '@lucide/svelte';
	import ScrollArea from '$lib/components/ui/scroll-area/scroll-area.svelte';
	import { activeMessages } from '$lib/stores/conversations.svelte';
	import { MessageRole } from '$lib/enums';
	import { getPreviewText } from '$lib/utils';
	import type { DatabaseMessage } from '$lib/types/database';

	interface Props {
		scrollContainer: HTMLDivElement | undefined;
	};

	let { scrollContainer }: Props = $props();

	let isOpen = $state(false);
	let activeMessageId = $state<string | null>(null);

	const userMessages = $derived(
		activeMessages().filter(
			(m) => m.role === MessageRole.USER && m.content.trim()
		)
	);

	function scrollToMessage(messageId: string) {
		const target = document.getElementById(`msg-${messageId}`);
		if (!target || !scrollContainer) return;

		activeMessageId = messageId;

		// 計算目標相對滾動容器的偏移量
		const containerRect = scrollContainer.getBoundingClientRect();
		const targetRect = target.getBoundingClientRect();
		const offset = targetRect.top - containerRect.top + scrollContainer.scrollTop - 80;

		scrollContainer.scrollTo({ top: offset, behavior: 'smooth' });

		// 短暫高亮效果
		setTimeout(() => {
			activeMessageId = null;
		}, 2000);
	}

	function getShortText(msg: DatabaseMessage): string {
		return getPreviewText(msg.content, 28) || '(无文字内容)';
	}
</script>

<!-- Toggle button — 固定在右側 -->
<button
	class="fixed top-20 z-40 flex h-8 w-6 items-center justify-center rounded-l-md border border-border/40 border-r-0 bg-background/80 text-muted-foreground backdrop-blur-md transition-all hover:bg-accent hover:text-accent-foreground"
	class:right-0={!isOpen}
	style:right={isOpen ? 'var(--nav-width)' : '0'}
	onclick={() => (isOpen = !isOpen)}
	aria-label={isOpen ? '收起消息导航' : '展开消息导航'}
	title={isOpen ? '收起消息导航' : '展开消息导航'}
>
	{#if isOpen}
		<ChevronRight class="h-3.5 w-3.5" />
	{:else}
		<ChevronLeft class="h-3.5 w-3.5" />
	{/if}
</button>

<!-- 右側導航面板 -->
{#if isOpen}
	<aside
		class="fixed top-0 right-0 z-30 flex h-full flex-col border-l border-border/30 bg-background/95 shadow-lg backdrop-blur-md transition-all duration-200 ease-linear"
		style="width: var(--nav-width);"
		in:fly={{ x: 80, duration: 200 }}
		out:fly={{ x: 80, duration: 200 }}
	>
		<!-- 標題列 -->
		<div class="flex items-center justify-between border-b border-border/30 px-4 py-3">
			<h3 class="text-sm font-semibold">消息导航</h3>
			<span class="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
				{userMessages.length} 条
			</span>
		</div>

		<!-- 消息列表 -->
		<ScrollArea class="flex-1">
			<nav class="flex flex-col py-1">
				{#if userMessages.length === 0}
					<div class="px-4 py-8 text-center text-sm text-muted-foreground">
						暂无用户消息
					</div>
				{:else}
					{#each userMessages as msg (msg.id)}
						{@const isActive = activeMessageId === msg.id}
						<button
							class="flex w-full cursor-pointer items-start gap-2 px-4 py-2.5 text-left text-xs transition-colors hover:bg-accent/60 {isActive
								? 'bg-accent/40 text-accent-foreground'
								: 'text-muted-foreground'}"
							onclick={() => scrollToMessage(msg.id)}
							title={(msg.content || '').slice(0, 200)}
						>
							<MessageSquare class="mt-0.5 h-3 w-3 flex-shrink-0 text-muted-foreground/60" />
							<span class="line-clamp-1 leading-relaxed">
								{getShortText(msg)}
							</span>
						</button>
					{/each}
				{/if}
			</nav>
		</ScrollArea>
	</aside>
{/if}
