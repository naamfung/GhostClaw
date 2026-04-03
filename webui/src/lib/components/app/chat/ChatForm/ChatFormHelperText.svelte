<script lang="ts">
        interface Props {
                class?: string;
                show?: boolean;
        }

        let { class: className = '', show = true }: Props = $props();
</script>

{#if show}
        <div class="mt-6 items-center justify-center {className} hidden md:flex">
                <p class="text-xs text-muted-foreground">
                        按 <kbd class="rounded bg-muted px-1 py-0.5 font-mono text-xs">Enter</kbd> 发送，
                        <kbd class="rounded bg-muted px-1 py-0.5 font-mono text-xs">Shift + Enter</kbd> 换行
                </p>
        </div>
{/if}
