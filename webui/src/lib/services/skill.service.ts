/**
 * Skill Service
 *
 * 处理技能管理相关的 API 请求
 */

export interface Skill {
	name: string;
	display_name: string;
	description: string;
	trigger_words?: string[];
	system_prompt: string;
	output_format?: string;
	examples?: string[];
	tags?: string[];
}

export interface SkillListItem {
	name: string;
	display_name: string;
	description: string;
	trigger_words?: string[];
	tags?: string[];
}

export interface SkillsListResponse {
	skills: SkillListItem[];
}

class SkillService {
	private baseUrl = '/api/skills';

	/**
	 * 获取所有技能列表
	 */
	async listSkills(): Promise<SkillsListResponse> {
		const response = await fetch(this.baseUrl, {
			method: 'GET',
			headers: {
				'Content-Type': 'application/json'
			}
		});

		if (!response.ok) {
			throw new Error(`获取技能列表失败: ${response.statusText}`);
		}

		return response.json();
	}

	/**
	 * 获取技能详情
	 */
	async getSkill(name: string): Promise<Skill> {
		const response = await fetch(`${this.baseUrl}/${encodeURIComponent(name)}`, {
			method: 'GET',
			headers: {
				'Content-Type': 'application/json'
			}
		});

		if (!response.ok) {
			throw new Error(`获取技能详情失败: ${response.statusText}`);
		}

		return response.json();
	}

	/**
	 * 创建技能
	 */
	async createSkill(skill: Partial<Skill>): Promise<{ message: string; name: string }> {
		const response = await fetch(this.baseUrl, {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json'
			},
			body: JSON.stringify(skill)
		});

		if (!response.ok) {
			const error = await response.json();
			throw new Error(error.error || '创建技能失败');
		}

		return response.json();
	}

	/**
	 * 更新技能
	 */
	async updateSkill(name: string, skill: Partial<Skill>): Promise<{ message: string }> {
		const response = await fetch(`${this.baseUrl}/${encodeURIComponent(name)}`, {
			method: 'PUT',
			headers: {
				'Content-Type': 'application/json'
			},
			body: JSON.stringify(skill)
		});

		if (!response.ok) {
			const error = await response.json();
			throw new Error(error.error || '更新技能失败');
		}

		return response.json();
	}

	/**
	 * 删除技能
	 */
	async deleteSkill(name: string): Promise<{ message: string }> {
		const response = await fetch(`${this.baseUrl}/${encodeURIComponent(name)}`, {
			method: 'DELETE',
			headers: {
				'Content-Type': 'application/json'
			}
		});

		if (!response.ok) {
			const error = await response.json();
			throw new Error(error.error || '删除技能失败');
		}

		return response.json();
	}
}

export const skillService = new SkillService();
