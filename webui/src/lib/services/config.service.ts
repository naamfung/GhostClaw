/**
 * Config Service
 *
 * 处理配置相关的 API 请求
 */

export interface APIConfig {
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
		const response = await fetch(`${this.baseUrl}/config`, {
			method: 'GET',
			headers: {
				'Content-Type': 'application/json'
			}
		});

		if (!response.ok) {
			throw new Error(`获取配置失败: ${response.statusText}`);
		}

		return response.json();
	}

	/**
	 * 更新配置
	 */
	async updateConfig(config: ConfigUpdateRequest): Promise<{ message: string }> {
		const response = await fetch(`${this.baseUrl}/config`, {
			method: 'PUT',
			headers: {
				'Content-Type': 'application/json'
			},
			body: JSON.stringify(config)
		});

		if (!response.ok) {
			const error = await response.json();
			throw new Error(error.error || '更新配置失败');
		}

		return response.json();
	}
}

export const configService = new ConfigService();
