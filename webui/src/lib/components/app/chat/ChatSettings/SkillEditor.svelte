<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import { ScrollArea } from '$lib/components/ui/scroll-area';
	import { Badge } from '$lib/components/ui/badge';
	import { X, Plus } from '@lucide/svelte';

	interface Skill {
		Name: string;
		DisplayName: string;
		Description: string;
		TriggerWords?: string[];
		SystemPrompt: string;
		OutputFormat?: string;
		Examples?: string[];
		Tags?: string[];
	}

	interface Props {
		skill: SkillListItem | null;
		onsave: () => void;
		oncancel: () => void;
	}

	interface SkillListItem {
		Name: string;
		DisplayName: string;
		Description: string;
		TriggerWords?: string[];
		Tags?: string[];
	}

	let { skill, onsave, oncancel }: Props = $props();

	let formData = $state<Partial<Skill>>({
		Name: '',
		DisplayName: '',
		Description: '',
		TriggerWords: [],
		SystemPrompt: '',
		OutputFormat: '',
		Examples: [],
		Tags: []
	});

	let newTriggerWord = $state('');
	let newTag = $state('');
	let isSaving = $state(false);
	let errorMessage = $state('');

	async function loadSkillDetails() {
		if (!skill) return;

		try {
			const response = await fetch(`/api/skills/${encodeURIComponent(skill.Name)}`);
			if (response.ok) {
				const data = await response.json();
				formData = { ...formData, ...data };
			}
		} catch (error) {
			console.error('加载技能详情失败:', error);
		}
	}

	function addTriggerWord() {
		if (newTriggerWord.trim()) {
			formData.TriggerWords = [...(formData.TriggerWords || []), newTriggerWord.trim()];
			newTriggerWord = '';
		}
	}

	function removeTriggerWord(index: number) {
		formData.TriggerWords = formData.TriggerWords?.filter((_, i) => i !== index);
	}

	function addTag() {
		if (newTag.trim()) {
			formData.Tags = [...(formData.Tags || []), newTag.trim()];
			newTag = '';
		}
	}

	function removeTag(index: number) {
		formData.Tags = formData.Tags?.filter((_, i) => i !== index);
	}

	async function handleSave() {
		// 验证必填字段
		if (!formData.DisplayName?.trim()) {
			errorMessage = '请输入显示名称';
			return;
		}
		if (!formData.SystemPrompt?.trim()) {
			errorMessage = '请输入系统提示';
			return;
		}

		// 如果是新建，自动生成 Name
		if (!skill && !formData.Name) {
			formData.Name = formData.DisplayName
				.toLowerCase()
				.replace(/\s+/g, '_')
				.replace(/[^a-z0-9_]/g, '');
		}

		isSaving = true;
		errorMessage = '';

		try {
			const url = skill
				? `/api/skills/${encodeURIComponent(skill.Name)}`
				: '/api/skills';
			const method = skill ? 'PUT' : 'POST';

			const response = await fetch(url, {
				method,
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(formData)
			});

			if (response.ok) {
				onsave();
			} else {
				const error = await response.json();
				errorMessage = error.error || '保存失败';
			}
		} catch (error) {
			console.error('保存技能失败:', error);
			errorMessage = '保存时发生错误';
		} finally {
			isSaving = false;
		}
	}

	$effect(() => {
		if (skill) {
			loadSkillDetails();
		}
	});
</script>

<div class="flex h-full flex-col">
	<!-- 头部 -->
	<div class="flex items-center justify-between border-b p-4">
		<h3 class="font-semibold">{skill ? '编辑技能' : '新建技能'}</h3>
		<div class="flex gap-2">
			<Button variant="outline" onclick={oncancel} disabled={isSaving}>取消</Button>
			<Button onclick={handleSave} disabled={isSaving}>
				{isSaving ? '保存中...' : '保存'}
			</Button>
		</div>
	</div>

	<!-- 表单 -->
	<ScrollArea class="flex-1">
		<form class="space-y-6 p-4" onsubmit={(e) => e.preventDefault()}>
			{#if errorMessage}
				<div class="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
					{errorMessage}
				</div>
			{/if}

			<!-- 基本信息 -->
			<div class="space-y-4">
				<h4 class="font-medium">基本信息</h4>

				<div class="space-y-2">
					<Label for="display-name">显示名称 *</Label>
					<Input
						id="display-name"
						placeholder="技能显示名称"
						bind:value={formData.DisplayName}
					/>
				</div>

				<div class="space-y-2">
					<Label for="description">描述</Label>
					<Textarea
						id="description"
						placeholder="简短描述这个技能的功能"
						rows={2}
						bind:value={formData.Description}
					/>
				</div>
			</div>

			<!-- 触发关键词 -->
			<div class="space-y-4">
				<h4 class="font-medium">触发关键词</h4>
				<p class="text-sm text-muted-foreground">
					当用户消息包含这些关键词时，技能可能会被激活
				</p>
				<div class="flex gap-2">
					<Input
						placeholder="添加触发关键词"
						bind:value={newTriggerWord}
						onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), addTriggerWord())}
					/>
					<Button type="button" variant="outline" size="icon" onclick={addTriggerWord}>
						<Plus class="h-4 w-4" />
					</Button>
				</div>
				<div class="flex flex-wrap gap-2">
					{#each formData.TriggerWords || [] as item, i (item)}
						<Badge variant="secondary" class="gap-1">
							{item}
							<button type="button" onclick={() => removeTriggerWord(i)} class="ml-1">
								<X class="h-3 w-3" />
							</button>
						</Badge>
					{/each}
				</div>
			</div>

			<!-- 系统提示 -->
			<div class="space-y-4">
				<h4 class="font-medium">系统提示 *</h4>
				<p class="text-sm text-muted-foreground">
					当技能激活时，系统提示会被注入到对话中，指导模型如何响应
				</p>
				<div class="space-y-2">
					<Textarea
						id="system-prompt"
						placeholder="输入系统提示..."
						rows={8}
						class="font-mono text-sm"
						bind:value={formData.SystemPrompt}
					/>
				</div>
			</div>

			<!-- 输出格式 -->
			<div class="space-y-4">
				<h4 class="font-medium">输出格式</h4>
				<p class="text-sm text-muted-foreground">定义模型输出的格式要求</p>
				<div class="space-y-2">
					<Textarea
						id="output-format"
						placeholder="描述输出格式..."
						rows={4}
						bind:value={formData.OutputFormat}
					/>
				</div>
			</div>

			<!-- 标签 -->
			<div class="space-y-4">
				<h4 class="font-medium">标签</h4>
				<div class="flex gap-2">
					<Input
						placeholder="添加标签"
						bind:value={newTag}
						onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), addTag())}
					/>
					<Button type="button" variant="outline" size="icon" onclick={addTag}>
						<Plus class="h-4 w-4" />
					</Button>
				</div>
				<div class="flex flex-wrap gap-2">
					{#each formData.Tags || [] as item, i (item)}
						<Badge variant="outline" class="gap-1">
							{item}
							<button type="button" onclick={() => removeTag(i)} class="ml-1">
								<X class="h-3 w-3" />
							</button>
						</Badge>
					{/each}
				</div>
			</div>
		</form>
	</ScrollArea>
</div>
