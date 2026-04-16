import { apiFetch, apiPost } from '$lib/utils';

export interface Skill {
	Name: string;
	DisplayName: string;
	Description: string;
	TriggerWords: string[];
	Tags: string[];
	SystemPrompt?: string;
	OutputFormat?: string;
	Examples?: string[];
}

export interface SkillsListResponse {
	Skills: Skill[];
}

export class SkillsService {
	private baseUrl = '/api/skills';

	/**
	 * 列出所有技能
	 */
	async list(): Promise<SkillsListResponse> {
		return apiFetch<SkillsListResponse>(this.baseUrl);
	}

	/**
	 * 创建新技能
	 */
	async create(skill: Skill): Promise<{ message: string; name: string }> {
		return apiPost<{ message: string; name: string }>(this.baseUrl, skill);
	}

	/**
	 * 获取技能详情
	 */
	async get(name: string): Promise<Skill> {
		return apiFetch<Skill>(`${this.baseUrl}/${name}`);
	}

	/**
	 * 更新技能
	 */
	async update(name: string, skill: Partial<Skill>): Promise<{ message: string }> {
		return apiPost<{ message: string }>(`${this.baseUrl}/${name}`, skill, {
			method: 'PUT'
		});
	}

	/**
	 * 删除技能
	 */
	async delete(name: string): Promise<{ message: string }> {
		return apiPost<{ message: string }>(`${this.baseUrl}/${name}`, {}, {
			method: 'DELETE'
		});
	}
}

export const skillsService = new SkillsService();
