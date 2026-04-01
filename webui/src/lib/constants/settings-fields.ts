/**
 * List of all numeric fields in settings configuration.
 * These fields will be converted from strings to numbers during save.
 */
export const NUMERIC_FIELDS = [
        'temperature',
        'max_tokens',
        'timeoutShell',
        'timeoutHttp',
        'timeoutPlugin',
        'timeoutBrowser'
] as const;

/**
 * Fields that must be positive integers (>= 1).
 * These will be clamped to minimum 1 and rounded during save.
 */
export const POSITIVE_INTEGER_FIELDS = ['timeoutShell', 'timeoutHttp', 'timeoutPlugin', 'timeoutBrowser'] as const;
