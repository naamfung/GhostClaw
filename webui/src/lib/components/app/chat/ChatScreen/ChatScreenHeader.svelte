<script lang="ts">
        import { Settings } from '@lucide/svelte';
        import { DialogChatSettings } from '$lib/components/app';
        import { Button } from '$lib/components/ui/button';
        import { useSidebar } from '$lib/components/ui/sidebar';
        import { serverStore, needsSetup } from '$lib/stores/server.svelte';
        import { SETTINGS_SECTION_TITLES } from '$lib/constants';
        import { onMount } from 'svelte';

        const sidebar = useSidebar();

        let settingsOpen = $state(false);
        let initialSection = $state<SettingsSectionTitle | undefined>(undefined);

        // 检测是否需要自动打开设置窗口
        $effect(() => {
                const currentNeedsSetup = needsSetup();
                if (currentNeedsSetup && !settingsOpen) {
                        // 自动打开设置窗口并定位到模型配置标签页
                        initialSection = SETTINGS_SECTION_TITLES.MODEL;
                        settingsOpen = true;
                }
        });

        function toggleSettings() {
                initialSection = undefined; // 用户手动点击时使用默认标签页
                settingsOpen = true;
        }

        function handleSettingsClose(open: boolean) {
                settingsOpen = open;
                if (!open) {
                        // 关闭时刷新 needsSetup 状态
                        serverStore.fetch();
                }
        }
</script>

<header
        class="pointer-events-none fixed top-0 right-0 left-0 z-50 flex items-center justify-end p-2 duration-200 ease-linear md:p-4 {sidebar.open
                ? 'md:left-[var(--sidebar-width)]'
                : ''}"
>
        <div class="pointer-events-auto flex items-center space-x-2">
                <Button
                        variant="ghost"
                        size="icon-lg"
                        onclick={toggleSettings}
                        class="rounded-full backdrop-blur-lg"
                >
                        <Settings class="h-4 w-4" />
                </Button>
        </div>
</header>

<DialogChatSettings open={settingsOpen} onOpenChange={handleSettingsClose} {initialSection} />
