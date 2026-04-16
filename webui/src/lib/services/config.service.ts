import { apiFetch, apiPost } from '$lib/utils';

/**
 * Config Service
 *
 * 处理配置相关的 API 请求
 */

export interface APIConfig {
        Name: string;
        Description: string;
        APIType: string;
        BaseURL: string;
        APIKey: string;
        Model: string;
        Temperature: number;
        MaxTokens: number;
        Stream: boolean;
        Thinking: boolean;
        BlockDangerousCommands: boolean;
}

export interface TimeoutConfig {
        Shell: number;
        HTTP: number;
        Plugin: number;
        Browser: number;
}

export interface ConfigResponse {
        APIConfig: APIConfig;
        DefaultRole: string;
        NeedsSetup: boolean;
        Timeout: TimeoutConfig;
}

export interface ConfigUpdateRequest {
        APIConfig?: Partial<APIConfig>;
        DefaultRole?: string;
        Timeout?: Partial<TimeoutConfig>;
}

class ConfigService {
        private baseUrl = '/api';

        /**
         * 获取当前配置
         */
        async getConfig(): Promise<ConfigResponse> {
                return apiFetch<ConfigResponse>(`${this.baseUrl}/config`);
        }

        /**
         * 更新配置
         */
        async updateConfig(config: ConfigUpdateRequest): Promise<{ message: string }> {
                return apiPost<{ message: string }>(`${this.baseUrl}/config`, config, {
                        method: 'PUT'
                });
        }
}

export const configService = new ConfigService();
