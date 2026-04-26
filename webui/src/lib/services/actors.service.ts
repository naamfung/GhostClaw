import { apiFetch, apiPost } from '$lib/utils';

export interface Actor {
	Name: string;
	Role: string;
	Model: string;
	CharacterName: string;
	CharacterBackground: string;
	Description: string;
	IsDefault: boolean;
}

export interface ActorsListResponse {
	Actors: Actor[];
}

export class ActorsService {
	private baseUrl = '/api/actors';

	/**
	 * 列出所有演员
	 */
	async list(): Promise<ActorsListResponse> {
		return apiFetch<ActorsListResponse>(this.baseUrl);
	}

	/**
	 * 创建新演员
	 */
	async create(actor: Omit<Actor, 'IsDefault'>): Promise<{ message: string; name: string }> {
		return apiPost<{ message: string; name: string }>(this.baseUrl, actor);
	}

	/**
	 * 获取演员详情
	 */
	async get(name: string): Promise<Actor> {
		return apiFetch<Actor>(`${this.baseUrl}/${name}`);
	}

	/**
	 * 更新演员
	 */
	async update(name: string, actor: Partial<Actor>): Promise<{ message: string }> {
		return apiPost<{ message: string }>(`${this.baseUrl}/${name}`, actor, {
			method: 'PUT'
		});
	}

	/**
	 * 删除演员
	 */
	async delete(name: string): Promise<{ message: string }> {
		return apiPost<{ message: string }>(`${this.baseUrl}/${name}`, {}, {
			method: 'DELETE'
		});
	}

	/**
	 * 设置默认演员
	 */
	async setDefault(name: string): Promise<{ message: string }> {
		return apiPost<{ message: string }>(`${this.baseUrl}/${name}/set-default`, {}, {
			method: 'POST'
		});
	}
}

export const actorsService = new ActorsService();
