/**
 * Settings key constants for ChatSettings configuration.
 *
 * These keys correspond to properties in SettingsConfigType and are used
 * in settings field configurations to ensure consistency.
 */
export const SETTINGS_KEYS = {
        // General
        THEME: 'theme',
        DEFAULT_ROLE: 'defaultRole',
        DEFAULT_LANGUAGE: 'defaultLanguage',
        // Display
        SHOW_MESSAGE_STATS: 'showMessageStats',
        SHOW_THOUGHT_IN_PROGRESS: 'showThoughtInProgress',
        RENDER_USER_CONTENT_AS_MARKDOWN: 'renderUserContentAsMarkdown',
        DISABLE_AUTO_SCROLL: 'disableAutoScroll',
        ALWAYS_SHOW_SIDEBAR_ON_DESKTOP: 'alwaysShowSidebarOnDesktop',
        AUTO_SHOW_SIDEBAR_ON_NEW_CHAT: 'autoShowSidebarOnNewChat',
        FULL_HEIGHT_CODE_BLOCKS: 'fullHeightCodeBlocks',
        SHOW_RAW_MODEL_NAMES: 'showRawModelNames',
        // Sampling (only keep what GhostClaw backend uses)
        TEMPERATURE: 'temperature',
        MAX_TOKENS: 'max_tokens',
        // Timeout
        TIMEOUT_SHELL: 'timeoutShell',
        TIMEOUT_HTTP: 'timeoutHttp',
        TIMEOUT_PLUGIN: 'timeoutPlugin',
        TIMEOUT_BROWSER: 'timeoutBrowser',
        // Security
        ENABLE_SSRF_PROTECTION: 'enableSSRFProtection',
        // Browser
        BROWSER_USER_MODE: 'browserUserMode',
        BROWSER_HEADLESS: 'browserHeadless',
        BROWSER_DISABLE_GPU: 'browserDisableGPU',
        BROWSER_DISABLE_DEV_TOOLS: 'browserDisableDevTools',
        BROWSER_NO_SANDBOX: 'browserNoSandbox',
        BROWSER_DISABLE_TOOLS: 'browserDisableTools',
        // SmartShell
        SMART_SHELL_SYNC_TIMEOUT: 'smartShellSyncTimeout',
        SMART_SHELL_UNKNOWN_TIMEOUT: 'smartShellUnknownTimeout',
        SMART_SHELL_DEFAULT_WAKE_MINS: 'smartShellDefaultWakeMins',
        SMART_SHELL_MAX_DIRECT_OUTPUT: 'smartShellMaxDirectOutput',
        // Agent Loop
        MAX_AGENT_ITERATIONS: 'maxAgentIterations',
        // Security extra
        ALLOW_PRIVATE_IPS: 'allowPrivateIPs',
} as const;
