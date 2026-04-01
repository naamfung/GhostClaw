<script lang="ts">
        import { Button } from '$lib/components/ui/button';
        import { Input } from '$lib/components/ui/input';
        import { ScrollArea } from '$lib/components/ui/scroll-area';
        import { Badge } from '$lib/components/ui/badge';
        import RoleEditor from './RoleEditor.svelte';
        import { DialogConfirmation } from '$lib/components/app/dialogs';
        import { onMount } from 'svelte';
        import { Pencil, Trash2, Plus, Search, User, Star, ChevronLeft } from '@lucide/svelte';

        interface RoleListItem {
                Name: string;
                DisplayName: string;
                Description: string;
                Icon?: string;
                IsPreset: boolean;
                Tags?: string[];
        }

        let roles = $state<RoleListItem[]>([]);
        let filteredRoles = $state<RoleListItem[]>([]);
        let searchQuery = $state('');
        let isLoading = $state(false);
        let selectedRole = $state<RoleListItem | null>(null);
        let showEditor = $state(false);
        let editingRole = $state<RoleListItem | null>(null);
        let showDeleteConfirm = $state(false);
        let roleToDelete = $state<RoleListItem | null>(null);
        let defaultRole = $state<string>('');
        let isSettingDefault = $state(false);

        async function loadRoles() {
                isLoading = true;
                try {
                        const response = await fetch('/api/roles');
                        if (response.ok) {
                                const data = await response.json();
                                roles = data.Roles || [];
                                filterRoles();
                        }
                } catch (error) {
                        console.error('加载角色列表失败:', error);
                } finally {
                        isLoading = false;
                }
        }

        async function loadDefaultRole() {
                try {
                        const response = await fetch('/api/config');
                        if (response.ok) {
                                const data = await response.json();
                                defaultRole = data.DefaultRole || '';
                        }
                } catch (error) {
                        console.error('加载默认角色设置失败:', error);
                }
        }

        function filterRoles() {
                if (!searchQuery.trim()) {
                        filteredRoles = roles;
                } else {
                        const query = searchQuery.toLowerCase();
                        filteredRoles = roles.filter(
                                (p) =>
                                        p.Name.toLowerCase().includes(query) ||
                                        p.DisplayName.toLowerCase().includes(query) ||
                                        p.Description.toLowerCase().includes(query)
                        );
                }
        }

        function handleCreate() {
                editingRole = null;
                showEditor = true;
        }

        function handleEdit(role: RoleListItem) {
                editingRole = role;
                showEditor = true;
        }

        function handleDeleteClick(role: RoleListItem) {
                if (role.IsPreset) return;
                roleToDelete = role;
                showDeleteConfirm = true;
        }

        async function confirmDelete() {
                if (!roleToDelete) return;

                try {
                        const response = await fetch(`/api/roles/${encodeURIComponent(roleToDelete.Name)}`, {
                                method: 'DELETE'
                        });

                        if (response.ok) {
                                roles = roles.filter((p) => p.Name !== roleToDelete.Name);
                                filterRoles();
                                if (selectedRole?.Name === roleToDelete.Name) {
                                        selectedRole = null;
                                }
                        } else {
                                const error = await response.json();
                                alert(error.error || '删除失败');
                        }
                } catch (error) {
                        console.error('删除角色失败:', error);
                        alert('删除角色时发生错误');
                } finally {
                        showDeleteConfirm = false;
                        roleToDelete = null;
                }
        }

        async function setAsDefaultRole(role: RoleListItem) {
                isSettingDefault = true;
                try {
                        const response = await fetch('/api/config', {
                                method: 'PUT',
                                headers: { 'Content-Type': 'application/json' },
                                body: JSON.stringify({ DefaultRole: role.Name })
                        });

                        if (response.ok) {
                                defaultRole = role.Name;
                        } else {
                                const error = await response.json();
                                alert(error.error || '设置失败');
                        }
                } catch (error) {
                        console.error('设置默认角色失败:', error);
                        alert('设置默认角色时发生错误');
                } finally {
                        isSettingDefault = false;
                }
        }

        async function clearDefaultRole() {
                isSettingDefault = true;
                try {
                        const response = await fetch('/api/config', {
                                method: 'PUT',
                                headers: { 'Content-Type': 'application/json' },
                                body: JSON.stringify({ DefaultRole: '' })
                        });

                        if (response.ok) {
                                defaultRole = '';
                        }
                } catch (error) {
                        console.error('清除默认角色失败:', error);
                } finally {
                        isSettingDefault = false;
                }
        }

        function handleEditorSave() {
                showEditor = false;
                editingRole = null;
                loadRoles();
        }

        function handleEditorCancel() {
                showEditor = false;
                editingRole = null;
        }

        // 使用 $effect 监听搜索查询变化
        $effect(() => {
                searchQuery;
                filterRoles();
        });

        onMount(() => {
                loadRoles();
                loadDefaultRole();
        });
</script>

<div class="flex h-full flex-col">
        {#if showEditor}
                <RoleEditor
                        role={editingRole}
                        onsave={handleEditorSave}
                        oncancel={handleEditorCancel}
                />
        {:else}
                <!-- 搜索栏和创建按钮 -->
                <div class="mb-4 flex items-center gap-2">
                        <div class="relative flex-1">
                                <Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                                <Input
                                        type="text"
                                        placeholder="搜索角色..."
                                        class="pl-9"
                                        bind:value={searchQuery}
                                />
                        </div>
                        <Button onclick={handleCreate}>
                                <Plus class="mr-1 h-4 w-4" />
                                新建
                        </Button>
                </div>

                <!-- 默认人格提示 -->
                {#if defaultRole}
                        <div class="mb-3 flex items-center justify-between rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2">
                                <div class="flex items-center gap-2 text-sm">
                                        <Star class="h-4 w-4 text-amber-500" />
                                        <span>默认角色：<strong>{roles.find(p => p.Name === defaultRole)?.DisplayName || defaultRole}</strong></span>
                                </div>
                                <Button variant="ghost" size="sm" onclick={clearDefaultRole} disabled={isSettingDefault}>
                                        清除
                                </Button>
                        </div>
                {/if}

                <!-- 人格列表 -->
                <div class="flex flex-1 gap-4 overflow-hidden md:flex-row flex-col">
                        <!-- 左侧列表 -->
                        <ScrollArea class="h-full w-full md:w-56 md:flex-shrink-0 border rounded-lg {selectedRole ? 'hidden md:block' : ''}">
                                {#if isLoading}
                                        <div class="flex items-center justify-center p-4">
                                                <span class="text-muted-foreground">加载中...</span>
                                        </div>
                                {:else if filteredRoles.length === 0}
                                        <div class="flex flex-col items-center justify-center p-8 text-center">
                                                <User class="mb-2 h-8 w-8 text-muted-foreground" />
                                                <p class="text-muted-foreground">没有找到角色</p>
                                        </div>
                                {:else}
                                        <div class="space-y-1 p-2">
                                                {#each filteredRoles as role (role.Name)}
                                                        <button
                                                                class="flex w-full cursor-pointer items-center gap-3 rounded-lg p-3 text-left transition-colors hover:bg-accent {selectedRole?.Name ===
                                                                role.Name
                                                                        ? 'bg-accent'
                                                                        : ''}"
                                                                onclick={() => (selectedRole = role)}
                                                        >
                                                                <span class="text-2xl">{role.Icon || '👤'}</span>
                                                                <div class="flex-1 overflow-hidden">
                                                                        <div class="flex items-center gap-2">
                                                                                <span class="truncate font-medium">{role.DisplayName}</span>
                                                                                {#if role.Name === defaultRole}
                                                                                        <Star class="h-3 w-3 text-amber-500" />
                                                                                {/if}
                                                                                {#if role.IsPreset}
                                                                                        <Badge variant="secondary" class="text-xs">预设</Badge>
                                                                                {/if}
                                                                        </div>
                                                                        <p class="truncate text-xs text-muted-foreground">{role.Description}</p>
                                                                </div>
                                                        </button>
                                                {/each}
                                        </div>
                                {/if}
                        </ScrollArea>

                        <!-- 右侧详情 -->
                        <div class="min-w-0 flex-1 overflow-hidden rounded-lg border {selectedRole ? '' : 'hidden md:block'}">
                                {#if selectedRole}
                                        {#key selectedRole.Name}
                                                <div class="flex h-full flex-col">
                                                <!-- 头部 -->
                                                <div class="flex flex-col gap-3 border-b p-4">
                                                        <!-- 移动端返回按钮 -->
                                                        <button
                                                                class="md:hidden flex items-center gap-1 text-sm text-muted-foreground mb-2 self-start"
                                                                onclick={() => (selectedRole = null)}
                                                        >
                                                                <ChevronLeft class="h-4 w-4" />
                                                                返回列表
                                                        </button>
                                                        <div class="flex items-center gap-3 min-w-0">
                                                                <span class="text-3xl flex-shrink-0">{selectedRole.Icon || '👤'}</span>
                                                                <div class="min-w-0 flex-1">
                                                                        <h3 class="font-semibold">
                                                                                {selectedRole.DisplayName}
                                                                                {#if selectedRole.Name === defaultRole}
                                                                                        <Star class="ml-1 inline h-4 w-4 text-amber-500" />
                                                                                {/if}
                                                                        </h3>
                                                                        <p class="truncate text-sm text-muted-foreground" title={selectedRole.Name}>{selectedRole.Name}</p>
                                                                </div>
                                                        </div>
                                                        <div class="flex flex-wrap gap-2">
                                                                <!-- 设为默认角色按钮 -->
                                                                {#if selectedRole.Name !== defaultRole}
                                                                        <Button 
                                                                                variant="outline" 
                                                                                size="sm" 
                                                                                onclick={() => setAsDefaultRole(selectedRole)}
                                                                                disabled={isSettingDefault}
                                                                        >
                                                                                <Star class="mr-1 h-4 w-4" />
                                                                                设为默认
                                                                        </Button>
                                                                {/if}
                                                                <Button variant="outline" size="sm" onclick={() => handleEdit(selectedRole)}>
                                                                        <Pencil class="mr-1 h-4 w-4" />
                                                                        编辑
                                                                </Button>
                                                                {#if !selectedRole.IsPreset}
                                                                        <Button
                                                                                variant="outline"
                                                                                size="sm"
                                                                                class="text-destructive hover:bg-destructive hover:text-destructive-foreground"
                                                                                onclick={() => handleDeleteClick(selectedRole)}
                                                                        >
                                                                                <Trash2 class="mr-1 h-4 w-4" />
                                                                                删除
                                                                        </Button>
                                                                {/if}
                                                        </div>
                                                </div>

                                                <!-- 描述 -->
                                                <ScrollArea class="flex-1 p-4">
                                                        <p class="text-sm">{selectedRole.Description}</p>

                                                        {#if selectedRole.Tags && selectedRole.Tags.length > 0}
                                                                <div class="mt-4">
                                                                        <h4 class="mb-2 text-sm font-medium">标签</h4>
                                                                        <div class="flex flex-wrap gap-2">
                                                                                {#each selectedRole.Tags as tag (tag)}
                                                                                        <Badge variant="outline">{tag}</Badge>
                                                                                {/each}
                                                                        </div>
                                                                </div>
                                                        {/if}
                                                </ScrollArea>
                                                </div>
                                        {/key}
                                {:else}
                                        <div class="flex h-full flex-col items-center justify-center text-center">
                                                <User class="mb-2 h-12 w-12 text-muted-foreground" />
                                                <p class="text-muted-foreground">选择一个角色查看详情</p>
                                        </div>
                                {/if}
                        </div>
                </div>
        {/if}
</div>

<DialogConfirmation
        open={showDeleteConfirm}
        title="确认删除"
        description="确定要删除角色「{roleToDelete?.DisplayName}」吗？此操作无法撤销。"
        confirmText="删除"
        onconfirm={confirmDelete}
        oncancel={() => {
                showDeleteConfirm = false;
                roleToDelete = null;
        }}
/>
