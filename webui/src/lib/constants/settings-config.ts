import { ColorMode } from '$lib/enums/ui';
import { Monitor, Moon, Sun } from '@lucide/svelte';

export const SETTING_CONFIG_DEFAULT: Record<string, string | number | boolean | undefined> = {
        // Note: in order not to introduce breaking changes, please keep the same data type (number, string, etc) if you want to change the default value.
        // Do not use nested objects, keep it single level. Prefix the key if you need to group them.
        theme: ColorMode.SYSTEM,
        defaultRole: '',
        defaultLanguage: '',
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
        timeoutMin: 0,  // 0 = no minimum
        timeoutShell: 60,
        timeoutHttp: 120,
        timeoutPlugin: 120,
        timeoutBrowser: 90,
        // security settings
        enableSSRFProtection: true,
        allowPrivateIPs: false,
        // browser settings
        browserUserMode: true,
        browserHeadless: false,
        browserDisableGPU: false,
        browserDisableDevTools: false,
        browserNoSandbox: true,
        browserDisableTools: false,
        // SmartShell settings
        smartShellSyncTimeout: 60,
        smartShellUnknownTimeout: 120,
        smartShellDefaultWakeMins: 5,

        // agent loop
        maxAgentIterations: 0,
        // compression
        compressionMode: 'token',
        compressionThreshold: 0.8,
        // skill
        skillCleanupThresholdDays: 90,
        // escalation
        escalationThreshold: 3,
        // resilience (network resilience)
        resilienceEnableFailover: true,
        resilienceEnableTimeoutScaling: true,
        resilienceMaxRetries: 0, // 0 = unlimited (when no failover)
        resilienceTimeoutScaleFactor: 1.5,
        resilienceMaxTimeoutSeconds: 600,
        resilienceInitialBackoffSeconds: 5,
        resilienceMaxBackoffSeconds: 300,
        resilienceBackoffMultiplier: 2.0,
        // prompt cache
        promptCacheEnabled: true,
        promptCacheStableTools: true,
};

export const SETTING_CONFIG_INFO: Record<string, string> = {
        theme:
                'Choose the color theme for the interface. You can choose between System (follows your device settings), Light, or Dark.',
        defaultRole:
                '设置 AI 的默认角色。留空则使用后端默认角色。',
        defaultLanguage:
                '约束 AI 的输出语言。使用自然语言描述，如「简体中文」「广东简体粤语」「English」。留空则不限制。',
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
        browserHeadless:
                '启用无头模式，在后台运行浏览器而不显示窗口。',
        browserDisableGPU:
                '禁用 GPU 加速，可在无头模式下节省资源。',
        browserDisableDevTools:
                '禁用开发者工具，可在无头模式下禁用调试功能。',
        browserNoSandbox:
                '禁用沙箱，提高兼容性但降低安全性（推荐启用）。',
        browserDisableTools:
                '当 opencli 可用时，禁用内置浏览器工具，强制使用 opencli 进行网页操作。',
        // Timeout settings
        timeoutMin: '全局最低超时（秒）。设定后所有超时选项自动提升至此值。设为 0 则不启用。',
        timeoutShell: 'Shell 命令执行的超时时间（秒）。超时后命令将被强制终止。',
        timeoutHttp: 'HTTP 请求的超时时间（秒）。包括 API 调用和网络请求。',
        timeoutPlugin: '插件内 HTTP 请求的超时时间（秒）。插件调用网络接口时的等待上限。',
        timeoutBrowser: '浏览器每次操作的超时时间（秒）。每次页面访问/搜索/下载的等待上限。',
        // SmartShell settings
        smartShellSyncTimeout: 'SmartShell 快速命令（已知命令）的超时时间（秒）。',
        smartShellUnknownTimeout: 'SmartShell 未知命令的超时时间（秒）。未知命令可能需要更长的等待时间。',
        smartShellDefaultWakeMins: 'SmartShell 异步任务的默认唤醒间隔（分钟）。',

        // agent loop
        maxAgentIterations: 'Agent Loop 最大迭代次数。设为 0 则不限制（默认 100）。降低此值可防止模型无限制循环。',
        // compression
        compressionMode: '壓縮觸發模式。「Token 模式」根據估算 token 數判斷是否需要壓縮；「消息計數模式」根據消息數量判斷。',
        compressionThreshold: 'Token 模式下觸發壓縮的閾值（0.1-0.9）。例如 0.8 表示 token 數超過 context window 嘅 80% 時觸發壓縮。',
        // skill
        skillCleanupThresholdDays: 'Skill 自動清理閾值（天）。長期未使用且使用次數少嘅 skill 超過此天數後會被自動刪除。範圍 30-365，預設 90。',
        // escalation
        escalationThreshold: '工具連續失敗升級閾值（次）。相同工具+相同參數連續失敗達到此次數後，系統會以用戶身份轉發錯誤記錄畀模型，強制佢改變策略。範圍 1-5，預設 3。',
        // security extra
        allowPrivateIPs: '允许访问私有 IP 地址（如 192.168.x.x、10.x.x.x）。仅在内网开发环境中启用。',
        // resilience
        resilienceEnableFailover: '同一模型配置多次失敗時自動切換到下一個可用 provider。需要先配置多個 provider。',
        resilienceEnableTimeoutScaling: '連續超時時自動放寬 HTTP 請求嘅 ResponseHeaderTimeout，避免臨時網絡波動導致失敗。',
        resilienceMaxRetries: '每個請求嘅最大重試次數。設為 0 表示無上限（當無 failover 可用時會堅持無限重試）。',
        resilienceTimeoutScaleFactor: '每次超時後將 ResponseHeaderTimeout 乘以此倍率（例如 1.5 表示每次放寬 50%）。',
        resilienceMaxTimeoutSeconds: '超時放寬嘅絕對上限（秒）。達到此上限後不再繼續放寬。',
        resilienceInitialBackoffSeconds: '第一次重試前等待嘅秒數。之後會按退避倍率指數增長。',
        resilienceMaxBackoffSeconds: '重試間隔嘅上限（秒）。退避時間達到此值後不會再增加。',
        resilienceBackoffMultiplier: '每次重試後將等待間隔乘以此倍率（例如 2.0 表示每次翻倍）。',
        // prompt cache
        promptCacheEnabled: '啟用 Anthropic API Prompt 快取。加入 cache_control breakpoints 同 anthropic-version header，令靜態內容（system prompt、tools）可以被 API 快取。僅影響 Anthropic API。',
        promptCacheStableTools: '請求之間唔改變工具列表，保持 cache prefix 一致。啟用後會使用完整工具集（跳過 tier/sampling/density 過濾），Plan Mode 改用 message 標記而非物理刪除工具。需要啟用「Prompt 快取」先生效。',
};

export const SETTINGS_COLOR_MODES_CONFIG = [
        { value: ColorMode.SYSTEM, label: 'System', icon: Monitor },
        { value: ColorMode.LIGHT, label: 'Light', icon: Sun },
        { value: ColorMode.DARK, label: 'Dark', icon: Moon }
];
