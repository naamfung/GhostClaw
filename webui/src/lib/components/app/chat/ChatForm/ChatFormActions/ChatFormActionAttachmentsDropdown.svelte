<script lang="ts">
        import { Plus } from '@lucide/svelte';
        import { Button } from '$lib/components/ui/button';
        import * as Tooltip from '$lib/components/ui/tooltip';

        interface Props {
                class?: string;
                disabled?: boolean;
                hasAudioModality?: boolean;
                hasVisionModality?: boolean;
                hasMcpPromptsSupport?: boolean;
                hasMcpResourcesSupport?: boolean;
                onFileUpload?: () => void;
                onSystemPromptClick?: () => void;
                onMcpPromptClick?: () => void;
                onMcpSettingsClick?: () => void;
                onMcpResourcesClick?: () => void;
        }

        let {
                class: className = '',
                disabled = false,
                onFileUpload
        }: Props = $props();

        const fileUploadTooltipText = 'Add files';
</script>

<div class="flex items-center gap-1 {className}">
        <Tooltip.Root>
                <Tooltip.Trigger class="w-full">
                        <Button
                                class="file-upload-button h-8 w-8 rounded-full p-0"
                                {disabled}
                                variant="secondary"
                                type="button"
                                onclick={() => onFileUpload?.()}
                        >
                                <span class="sr-only">{fileUploadTooltipText}</span>

                                <Plus class="h-4 w-4" />
                        </Button>
                </Tooltip.Trigger>

                <Tooltip.Content>
                        <p>{fileUploadTooltipText}</p>
                </Tooltip.Content>
        </Tooltip.Root>
</div>
