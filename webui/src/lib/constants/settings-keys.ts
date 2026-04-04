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
        BROWSER_NO_SANDBOX: 'browserNoSandbox'
} as const;
