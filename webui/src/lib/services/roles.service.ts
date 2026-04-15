import { apiFetch, apiPost } from '$lib/utils';

export interface Role {
	Name: string;
	DisplayName: string;
	Description: string;
	Icon: string;
	IsPreset: boolean;
	Tags: string[];
	Identity?: string;
	Personality?: string;
	SpeakingStyle?: string;
	Expertise?: string[];
	Guidelines?: string[];
	Forbidden?: string[];
	Skills?: string[];
}

export interface RolesListResponse {
	Roles: Role[];
}

export class RolesService {
	private baseUrl = '/api/roles';

	/**
	 * 列出所有人格
	 */
	async list(): Promise<RolesListResponse> {
		return apiFetch<RolesListResponse>(this.baseUrl);
	}

	/**
	 * 创建新人格
	 */
	async create(role: Omit<Role, 'IsPreset'>): Promise<{ message: string; name: string }> {
		return apiPost<{ message: string; name: string }>(this.baseUrl, role);
	}

	/**
	 * 获取人格详情
	 */
	async get(name: string): Promise<Role> {
		return apiFetch<Role>(`${this.baseUrl}/${name}`);
	}

	/**
	 * 更新人格
	 */
	async update(name: string, role: Partial<Role>): Promise<{ message: string }> {
		return apiPost<{ message: string }>(`${this.baseUrl}/${name}`, role, {
			method: 'PUT'
		});
	}

	/**
	 * 删除人格
	 */
	async delete(name: string): Promise<{ message: string }> {
		return apiPost<{ message: string }>(`${this.baseUrl}/${name}`, {}, {
			method: 'DELETE'
		});
	}
}

export const rolesService = new RolesService();
