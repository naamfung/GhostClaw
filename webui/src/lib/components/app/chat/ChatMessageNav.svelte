<script lang="ts">
	import { ChevronLeft, ChevronRight, MessageSquare } from '@lucide/svelte';
	import ScrollArea from '$lib/components/ui/scroll-area/scroll-area.svelte';
	import { activeMessages } from '$lib/stores/conversations.svelte';
	import { MessageRole } from '$lib/enums';
	import { getPreviewText } from '$lib/utils';
	import type { DatabaseMessage } from '$lib/types/database';

	interface Props {
		open?: boolean;
		onOpenChange?: (open: boolean) => void;
		scrollContainer: HTMLDivElement | undefined;
	};

	let { open = false, onOpenChange, scrollContainer }: Props = $props();

	let activeMessageId = $state<string | null>(null);

	const userMessages = $derived(
		activeMessages().filter(
			(m) => m.role === MessageRole.USER && m.content.trim()
		)
	);

	function toggle() {
		open = !open;
		onOpenChange?.(open);
	}

	function scrollToMessage(messageId: string) {
		const target = document.getElementById(`msg-${messageId}`);
		if (!target || !scrollContainer) return;

		activeMessageId = messageId;

		const containerRect = scrollContainer.getBoundingClientRect();
		const targetRect = target.getBoundingClientRect();
		const offset = targetRect.top - containerRect.top + scrollContainer.scrollTop - 80;

		scrollContainer.scrollTo({ top: offset, behavior: 'smooth' });

		setTimeout(() => {
			activeMessageId = null;
		}, 2000);
	}

	function getShortText(msg: DatabaseMessage): string {
		return getPreviewText(msg.content, 28) || '(无文字内容)';
	}
</script>

<!-- 整個導航欄容器：固定右側，用 translate 控制顯示/隱藏 -->
<div
	class="fixed top-0 z-40 flex h-full transition-transform duration-200 ease-linear"
	style="right: 0; width: var(--nav-width);"
	class:translate-x-0={open}
	class:translate-x-full={!open}
>
	<!-- 切換按鈕：固定在面板左邊緣外側，跟住面板一齊郁；附帶消息數量 -->
	<button
		class="absolute -left-6 top-20 flex h-8 min-w-6 items-center justify-center gap-1 rounded-l-md border border-border/40 border-r-0 bg-background/80 text-muted-foreground backdrop-blur-md hover:bg-accent hover:text-accent-foreground px-1.5"
		onclick={toggle}
		aria-label={open ? '收起消息导航' : '展开消息导航'}
		title={open ? '收起消息导航' : '展开消息导航'}
	>
		{#if open}
			<ChevronRight class="h-3.5 w-3.5" />
		{:else}
			<ChevronLeft class="h-3.5 w-3.5" />
		{/if}
		<span class="text-[10px] leading-none tabular-nums">{userMessages.length}</span>
	</button>

	<!-- 面板內容 -->
	<aside class="flex h-full w-full flex-col border-l border-border/30 bg-background/95 shadow-lg backdrop-blur-md">
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
</div>
