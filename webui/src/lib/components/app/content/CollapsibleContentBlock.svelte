<script lang="ts">
        import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
        import * as Collapsible from '$lib/components/ui/collapsible/index.js';
        import { buttonVariants } from '$lib/components/ui/button/index.js';
        import { Card } from '$lib/components/ui/card';
        import { createAutoScrollController } from '$lib/hooks/use-auto-scroll.svelte';
        import type { Snippet } from 'svelte';
        import type { Component } from 'svelte';

        interface Props {
                open?: boolean;
                class?: string;
                icon?: Component;
                iconClass?: string;
                title: string;
                subtitle?: string;
                isStreaming?: boolean;
                /** When true, applies warning-style yellow border and background to indicate an error state */
                error?: boolean;
                onToggle?: () => void;
                children: Snippet;
        }

        let {
                open = $bindable(false),
                class: className = '',
                icon: Icon,
                iconClass = 'h-4 w-4',
                title,
                subtitle,
                isStreaming = false,
                error = false,
                onToggle,
                children
        }: Props = $props();

        let contentContainer: HTMLDivElement | undefined = $state();
        const autoScroll = createAutoScrollController();

        $effect(() => {
                autoScroll.setContainer(contentContainer);
        });

        $effect(() => {
                // Only auto-scroll when open and streaming
                autoScroll.updateInterval(open && isStreaming);
        });

        function handleScroll() {
                autoScroll.handleScroll();
        }

        const cardClass = $derived(
                error
                        ? 'gap-0 border-yellow-500/50 bg-yellow-500/8 py-0'
                        : 'gap-0 border-muted bg-muted/30 py-0'
        );

        const triggerTextColor = $derived(
                error ? 'text-yellow-600 dark:text-yellow-400' : 'text-muted-foreground'
        );

        const contentBorderClass = $derived(
                error ? 'border-yellow-500/30' : 'border-muted'
        );
</script>

<Collapsible.Root
        {open}
        onOpenChange={(value) => {
                open = value;
                onToggle?.();
        }}
        class={className}
>
        <Card class={cardClass}>
                <Collapsible.Trigger class="flex w-full cursor-pointer items-center justify-between p-3">
                        <div class="flex items-center gap-2 {triggerTextColor}">
                                {#if Icon}
                                        <Icon class={iconClass} />
                                {/if}

                                <span class="font-mono text-sm font-medium">{title}</span>

                                {#if subtitle}
                                        <span class="text-xs italic">{subtitle}</span>
                                {/if}
                        </div>

                        <div
                                class={buttonVariants({
                                        variant: 'ghost',
                                        size: 'sm',
                                        class: 'h-6 w-6 p-0 text-muted-foreground hover:text-foreground'
                                })}
                        >
                                <ChevronsUpDownIcon class="h-4 w-4" />

                                <span class="sr-only">Toggle content</span>
                        </div>
                </Collapsible.Trigger>

                <Collapsible.Content>
                        <div
                                bind:this={contentContainer}
                                class="overflow-y-auto border-t {contentBorderClass} px-3 pb-3"
                                onscroll={handleScroll}
                                style="min-height: var(--min-message-height); max-height: var(--max-message-height);"
                        >
                                {@render children()}
                        </div>
                </Collapsible.Content>
        </Card>
</Collapsible.Root>
