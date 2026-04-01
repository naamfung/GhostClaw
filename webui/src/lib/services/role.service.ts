/**
 * Role Service
 *
 * 处理角色管理相关的 API 请求
 */

export interface Role {
        name: string;
        display_name: string;
        description: string;
        icon?: string;
        identity?: string;
        personality?: string;
        speaking_style?: string;
        expertise?: string[];
        guidelines?: string[];
        forbidden?: string[];
        skills?: string[];
        tags?: string[];
        is_preset: boolean;
        examples?: Array<{
                user: string;
                assistant: string;
                context?: string;
        }>;
        tool_permission?: {
                mode: string;
                allowed_tools?: string[];
                denied_tools?: string[];
        };
}

export interface RoleListItem {
        name: string;
        display_name: string;
        description: string;
        icon?: string;
        is_preset: boolean;
        tags?: string[];
}

export interface RolesListResponse {
        roles: RoleListItem[];
}

class RoleService {
        private baseUrl = '/api/roles';

        /**
         * 获取所有角色列表
         */
        async listRoles(): Promise<RolesListResponse> {
                const response = await fetch(this.baseUrl, {
                        method: 'GET',
                        headers: {
                                'Content-Type': 'application/json'
                        }
                });

                if (!response.ok) {
                        throw new Error(`获取角色列表失败: ${response.statusText}`);
                }

                return response.json();
        }

        /**
         * 获取角色详情
         */
        async getRole(name: string): Promise<Role> {
                const response = await fetch(`${this.baseUrl}/${encodeURIComponent(name)}`, {
                        method: 'GET',
                        headers: {
                                'Content-Type': 'application/json'
                        }
                });

                if (!response.ok) {
                        throw new Error(`获取角色详情失败: ${response.statusText}`);
                }

                return response.json();
        }

        /**
         * 创建角色
         */
        async createRole(role: Partial<Role>): Promise<{ message: string; name: string }> {
                const response = await fetch(this.baseUrl, {
                        method: 'POST',
                        headers: {
                                'Content-Type': 'application/json'
                        },
                        body: JSON.stringify(role)
                });

                if (!response.ok) {
                        const error = await response.json();
                        throw new Error(error.error || '创建角色失败');
                }

                return response.json();
        }

        /**
         * 更新角色
         */
        async updateRole(name: string, role: Partial<Role>): Promise<{ message: string }> {
                const response = await fetch(`${this.baseUrl}/${encodeURIComponent(name)}`, {
                        method: 'PUT',
                        headers: {
                                'Content-Type': 'application/json'
                        },
                        body: JSON.stringify(role)
                });

                if (!response.ok) {
                        const error = await response.json();
                        throw new Error(error.error || '更新角色失败');
                }

                return response.json();
        }

        /**
         * 删除角色
         */
        async deleteRole(name: string): Promise<{ message: string }> {
                const response = await fetch(`${this.baseUrl}/${encodeURIComponent(name)}`, {
                        method: 'DELETE',
                        headers: {
                                'Content-Type': 'application/json'
                        }
                });

                if (!response.ok) {
                        const error = await response.json();
                        throw new Error(error.error || '删除角色失败');
                }

                return response.json();
        }
}

export const roleService = new RoleService();
