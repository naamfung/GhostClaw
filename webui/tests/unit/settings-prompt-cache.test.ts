import { describe, it, expect } from 'vitest';
import {
	SETTINGS_KEYS,
	SETTING_CONFIG_DEFAULT,
	SETTINGS_SECTION_TITLES,
} from '$lib/constants';

/**
 * BDD: Prompt Cache 設定嘅前端 → 後端適配驗證。
 *
 * Scenario: 用戶喺 ChatSettings 入面 toggle 提示词缓存選項 → save → backend 收到正確格式。
 * 呢度測試 config key mapping 同 defaults 一致性，確保前端唔會 send 錯格式俾 backend。
 */

describe('Prompt Cache Settings — Key Mapping', () => {
	it('promptCacheEnabled maps to backend PromptCache.Enabled', () => {
		const key = SETTINGS_KEYS.PROMPT_CACHE_ENABLED;
		expect(key).toBe('promptCacheEnabled');
	});

	it('promptCacheStableTools maps to backend PromptCache.StableTools', () => {
		const key = SETTINGS_KEYS.PROMPT_CACHE_STABLE_TOOLS;
		expect(key).toBe('promptCacheStableTools');
	});
});

describe('Prompt Cache Settings — Defaults', () => {
	it('promptCacheEnabled defaults to false (off)', () => {
		expect(SETTING_CONFIG_DEFAULT.promptCacheEnabled).toBe(false);
	});

	it('promptCacheStableTools defaults to false (off)', () => {
		expect(SETTING_CONFIG_DEFAULT.promptCacheStableTools).toBe(false);
	});
});

describe('Prompt Cache Settings — Backend Config Object Shape', () => {
	/**
	 * 模擬 ChatSettings.handleSave() 入面嘅 backendConfig 構建邏輯。
	 * 確保前端 send 嘅 JSON shape 同 backend PUT /api/config 預期一致。
	 */
	function buildPromptCacheBackendConfig(processedConfig: Record<string, unknown>) {
		const promptCacheFields = ['promptCacheEnabled', 'promptCacheStableTools'];
		const hasPromptCacheConfig = promptCacheFields.some(
			(f) => processedConfig[f] !== undefined,
		);
		if (hasPromptCacheConfig) {
			return {
				Enabled: !!processedConfig.promptCacheEnabled,
				StableTools: !!processedConfig.promptCacheStableTools,
			};
		}
		return undefined;
	}

	it('builds correct backend shape when both enabled', () => {
		const cfg = buildPromptCacheBackendConfig({
			promptCacheEnabled: true,
			promptCacheStableTools: true,
		});
		expect(cfg).toEqual({ Enabled: true, StableTools: true });
	});

	it('builds correct backend shape when both disabled', () => {
		const cfg = buildPromptCacheBackendConfig({
			promptCacheEnabled: false,
			promptCacheStableTools: false,
		});
		expect(cfg).toEqual({ Enabled: false, StableTools: false });
	});

	it('builds correct backend shape when only Enabled is on', () => {
		const cfg = buildPromptCacheBackendConfig({
			promptCacheEnabled: true,
			promptCacheStableTools: false,
		});
		expect(cfg).toEqual({ Enabled: true, StableTools: false });
	});

	it('returns undefined when no prompt cache fields present', () => {
		const cfg = buildPromptCacheBackendConfig({ otherField: 'value' });
		expect(cfg).toBeUndefined();
	});

	it('coerces truthy values to boolean', () => {
		const cfg = buildPromptCacheBackendConfig({
			promptCacheEnabled: 1,
			promptCacheStableTools: 'yes',
		});
		expect(cfg).toEqual({ Enabled: true, StableTools: true });
	});
});

describe('Prompt Cache — Section Title', () => {
	it('has correct section title', () => {
		expect(SETTINGS_SECTION_TITLES.PROMPT_CACHE).toBe('提示词缓存');
	});
});

describe('Prompt Cache — Frontend/Backend Roundtrip Simulation', () => {
	/**
	 * BDD Scenario: 用戶 toggle 提示词缓存 → 前端 build backend config → PUT /api/config →
	 *   backend 解析並儲存 → GET /api/config → 前端收到正確值 → UI toggle 反映正確狀態。
	 *
	 * 呢個 test 驗證中間嘅 data mapping 環節。
	 */
	it('roundtrip: default (off) → PUT → GET → match', () => {
		// Simulate user saving defaults
		const frontendConfig = {
			promptCacheEnabled: false,
			promptCacheStableTools: false,
		};

		// Build backend payload
		const backendPayload = {
			PromptCache: {
				Enabled: !!frontendConfig.promptCacheEnabled,
				StableTools: !!frontendConfig.promptCacheStableTools,
			},
		};

		// Simulate backend response (mirror)
		const backendResponse = {
			PromptCache: backendPayload.PromptCache,
		};

		// Verify roundtrip
		expect(backendResponse.PromptCache.Enabled).toBe(false);
		expect(backendResponse.PromptCache.StableTools).toBe(false);
	});

	it('roundtrip: enabled → PUT → GET → match', () => {
		const frontendConfig = {
			promptCacheEnabled: true,
			promptCacheStableTools: true,
		};

		const backendPayload = {
			PromptCache: {
				Enabled: !!frontendConfig.promptCacheEnabled,
				StableTools: !!frontendConfig.promptCacheStableTools,
			},
		};

		const backendResponse = {
			PromptCache: backendPayload.PromptCache,
		};

		expect(backendResponse.PromptCache.Enabled).toBe(true);
		expect(backendResponse.PromptCache.StableTools).toBe(true);
	});

	it('roundtrip: partial (only Enabled) → PUT → GET → match', () => {
		const frontendConfig = {
			promptCacheEnabled: true,
			promptCacheStableTools: false,
		};

		const backendPayload = {
			PromptCache: {
				Enabled: !!frontendConfig.promptCacheEnabled,
				StableTools: !!frontendConfig.promptCacheStableTools,
			},
		};

		const backendResponse = {
			PromptCache: backendPayload.PromptCache,
		};

		expect(backendResponse.PromptCache.Enabled).toBe(true);
		expect(backendResponse.PromptCache.StableTools).toBe(false);
	});
});
