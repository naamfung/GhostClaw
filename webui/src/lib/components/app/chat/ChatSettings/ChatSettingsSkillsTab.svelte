<script lang="ts">
        import { Button } from '$lib/components/ui/button';
        import { Input } from '$lib/components/ui/input';
        import { ScrollArea } from '$lib/components/ui/scroll-area';
        import { Badge } from '$lib/components/ui/badge';
        import SkillEditor from './SkillEditor.svelte';
        import { DialogConfirmation } from '$lib/components/app/dialogs';
        import { onMount } from 'svelte';
        import { Pencil, Trash2, Plus, Search, Wrench, ChevronLeft } from '@lucide/svelte';

        interface SkillListItem {
                Name: string;
                DisplayName: string;
                Description: string;
                TriggerWords?: string[];
                Tags?: string[];
        }

        let skills = $state<SkillListItem[]>([]);
        let filteredSkills = $state<SkillListItem[]>([]);
        let searchQuery = $state('');
        let isLoading = $state(false);
        let selectedSkill = $state<SkillListItem | null>(null);
        let showEditor = $state(false);
        let editingSkill = $state<SkillListItem | null>(null);
        let showDeleteConfirm = $state(false);
        let skillToDelete = $state<SkillListItem | null>(null);

        async function loadSkills() {
                isLoading = true;
                try {
                        const response = await fetch('/api/skills');
                        if (response.ok) {
                                const data = await response.json();
                                skills = data.Skills || [];
                                filterSkills();
                        }
                } catch (error) {
                        console.error('加载技能列表失败:', error);
                } finally {
                        isLoading = false;
                }
        }

        function filterSkills() {
                if (!searchQuery.trim()) {
                        filteredSkills = skills;
                } else {
                        const query = searchQuery.toLowerCase();
                        filteredSkills = skills.filter(
                                (s) =>
                                        s.Name.toLowerCase().includes(query) ||
                                        s.DisplayName.toLowerCase().includes(query) ||
                                        s.Description.toLowerCase().includes(query)
                        );
                }
        }

        function handleCreate() {
                editingSkill = null;
                showEditor = true;
        }

        function handleEdit(skill: SkillListItem) {
                editingSkill = skill;
                showEditor = true;
        }

        function handleDeleteClick(skill: SkillListItem) {
                skillToDelete = skill;
                showDeleteConfirm = true;
        }

        async function confirmDelete() {
                if (!skillToDelete) return;

                try {
                        const response = await fetch(`/api/skills/${encodeURIComponent(skillToDelete.Name)}`, {
                                method: 'DELETE'
                        });

                        if (response.ok) {
                                skills = skills.filter((s) => s.Name !== skillToDelete.Name);
                                filterSkills();
                                if (selectedSkill?.Name === skillToDelete.Name) {
                                        selectedSkill = null;
                                }
                        } else {
                                const error = await response.json();
                                alert(error.error || '删除失败');
                        }
                } catch (error) {
                        console.error('删除技能失败:', error);
                        alert('删除技能时发生错误');
                } finally {
                        showDeleteConfirm = false;
                        skillToDelete = null;
                }
        }

        function handleEditorSave() {
                showEditor = false;
                editingSkill = null;
                loadSkills();
        }

        function handleEditorCancel() {
                showEditor = false;
                editingSkill = null;
        }

        // 使用 $effect 监听搜索查询变化
        $effect(() => {
                searchQuery;
                filterSkills();
        });

        onMount(() => {
                loadSkills();
        });
</script>

<div class="flex h-full flex-col">
        {#if showEditor}
                <SkillEditor skill={editingSkill} onsave={handleEditorSave} oncancel={handleEditorCancel} />
        {:else}
                <!-- 搜索栏和创建按钮 -->
                <div class="mb-4 flex items-center gap-2">
                        <div class="relative flex-1">
                                <Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                                <Input
                                        type="text"
                                        placeholder="搜索技能..."
                                        class="pl-9"
                                        bind:value={searchQuery}
                                />
                        </div>
                        <Button onclick={handleCreate}>
                                <Plus class="mr-1 h-4 w-4" />
                                新建
                        </Button>
                </div>

                <!-- 技能列表 -->
                <div class="flex flex-1 gap-4 overflow-hidden md:flex-row flex-col">
                        <!-- 左侧列表 -->
                        <ScrollArea class="h-full w-full md:w-56 md:flex-shrink-0 border rounded-lg {selectedSkill ? 'hidden md:block' : ''}">
                                {#if isLoading}
                                        <div class="flex items-center justify-center p-4">
                                                <span class="text-muted-foreground">加载中...</span>
                                        </div>
                                {:else if filteredSkills.length === 0}
                                        <div class="flex flex-col items-center justify-center p-8 text-center">
                                                <Wrench class="mb-2 h-8 w-8 text-muted-foreground" />
                                                <p class="text-muted-foreground">没有找到技能</p>
                                        </div>
                                {:else}
                                        <div class="space-y-1 p-2">
                                                {#each filteredSkills as skill (skill.Name)}
                                                        <button
                                                                class="flex w-full cursor-pointer items-center gap-3 rounded-lg p-3 text-left transition-colors hover:bg-accent {selectedSkill?.Name ===
                                                                skill.Name
                                                                        ? 'bg-accent'
                                                                        : ''}"
                                                                onclick={() => (selectedSkill = skill)}
                                                        >
                                                                <Wrench class="h-5 w-5 flex-shrink-0 text-muted-foreground" />
                                                                <div class="flex-1 overflow-hidden">
                                                                        <span class="truncate font-medium">{skill.DisplayName}</span>
                                                                        <p class="truncate text-xs text-muted-foreground">{skill.Description}</p>
                                                                </div>
                                                        </button>
                                                {/each}
                                        </div>
                                {/if}
                        </ScrollArea>

                        <!-- 右侧详情 -->
                        <div class="min-w-0 flex-1 overflow-hidden rounded-lg border {selectedSkill ? '' : 'hidden md:block'}">
                                {#if selectedSkill}
                                        {#key selectedSkill.Name}
                                                <div class="flex h-full flex-col">
                                                <!-- 头部 -->
                                                <div class="flex flex-col gap-3 border-b p-4">
                                                        <!-- 移动端返回按钮 -->
                                                        <button
                                                                class="md:hidden flex items-center gap-1 text-sm text-muted-foreground mb-2 self-start"
                                                                onclick={() => (selectedSkill = null)}
                                                        >
                                                                <ChevronLeft class="h-4 w-4" />
                                                                返回列表
                                                        </button>
                                                        <div class="flex items-center justify-between min-w-0">
                                                                <div class="flex items-center gap-3">
                                                                        <Wrench class="h-6 w-6 text-muted-foreground" />
                                                                        <div>
                                                                                <h3 class="font-semibold">{selectedSkill.DisplayName}</h3>
                                                                                <p class="text-sm text-muted-foreground">{selectedSkill.Name}</p>
                                                                        </div>
                                                                </div>
                                                                <div class="flex gap-2">
                                                                        <Button variant="outline" size="sm" onclick={() => handleEdit(selectedSkill)}>
                                                                                <Pencil class="mr-1 h-4 w-4" />
                                                                                编辑
                                                                        </Button>
                                                                        <Button
                                                                                variant="outline"
                                                                                size="sm"
                                                                                class="text-destructive hover:bg-destructive hover:text-destructive-foreground"
                                                                                onclick={() => handleDeleteClick(selectedSkill)}
                                                                        >
                                                                                <Trash2 class="mr-1 h-4 w-4" />
                                                                                删除
                                                                        </Button>
                                                                </div>
                                                        </div>
                                                </div>

                                                <!-- 详情 -->
                                                <ScrollArea class="flex-1 p-4">
                                                        <p class="text-sm">{selectedSkill.Description}</p>

                                                        {#if selectedSkill.TriggerWords && selectedSkill.TriggerWords.length > 0}
                                                                <div class="mt-4">
                                                                        <h4 class="mb-2 text-sm font-medium">触发关键词</h4>
                                                                        <div class="flex flex-wrap gap-2">
                                                                                {#each selectedSkill.TriggerWords as word (word)}
                                                                                        <Badge variant="secondary">{word}</Badge>
                                                                                {/each}
                                                                        </div>
                                                                </div>
                                                        {/if}

                                                        {#if selectedSkill.Tags && selectedSkill.Tags.length > 0}
                                                                <div class="mt-4">
                                                                        <h4 class="mb-2 text-sm font-medium">标签</h4>
                                                                        <div class="flex flex-wrap gap-2">
                                                                                {#each selectedSkill.Tags as tag (tag)}
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
                                                <Wrench class="mb-2 h-12 w-12 text-muted-foreground" />
                                                <p class="text-muted-foreground">选择一个技能查看详情</p>
                                        </div>
                                {/if}
                        </div>
                </div>
        {/if}
</div>

<DialogConfirmation
        open={showDeleteConfirm}
        title="确认删除"
        description="确定要删除技能「{skillToDelete?.DisplayName}」吗？此操作无法撤销。"
        confirmText="删除"
        onconfirm={confirmDelete}
        oncancel={() => {
                showDeleteConfirm = false;
                skillToDelete = null;
        }}
/>
