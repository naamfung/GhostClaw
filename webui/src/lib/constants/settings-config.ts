import { ColorMode } from '$lib/enums/ui';
import { Monitor, Moon, Sun } from '@lucide/svelte';

export const SETTING_CONFIG_DEFAULT: Record<string, string | number | boolean | undefined> = {
        // Note: in order not to introduce breaking changes, please keep the same data type (number, string, etc) if you want to change the default value.
        // Do not use nested objects, keep it single level. Prefix the key if you need to group them.
        theme: ColorMode.SYSTEM,
        defaultRole: '',
        showThoughtInProgress: false,
        showMessageStats: true,
        disableAutoScroll: false,
        renderUserContentAsMarkdown: false,
        alwaysShowSidebarOnDesktop: false,
        autoShowSidebarOnNewChat: true,
        fullHeightCodeBlocks: false,
        showRawModelNames: false,
        // sampling params: empty means "use server default"
        temperature: undefined,
        max_tokens: undefined,
        // timeout settings (in seconds)
        timeoutShell: 60,
        timeoutHttp: 120,
        timeoutPlugin: 120,
        timeoutBrowser: 30,
        // security settings
        enableSSRFProtection: true,
        // browser settings
        browserUserMode: false
};

export const SETTING_CONFIG_INFO: Record<string, string> = {
        theme:
                'Choose the color theme for the interface. You can choose between System (follows your device settings), Light, or Dark.',
        defaultRole:
                '设置 AI 的默认角色。留空则使用后端默认角色。',
        temperature:
                'Controls the randomness of the generated text by affecting the probability distribution of the output tokens. Higher = more random, lower = more focused.',
        max_tokens: 'The maximum number of token per output. Use -1 for infinite (no limit).',
        showThoughtInProgress: 'Expand thought process by default when generating messages.',
        showMessageStats:
                'Display generation statistics (tokens/second, token count, duration) below each assistant message.',
        disableAutoScroll:
                'Disable automatic scrolling while messages stream so you can control the viewport position manually.',
        renderUserContentAsMarkdown: 'Render user messages using markdown formatting in the chat.',
        alwaysShowSidebarOnDesktop:
                'Always keep the sidebar visible on desktop instead of auto-hiding it.',
        autoShowSidebarOnNewChat:
                'Automatically show sidebar when starting a new chat. Disable to keep the sidebar hidden until you click on it.',
        fullHeightCodeBlocks:
                'Always display code blocks at their full natural height, overriding any height limits.',
        showRawModelNames:
                'Display full raw model identifiers (e.g. "ggml-org/GLM-4.7-Flash-GGUF:Q8_0") instead of parsed names with badges.',
        enableSSRFProtection:
                '启用 SSRF 防护，阻止对内部网络地址的请求。',
        browserUserMode:
                '启用浏览器用户模式，使用普通用户权限运行浏览器操作。',
        // Timeout settings
        timeoutShell: 'Shell 命令执行的超时时间（秒）。超时后命令将被强制终止。',
        timeoutHttp: 'HTTP 请求的超时时间（秒）。包括 API 调用和网络请求。',
        timeoutPlugin: '插件内 HTTP 请求的超时时间（秒）。插件调用网络接口时的等待上限。',
        timeoutBrowser: '浏览器每次操作的超时时间（秒）。每次页面访问/搜索/下载的等待上限。'
};

export const SETTINGS_COLOR_MODES_CONFIG = [
        { value: ColorMode.SYSTEM, label: 'System', icon: Monitor },
        { value: ColorMode.LIGHT, label: 'Light', icon: Sun },
        { value: ColorMode.DARK, label: 'Dark', icon: Moon }
];
