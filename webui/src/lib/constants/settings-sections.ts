/**
 * Settings section titles constants for ChatSettings component.
 *
 * These titles define the navigation sections in the settings dialog.
 * Used for both sidebar navigation and mobile horizontal scroll menu.
 */
export const SETTINGS_SECTION_TITLES = {
        GENERAL: '常规',
        DISPLAY: '显示',
        MODEL: '模型管理',
        SAMPLING: '采样',
        PENALTIES: '惩罚',
        ROLES: '角色管理',
        ACTORS: '演员管理',
        SKILLS: '技能管理',
        IMPORT_EXPORT: '导入/导出',
        MCP: 'MCP 服务',
        TIMEOUT: '超时配置',
        DEVELOPER: '开发者'
} as const;

/** Type for settings section titles */
export type SettingsSectionTitle =
        (typeof SETTINGS_SECTION_TITLES)[keyof typeof SETTINGS_SECTION_TITLES];
