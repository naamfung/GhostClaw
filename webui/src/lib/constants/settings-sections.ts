/**
 * Settings section titles constants for ChatSettings component.
 *
 * These titles define the navigation sections in the settings dialog.
 * Used for both sidebar navigation and mobile horizontal scroll menu.
 */
export const SETTINGS_SECTION_TITLES = {
        MODEL: '模型管理',
        GENERAL: '常规',
        DISPLAY: '显示',
        ROLES: '角色管理',
        ACTORS: '演员管理',
        SKILLS: '技能管理',
        SECURITY: '安全',
        BROWSER: '浏览器',
        WORKFLOW: '工作模式',
        TIMEOUT: '超时配置',
        IMPORT_EXPORT: '导入/导出',
        MCP: 'MCP 服务'
} as const;

/** Type for settings section titles */
export type SettingsSectionTitle =
        (typeof SETTINGS_SECTION_TITLES)[keyof typeof SETTINGS_SECTION_TITLES];
