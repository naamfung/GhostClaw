<script lang="ts">
        import { Button } from '$lib/components/ui/button';
        import { Input } from '$lib/components/ui/input';
        import { Label } from '$lib/components/ui/label';
        import { ScrollArea } from '$lib/components/ui/scroll-area';
        import { Badge } from '$lib/components/ui/badge';
        import { Textarea } from '$lib/components/ui/textarea';
        import { DialogConfirmation } from '$lib/components/app/dialogs';
        import { onMount } from 'svelte';
        import { Pencil, Trash2, Plus, Search, Users, Star, ChevronLeft } from '@lucide/svelte';

        interface Actor {
                Name: string;
                Role: string;
                Model: string;
                CharacterName: string;
                CharacterBackground: string;
                Description: string;
                IsDefault: boolean;
        }

        interface RoleInfo {
                Name: string;
                DisplayName: string;
        }

        interface ModelInfo {
                Name: string;
                Model: string;
                Description: string;
        }

        let actors = $state<Actor[]>([]);
        let filteredActors = $state<Actor[]>([]);
        let roles = $state<RoleInfo[]>([]);
        let models = $state<ModelInfo[]>([]);
        let searchQuery = $state('');
        let isLoading = $state(false);
        let selectedActor = $state<Actor | null>(null);
        let showEditor = $state(false);
        let editingActor = $state<Actor | null>(null);
        let showDeleteConfirm = $state(false);
        let actorToDelete = $state<Actor | null>(null);
        let isSettingDefault = $state(false);

        // 编辑表单字段
        let editForm = $state({
                Name: '',
                Role: '',
                Model: 'main',
                CharacterName: '',
                CharacterBackground: '',
                Description: ''
        });

        async function loadActors() {
                isLoading = true;
                try {
                        const response = await fetch('/api/actors');
                        if (response.ok) {
                                const data = await response.json();
                                actors = data.Actors || [];
                                filterActors();
                        }
                } catch (error) {
                        console.error('加载演员列表失败:', error);
                } finally {
                        isLoading = false;
                }
        }

        async function loadRoles() {
                try {
                        const response = await fetch('/api/roles');
                        if (response.ok) {
                                const data = await response.json();
                                roles = data.Roles || [];
                        }
                } catch (error) {
                        console.error('加载角色列表失败:', error);
                }
        }

        async function loadModels() {
                try {
                        const response = await fetch('/api/models');
                        if (response.ok) {
                                const data = await response.json();
                                models = data.Models || [];
                        }
                } catch (error) {
                        console.error('加载模型列表失败:', error);
                }
        }

        function filterActors() {
                if (!searchQuery.trim()) {
                        filteredActors = actors;
                } else {
                        const query = searchQuery.toLowerCase();
                        filteredActors = actors.filter(
                                (a) =>
                                        a.Name.toLowerCase().includes(query) ||
                                        a.CharacterName.toLowerCase().includes(query) ||
                                        a.Description.toLowerCase().includes(query)
                        );
                }
        }

        function handleCreate() {
                editingActor = null;
                editForm = {
                        Name: '',
                        Role: roles[0]?.Name || 'coder',
                        Model: 'main',
                        CharacterName: '',
                        CharacterBackground: '',
                        Description: ''
                };
                showEditor = true;
        }

        function handleEdit(actor: Actor) {
                editingActor = actor;
                editForm = {
                        Name: actor.Name,
                        Role: actor.Role,
                        Model: actor.Model || 'main',
                        CharacterName: actor.CharacterName,
                        CharacterBackground: actor.CharacterBackground,
                        Description: actor.Description
                };
                showEditor = true;
        }

        function handleDeleteClick(actor: Actor) {
                if (actor.IsDefault) return;
                actorToDelete = actor;
                showDeleteConfirm = true;
        }

        async function confirmDelete() {
                if (!actorToDelete) return;

                try {
                        const response = await fetch(`/api/actors/${encodeURIComponent(actorToDelete.Name)}`, {
                                method: 'DELETE'
                        });

                        if (response.ok) {
                                actors = actors.filter((a) => a.Name !== actorToDelete.Name);
                                filterActors();
                                if (selectedActor?.Name === actorToDelete.Name) {
                                        selectedActor = null;
                                }
                        } else {
                                const error = await response.json();
                                alert(error.error || '删除失败');
                        }
                } catch (error) {
                        console.error('删除演员失败:', error);
                        alert('删除演员时发生错误');
                } finally {
                        showDeleteConfirm = false;
                        actorToDelete = null;
                }
        }

        async function setAsDefaultActor(actor: Actor) {
                isSettingDefault = true;
                try {
                        const response = await fetch(`/api/actors/${encodeURIComponent(actor.Name)}/set-default`, {
                                method: 'POST'
                        });

                        if (response.ok) {
                                // 更新本地状态
                                actors = actors.map(a => ({
                                        ...a,
                                        IsDefault: a.Name === actor.Name
                                }));
                                if (selectedActor) {
                                        selectedActor = actors.find(a => a.Name === selectedActor.Name) || null;
                                }
                        } else {
                                const error = await response.json();
                                alert(error.error || '设置失败');
                        }
                } catch (error) {
                        console.error('设置默认演员失败:', error);
                        alert('设置默认演员时发生错误');
                } finally {
                        isSettingDefault = false;
                }
        }

        async function handleEditorSave() {
                if (!editForm.Name.trim()) {
                        alert('演员名称不能为空');
                        return;
                }

                try {
                        let response;
                        if (editingActor) {
                                // 更新
                                response = await fetch(`/api/actors/${encodeURIComponent(editingActor.Name)}`, {
                                        method: 'PUT',
                                        headers: { 'Content-Type': 'application/json' },
                                        body: JSON.stringify(editForm)
                                });
                        } else {
                                // 创建
                                response = await fetch('/api/actors', {
                                        method: 'POST',
                                        headers: { 'Content-Type': 'application/json' },
                                        body: JSON.stringify(editForm)
                                });
                        }

                        if (response.ok) {
                                showEditor = false;
                                editingActor = null;
                                loadActors();
                        } else {
                                const error = await response.json();
                                alert(error.error || '保存失败');
                        }
                } catch (error) {
                        console.error('保存演员失败:', error);
                        alert('保存演员时发生错误');
                }
        }

        function handleEditorCancel() {
                showEditor = false;
                editingActor = null;
        }

        $effect(() => {
                searchQuery;
                filterActors();
        });

        onMount(() => {
                loadActors();
                loadRoles();
                loadModels();
        });
</script>

<div class="flex h-full flex-col">
        {#if showEditor}
                <!-- 编辑器 -->
                <div class="flex h-full flex-col">
                        <div class="mb-4 flex items-center justify-between border-b pb-3">
                                <h3 class="font-semibold">{editingActor ? '编辑演员' : '新建演员'}</h3>
                        </div>

                        <ScrollArea class="flex-1 pr-4">
                                <div class="space-y-4">
                                        <div class="space-y-2">
                                                <Label for="name">演员名称</Label>
                                                <Input
                                                        id="name"
                                                        bind:value={editForm.Name}
                                                        placeholder="如：hero_lin"
                                                        disabled={!!editingActor}
                                                />
                                                <p class="text-xs text-muted-foreground">唯一标识符，创建后不可修改</p>
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="role">角色模板</Label>
                                                <select
                                                        id="role"
                                                        class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                                                        bind:value={editForm.Role}
                                                >
                                                        {#each roles as r (r.Name)}
                                                                <option value={r.Name}>{r.DisplayName}</option>
                                                        {/each}
                                                </select>
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="model">模型配置</Label>
                                                <select
                                                        id="model"
                                                        class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                                                        bind:value={editForm.Model}
                                                >
                                                        {#each models as m (m.Name)}
                                                                <option value={m.Name}>{m.Name} ({m.Model})</option>
                                                        {/each}
                                                        {#if models.length === 0}
                                                                <option value="main">main</option>
                                                        {/if}
                                                </select>
                                                <p class="text-xs text-muted-foreground">选择演员使用的模型配置</p>
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="character_name">角色名</Label>
                                                <Input
                                                        id="character_name"
                                                        bind:value={editForm.CharacterName}
                                                        placeholder="如：林风"
                                                />
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="character_background">角色背景</Label>
                                                <Textarea
                                                        id="character_background"
                                                        bind:value={editForm.CharacterBackground}
                                                        placeholder="角色的背景故事..."
                                                        rows={4}
                                                />
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="description">描述</Label>
                                                <Textarea
                                                        id="description"
                                                        bind:value={editForm.Description}
                                                        placeholder="演员的简短描述..."
                                                        rows={2}
                                                />
                                        </div>
                                </div>
                        </ScrollArea>

                        <div class="mt-4 flex justify-end gap-2 border-t pt-4">
                                <Button variant="outline" onclick={handleEditorCancel}>取消</Button>
                                <Button onclick={handleEditorSave}>保存</Button>
                        </div>
                </div>
        {:else}
                <!-- 搜索栏和创建按钮 -->
                <div class="mb-4 flex items-center gap-2">
                        <div class="relative flex-1">
                                <Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                                <Input
                                        type="text"
                                        placeholder="搜索演员..."
                                        class="pl-9"
                                        bind:value={searchQuery}
                                />
                        </div>
                        <Button onclick={handleCreate}>
                                <Plus class="mr-1 h-4 w-4" />
                                新建
                        </Button>
                </div>

                <!-- 演员列表 -->
                <div class="flex flex-1 gap-4 overflow-hidden md:flex-row flex-col">
                        <!-- 左侧列表 -->
                        <ScrollArea class="h-full w-full md:w-56 md:flex-shrink-0 border rounded-lg {selectedActor ? 'hidden md:block' : ''}">
                                {#if isLoading}
                                        <div class="flex items-center justify-center p-4">
                                                <span class="text-muted-foreground">加载中...</span>
                                        </div>
                                {:else if filteredActors.length === 0}
                                        <div class="flex flex-col items-center justify-center p-8 text-center">
                                                <Users class="mb-2 h-8 w-8 text-muted-foreground" />
                                                <p class="text-muted-foreground">没有找到演员</p>
                                        </div>
                                {:else}
                                        <div class="space-y-1 p-2">
                                                {#each filteredActors as actor (actor.Name)}
                                                        <button
                                                                class="flex w-full cursor-pointer items-center gap-3 rounded-lg p-3 text-left transition-colors hover:bg-accent {selectedActor?.Name ===
                                                                actor.Name
                                                                        ? 'bg-accent'
                                                                        : ''}"
                                                                onclick={() => (selectedActor = actor)}
                                                        >
                                                                <div class="flex-1 overflow-hidden">
                                                                        <div class="flex items-center gap-2">
                                                                                <span class="truncate font-medium">{actor.CharacterName || actor.Name}</span>
                                                                                {#if actor.IsDefault}
                                                                                        <Star class="h-3 w-3 text-amber-500" />
                                                                                {/if}
                                                                        </div>
                                                                        <p class="truncate text-xs text-muted-foreground">{actor.Description || '无描述'}</p>
                                                                </div>
                                                        </button>
                                                {/each}
                                        </div>
                                {/if}
                        </ScrollArea>

                        <!-- 右侧详情 -->
                        <div class="min-w-0 flex-1 overflow-hidden rounded-lg border {selectedActor ? '' : 'hidden md:block'}">
                                {#if selectedActor}
                                        {#key selectedActor.Name}
                                                <div class="flex h-full flex-col">
                                                <!-- 头部 -->
                                                <div class="flex flex-col gap-3 border-b p-4">
                                                        <!-- 移动端返回按钮 -->
                                                        <button
                                                                class="md:hidden flex items-center gap-1 text-sm text-muted-foreground mb-2 self-start"
                                                                onclick={() => (selectedActor = null)}
                                                        >
                                                                <ChevronLeft class="h-4 w-4" />
                                                                返回列表
                                                        </button>
                                                        <div class="flex items-center gap-3 min-w-0">
                                                                <div class="min-w-0 flex-1">
                                                                        <h3 class="font-semibold">
                                                                                {selectedActor.CharacterName || selectedActor.Name}
                                                                                {#if selectedActor.IsDefault}
                                                                                        <Star class="ml-1 inline h-4 w-4 text-amber-500" />
                                                                                {/if}
                                                                        </h3>
                                                                        <p class="truncate text-sm text-muted-foreground" title={selectedActor.Name}>{selectedActor.Name}</p>
                                                                </div>
                                                        </div>
                                                        <div class="flex flex-wrap gap-2">
                                                                {#if !selectedActor.IsDefault}
                                                                        <Button
                                                                                variant="outline"
                                                                                size="sm"
                                                                                onclick={() => setAsDefaultActor(selectedActor)}
                                                                                disabled={isSettingDefault}
                                                                        >
                                                                                <Star class="mr-1 h-4 w-4" />
                                                                                设为默认
                                                                        </Button>
                                                                {/if}
                                                                <Button variant="outline" size="sm" onclick={() => handleEdit(selectedActor)}>
                                                                        <Pencil class="mr-1 h-4 w-4" />
                                                                        编辑
                                                                </Button>
                                                                {#if !selectedActor.IsDefault}
                                                                        <Button
                                                                                variant="outline"
                                                                                size="sm"
                                                                                class="text-destructive hover:bg-destructive hover:text-destructive-foreground"
                                                                                onclick={() => handleDeleteClick(selectedActor)}
                                                                        >
                                                                                <Trash2 class="mr-1 h-4 w-4" />
                                                                                删除
                                                                        </Button>
                                                                {/if}
                                                        </div>
                                                </div>

                                                <!-- 详情内容 -->
                                                <ScrollArea class="flex-1 p-4">
                                                        <div class="space-y-4">
                                                                <div>
                                                                        <h4 class="mb-1 text-xs font-medium text-muted-foreground">角色模板</h4>
                                                                        <Badge variant="secondary">{selectedActor.Role}</Badge>
                                                                </div>

                                                                <div>
                                                                        <h4 class="mb-1 text-xs font-medium text-muted-foreground">模型配置</h4>
                                                                        <Badge variant="outline">{selectedActor.Model}</Badge>
                                                                </div>

                                                                {#if selectedActor.Description}
                                                                        <div>
                                                                                <h4 class="mb-1 text-xs font-medium text-muted-foreground">描述</h4>
                                                                                <p class="text-sm">{selectedActor.Description}</p>
                                                                        </div>
                                                                {/if}

                                                                {#if selectedActor.CharacterName}
                                                                        <div>
                                                                                <h4 class="mb-1 text-xs font-medium text-muted-foreground">角色名</h4>
                                                                                <p class="text-sm">{selectedActor.CharacterName}</p>
                                                                        </div>
                                                                {/if}

                                                                {#if selectedActor.CharacterBackground}
                                                                        <div>
                                                                                <h4 class="mb-1 text-xs font-medium text-muted-foreground">角色背景</h4>
                                                                                <p class="text-sm whitespace-pre-wrap">{selectedActor.CharacterBackground}</p>
                                                                        </div>
                                                                {/if}
                                                        </div>
                                                </ScrollArea>
                                                </div>
                                        {/key}
                                {:else}
                                        <div class="flex h-full flex-col items-center justify-center text-center">
                                                <Users class="mb-2 h-12 w-12 text-muted-foreground" />
                                                <p class="text-muted-foreground">选择一个演员查看详情</p>
                                        </div>
                                {/if}
                        </div>
                </div>
        {/if}
</div>

<DialogConfirmation
        open={showDeleteConfirm}
        title="确认删除"
        description="确定要删除演员「{actorToDelete?.CharacterName || actorToDelete?.Name}」吗？此操作无法撤销。"
        confirmText="删除"
        onConfirm={confirmDelete}
        onCancel={() => {
                showDeleteConfirm = false;
                actorToDelete = null;
        }}
/>
