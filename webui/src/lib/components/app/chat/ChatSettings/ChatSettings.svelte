<script lang="ts">
        import {
                Settings,
                Code,
                Monitor,
                ChevronLeft,
                ChevronRight,
                Database,
                Cpu,
                User,
                Wrench,
                Users,
                Timer,
                Shield,
                Globe
        } from '@lucide/svelte';
        import { McpLogo, McpServersSettings } from '$lib/components/app/mcp';
        import {
                ChatSettingsFooter,
                ChatSettingsImportExportTab,
                ChatSettingsFields
        } from '$lib/components/app';
        import { ScrollArea } from '$lib/components/ui/scroll-area';
        import { config, settingsStore } from '$lib/stores/settings.svelte';
        import {
                SETTINGS_SECTION_TITLES,
                type SettingsSectionTitle,
                NUMERIC_FIELDS,
                POSITIVE_INTEGER_FIELDS,
                SETTINGS_COLOR_MODES_CONFIG,
                SETTINGS_KEYS
        } from '$lib/constants';
        import { setMode } from 'mode-watcher';
        import { ColorMode } from '$lib/enums/ui';
        import { SettingsFieldType } from '$lib/enums/settings';
        import type { Component } from 'svelte';
        import ChatSettingsModelTab from './ChatSettingsModelTab.svelte';
        import ChatSettingsRolesTab from './ChatSettingsRolesTab.svelte';
        import ChatSettingsSkillsTab from './ChatSettingsSkillsTab.svelte';
        import ChatSettingsActorsTab from './ChatSettingsActorsTab.svelte';

        interface Props {
                onSave?: () => void;
                initialSection?: SettingsSectionTitle;
        }

        let { onSave, initialSection }: Props = $props();

        // 设置标签页顺序：模型相关 → 角色系统 → 界面设置 → 数据管理
        const settingSections: Array<{
                fields: SettingsFieldConfig[];
                icon: Component;
                title: SettingsSectionTitle;
        }> = [
                // ===== 模型相关设置 =====
                {
                        title: SETTINGS_SECTION_TITLES.MODEL,
                        icon: Cpu,
                        fields: [] // 使用自定义组件
                },
                // ===== 角色系统 =====
                {
                        title: SETTINGS_SECTION_TITLES.ROLES,
                        icon: User,
                        fields: [] // 使用自定义组件
                },
                {
                        title: SETTINGS_SECTION_TITLES.ACTORS,
                        icon: Users,
                        fields: [] // 使用自定义组件
                },
                {
                        title: SETTINGS_SECTION_TITLES.SKILLS,
                        icon: Wrench,
                        fields: [] // 使用自定义组件
                },
                // ===== 界面设置 =====
                {
                        title: SETTINGS_SECTION_TITLES.GENERAL,
                        icon: Settings,
                        fields: [
                                {
                                        key: SETTINGS_KEYS.THEME,
                                        label: '主题',
                                        type: SettingsFieldType.SELECT,
                                        options: SETTINGS_COLOR_MODES_CONFIG
                                },
                                {
                                        key: SETTINGS_KEYS.DEFAULT_ROLE,
                                        label: '默认角色',
                                        type: SettingsFieldType.TEXTAREA
                                }
                        ]
                },
                {
                        title: SETTINGS_SECTION_TITLES.DISPLAY,
                        icon: Monitor,
                        fields: [
                                {
                                        key: SETTINGS_KEYS.SHOW_MESSAGE_STATS,
                                        label: '显示消息生成统计',
                                        type: SettingsFieldType.CHECKBOX
                                },
                                {
                                        key: SETTINGS_KEYS.SHOW_THOUGHT_IN_PROGRESS,
                                        label: '显示思考过程',
                                        type: SettingsFieldType.CHECKBOX
                                },
                                {
                                        key: SETTINGS_KEYS.RENDER_USER_CONTENT_AS_MARKDOWN,
                                        label: '将用户内容渲染为 Markdown',
                                        type: SettingsFieldType.CHECKBOX
                                },
                                {
                                        key: SETTINGS_KEYS.FULL_HEIGHT_CODE_BLOCKS,
                                        label: '使用全高代码块',
                                        type: SettingsFieldType.CHECKBOX
                                },
                                {
                                        key: SETTINGS_KEYS.DISABLE_AUTO_SCROLL,
                                        label: '禁用自动滚动',
                                        type: SettingsFieldType.CHECKBOX
                                },
                                {
                                        key: SETTINGS_KEYS.ALWAYS_SHOW_SIDEBAR_ON_DESKTOP,
                                        label: '桌面端始终显示侧边栏',
                                        type: SettingsFieldType.CHECKBOX
                                },
                                {
                                        key: SETTINGS_KEYS.AUTO_SHOW_SIDEBAR_ON_NEW_CHAT,
                                        label: '新建对话时自动显示侧边栏',
                                        type: SettingsFieldType.CHECKBOX
                                },
                                {
                                        key: SETTINGS_KEYS.SHOW_RAW_MODEL_NAMES,
                                        label: '显示原始模型名称',
                                        type: SettingsFieldType.CHECKBOX
                                }
                        ]
                },
                // ===== 安全设置 =====
                {
                        title: SETTINGS_SECTION_TITLES.SECURITY,
                        icon: Shield,
                        fields: [
                                {
                                        key: SETTINGS_KEYS.ENABLE_SSRF_PROTECTION,
                                        label: '启用 SSRF 防护',
                                        type: SettingsFieldType.CHECKBOX
                                }
                        ]
                },
                // ===== 浏览器设置 =====
                {
                        title: SETTINGS_SECTION_TITLES.BROWSER,
                        icon: Globe,
                        fields: [
                                {
                                        key: SETTINGS_KEYS.BROWSER_USER_MODE,
                                        label: '用户模式',
                                        type: SettingsFieldType.CHECKBOX
                                },
                                {
                                        key: SETTINGS_KEYS.BROWSER_HEADLESS,
                                        label: '无头模式',
                                        type: SettingsFieldType.CHECKBOX
                                },
                                {
                                        key: SETTINGS_KEYS.BROWSER_DISABLE_GPU,
                                        label: '禁用 GPU',
                                        type: SettingsFieldType.CHECKBOX
                                },
                                {
                                        key: SETTINGS_KEYS.BROWSER_DISABLE_DEV_TOOLS,
                                        label: '禁用开发者工具',
                                        type: SettingsFieldType.CHECKBOX
                                },
                                {
                                        key: SETTINGS_KEYS.BROWSER_NO_SANDBOX,
                                        label: '禁用沙箱',
                                        type: SettingsFieldType.CHECKBOX
                                },
                                {
                                        key: SETTINGS_KEYS.BROWSER_DISABLE_TOOLS,
                                        label: '禁用内置浏览器工具',
                                        type: SettingsFieldType.CHECKBOX
                                }
                        ]
                },
                // ===== 数据管理 =====
                {
                        title: SETTINGS_SECTION_TITLES.IMPORT_EXPORT,
                        icon: Database,
                        fields: []
                },
                // ===== MCP 服务 =====
                {
                        title: SETTINGS_SECTION_TITLES.MCP,
                        icon: McpLogo,
                        fields: [] // 仅使用 McpServersSettings 组件
                },
                // ===== 超时配置 =====
                {
                        title: SETTINGS_SECTION_TITLES.TIMEOUT,
                        icon: Timer,
                        fields: [
                                {
                                        key: SETTINGS_KEYS.TIMEOUT_SHELL,
                                        label: 'Shell 命令超时（秒）',
                                        type: SettingsFieldType.INPUT
                                },
                                {
                                        key: SETTINGS_KEYS.TIMEOUT_HTTP,
                                        label: 'HTTP 请求超时（秒）',
                                        type: SettingsFieldType.INPUT
                                },
                                {
                                        key: SETTINGS_KEYS.TIMEOUT_PLUGIN,
                                        label: '插件 HTTP 超时（秒）',
                                        type: SettingsFieldType.INPUT
                                },
                                {
                                        key: SETTINGS_KEYS.TIMEOUT_BROWSER,
                                        label: '浏览器每步超时（秒）',
                                        type: SettingsFieldType.INPUT
                                }
                        ]
                }
        ];

        let activeSection = $derived<SettingsSectionTitle>(
                initialSection ?? SETTINGS_SECTION_TITLES.MODEL
        );
        let currentSection = $derived(
                settingSections.find((section) => section.title === activeSection) || settingSections[0]
        );
        let localConfig: SettingsConfigType = $state({ ...config() });

        let canScrollLeft = $state(false);
        let canScrollRight = $state(false);
        let scrollContainer: HTMLDivElement | undefined = $state();

        $effect(() => {
                if (initialSection) {
                        activeSection = initialSection;
                }
        });

        function handleThemeChange(newTheme: string) {
                localConfig.theme = newTheme;

                setMode(newTheme as ColorMode);
        }

        function handleConfigChange(key: string, value: string | boolean) {
                localConfig[key] = value;
        }

        function handleReset() {
                localConfig = { ...config() };

                setMode(localConfig.theme as ColorMode);
        }

        async function handleSave() {
                // Convert numeric strings to numbers for numeric fields
                const processedConfig = { ...localConfig };

                for (const field of NUMERIC_FIELDS) {
                        if (processedConfig[field] !== undefined && processedConfig[field] !== '') {
                                const numValue = Number(processedConfig[field]);
                                if (!isNaN(numValue)) {
                                        if ((POSITIVE_INTEGER_FIELDS as readonly string[]).includes(field)) {
                                                processedConfig[field] = Math.max(1, Math.round(numValue));
                                        } else {
                                                processedConfig[field] = numValue;
                                        }
                                } else {
                                        alert(`Invalid numeric value for ${field}. Please enter a valid number.`);
                                        return;
                                }
                        }
                }

                settingsStore.updateMultipleConfig(processedConfig);

                // Send configuration to backend
                try {
                        const backendConfig: Record<string, unknown> = {};

                        // Timeout configuration
                        backendConfig.timeout = {
                                shell: Number(processedConfig.timeoutShell) || 60,
                                http: Number(processedConfig.timeoutHttp) || 120,
                                plugin: Number(processedConfig.timeoutPlugin) || 120,
                                browser: Number(processedConfig.timeoutBrowser) || 30
                        };

                        // Default role
                        if (processedConfig.defaultRole !== undefined) {
                                backendConfig.defaultRole = processedConfig.defaultRole;
                        }

                        await fetch('/api/config', {
                                method: 'PUT',
                                headers: { 'Content-Type': 'application/json' },
                                body: JSON.stringify(backendConfig)
                        });
                } catch (error) {
                        console.error('Failed to save config to server:', error);
                }

                onSave?.();
        }

        function scrollToCenter(element: HTMLElement) {
                if (!scrollContainer) return;

                const containerRect = scrollContainer.getBoundingClientRect();
                const elementRect = element.getBoundingClientRect();

                const elementCenter = elementRect.left + elementRect.width / 2;
                const containerCenter = containerRect.left + containerRect.width / 2;
                const scrollOffset = elementCenter - containerCenter;

                scrollContainer.scrollBy({ left: scrollOffset, behavior: 'smooth' });
        }

        function scrollLeft() {
                if (!scrollContainer) return;

                scrollContainer.scrollBy({ left: -250, behavior: 'smooth' });
        }

        function scrollRight() {
                if (!scrollContainer) return;

                scrollContainer.scrollBy({ left: 250, behavior: 'smooth' });
        }

        function updateScrollButtons() {
                if (!scrollContainer) return;

                const { scrollLeft, scrollWidth, clientWidth } = scrollContainer;
                canScrollLeft = scrollLeft > 0;
                canScrollRight = scrollLeft < scrollWidth - clientWidth - 1; // -1 for rounding
        }

        export function reset() {
                localConfig = { ...config() };

                setTimeout(updateScrollButtons, 100);
        }

        $effect(() => {
                if (scrollContainer) {
                        updateScrollButtons();
                }
        });
</script>

<div class="flex h-full flex-col overflow-hidden md:flex-row">
        <!-- Desktop Sidebar -->
        <div class="hidden w-52 flex-shrink-0 border-r border-border/30 p-4 md:block">
                <nav class="space-y-1 py-2">
                        {#each settingSections as section (section.title)}
                                <button
                                        class="flex w-full cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-left text-sm transition-colors hover:bg-accent {activeSection ===
                                        section.title
                                                ? 'bg-accent text-accent-foreground'
                                                : 'text-muted-foreground'}"
                                        onclick={() => (activeSection = section.title)}
                                >
                                        <section.icon class="h-4 w-4 flex-shrink-0" />

                                        <span class="whitespace-nowrap">{section.title}</span>
                                </button>
                        {/each}
                </nav>
        </div>

        <!-- Mobile Header with Horizontal Scrollable Menu -->
        <div class="flex flex-col pt-6 md:hidden">
                <div class="border-b border-border/30 pt-4 md:py-4">
                        <!-- Horizontal Scrollable Category Menu with Navigation -->
                        <div class="relative flex items-center" style="scroll-padding: 1rem;">
                                <button
                                        class="absolute left-2 z-10 flex h-6 w-6 items-center justify-center rounded-full bg-muted shadow-md backdrop-blur-sm transition-opacity hover:bg-accent {canScrollLeft
                                                ? 'opacity-100'
                                                : 'pointer-events-none opacity-0'}"
                                        onclick={scrollLeft}
                                        aria-label="Scroll left"
                                >
                                        <ChevronLeft class="h-4 w-4" />
                                </button>

                                <div
                                        class="scrollbar-hide overflow-x-auto py-2"
                                        bind:this={scrollContainer}
                                        onscroll={updateScrollButtons}
                                >
                                        <div class="flex min-w-max gap-2">
                                                {#each settingSections as section (section.title)}
                                                        <button
                                                                class="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm whitespace-nowrap transition-colors first:ml-4 last:mr-4 hover:bg-accent {activeSection ===
                                                                section.title
                                                                        ? 'bg-accent text-accent-foreground'
                                                                        : 'text-muted-foreground'}"
                                                                onclick={(e: MouseEvent) => {
                                                                        activeSection = section.title;
                                                                        scrollToCenter(e.currentTarget as HTMLElement);
                                                                }}
                                                        >
                                                                <section.icon class="h-4 w-4 flex-shrink-0" />
                                                                <span>{section.title}</span>
                                                        </button>
                                                {/each}
                                        </div>
                                </div>

                                <button
                                        class="absolute right-2 z-10 flex h-6 w-6 items-center justify-center rounded-full bg-muted shadow-md backdrop-blur-sm transition-opacity hover:bg-accent {canScrollRight
                                                ? 'opacity-100'
                                                : 'pointer-events-none opacity-0'}"
                                        onclick={scrollRight}
                                        aria-label="Scroll right"
                                >
                                        <ChevronRight class="h-4 w-4" />
                                </button>
                        </div>
                </div>
        </div>

        <ScrollArea class="max-h-[calc(100dvh-13.5rem)] flex-1 md:max-h-[calc(100vh-13.5rem)]">
                <div class="space-y-6 p-4 md:p-6">
                        <div class="grid">
                                <div class="mb-6 flex hidden items-center gap-2 border-b border-border/30 pb-6 md:flex">
                                        <currentSection.icon class="h-5 w-5" />

                                        <h3 class="text-lg font-semibold">{currentSection.title}</h3>
                                </div>

                                {#if currentSection.title === SETTINGS_SECTION_TITLES.MODEL}
                                        <ChatSettingsModelTab />
                                {:else if currentSection.title === SETTINGS_SECTION_TITLES.ROLES}
                                        <ChatSettingsRolesTab />
                                {:else if currentSection.title === SETTINGS_SECTION_TITLES.SKILLS}
                                        <ChatSettingsSkillsTab />
                                {:else if currentSection.title === SETTINGS_SECTION_TITLES.ACTORS}
                                        <ChatSettingsActorsTab />
                                {:else if currentSection.title === SETTINGS_SECTION_TITLES.IMPORT_EXPORT}
                                        <ChatSettingsImportExportTab />
                                {:else if currentSection.title === SETTINGS_SECTION_TITLES.MCP}
                                        <McpServersSettings />
                                {:else}
                                        <div class="space-y-6">
                                                <ChatSettingsFields
                                                        fields={currentSection.fields}
                                                        {localConfig}
                                                        onConfigChange={handleConfigChange}
                                                        onThemeChange={handleThemeChange}
                                                />
                                        </div>
                                {/if}
                        </div>

                        {#if currentSection.title !== SETTINGS_SECTION_TITLES.ROLES &&
                                currentSection.title !== SETTINGS_SECTION_TITLES.SKILLS &&
                                currentSection.title !== SETTINGS_SECTION_TITLES.ACTORS}
                                <div class="mt-8 border-t pt-6">
                                        <p class="text-xs text-muted-foreground">设置保存在浏览器本地存储中</p>
                                </div>
                        {/if}
                </div>
        </ScrollArea>
</div>

<ChatSettingsFooter onReset={handleReset} onSave={handleSave} />
