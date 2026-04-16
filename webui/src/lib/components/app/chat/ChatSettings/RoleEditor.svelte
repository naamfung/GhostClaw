<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import { ScrollArea } from '$lib/components/ui/scroll-area';
	import { Badge } from '$lib/components/ui/badge';
	import { X, Plus } from '@lucide/svelte';

	interface Role {
		Name: string;
		DisplayName: string;
		Description: string;
		Icon?: string;
		Identity?: string;
		Personality?: string;
		SpeakingStyle?: string;
		Expertise?: string[];
		Guidelines?: string[];
		Forbidden?: string[];
		Skills?: string[];
		Tags?: string[];
		IsPreset: boolean;
	}

	interface Props {
		role: RoleListItem | null;
		onsave: () => void;
		oncancel: () => void;
	}

	interface RoleListItem {
		Name: string;
		DisplayName: string;
		Description: string;
		Icon?: string;
		IsPreset: boolean;
		Tags?: string[];
	}

	let { role, onsave, oncancel }: Props = $props();

	let formData = $state<Partial<Role>>({
		Name: '',
		DisplayName: '',
		Description: '',
		Icon: '👤',
		Identity: '',
		Personality: '',
		SpeakingStyle: '',
		Expertise: [],
		Guidelines: [],
		Forbidden: [],
		Skills: [],
		Tags: [],
		IsPreset: false
	});

	let newExpertise = $state('');
	let newGuideline = $state('');
	let newForbidden = $state('');
	let newTag = $state('');
	let isSaving = $state(false);
	let errorMessage = $state('');

	async function loadRoleDetails() {
		if (!role) return;

		try {
			const response = await fetch(`/api/roles/${encodeURIComponent(role.Name)}`);
			if (response.ok) {
				const data = await response.json();
				formData = { ...formData, ...data };
			}
		} catch (error) {
			console.error('加载角色详情失败:', error);
		}
	}

	function addExpertise() {
		if (newExpertise.trim()) {
			formData.Expertise = [...(formData.Expertise || []), newExpertise.trim()];
			newExpertise = '';
		}
	}

	function removeExpertise(index: number) {
		formData.Expertise = formData.Expertise?.filter((_, i) => i !== index);
	}

	function addGuideline() {
		if (newGuideline.trim()) {
			formData.Guidelines = [...(formData.Guidelines || []), newGuideline.trim()];
			newGuideline = '';
		}
	}

	function removeGuideline(index: number) {
		formData.Guidelines = formData.Guidelines?.filter((_, i) => i !== index);
	}

	function addForbidden() {
		if (newForbidden.trim()) {
			formData.Forbidden = [...(formData.Forbidden || []), newForbidden.trim()];
			newForbidden = '';
		}
	}

	function removeForbidden(index: number) {
		formData.Forbidden = formData.Forbidden?.filter((_, i) => i !== index);
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

		// 如果是新建，自动生成 Name
		if (!role && !formData.Name) {
			formData.Name = formData.DisplayName.toLowerCase().replace(/\s+/g, '_');
		}

		isSaving = true;
		errorMessage = '';

		try {
			const url = role
				? `/api/roles/${encodeURIComponent(role.Name)}`
				: '/api/roles';
			const method = role ? 'PUT' : 'POST';

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
			console.error('保存人格失败:', error);
			errorMessage = '保存时发生错误';
		} finally {
			isSaving = false;
		}
	}

	$effect(() => {
		if (role) {
			loadRoleDetails();
		}
	});
</script>

<div class="flex h-full flex-col">
	<!-- 头部 -->
	<div class="flex items-center justify-between border-b p-4">
		<h3 class="font-semibold">{role ? '编辑角色' : '新建角色'}</h3>
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

				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-2">
						<Label for="display-name">显示名称 *</Label>
						<Input
							id="display-name"
							placeholder="角色显示名称"
							bind:value={formData.DisplayName}
						/>
					</div>

					<div class="space-y-2">
						<Label for="icon">图标</Label>
						<Input id="icon" placeholder="emoji 图标" bind:value={formData.Icon} />
					</div>
				</div>

				<div class="space-y-2">
					<Label for="description">描述</Label>
					<Textarea
						id="description"
						placeholder="简短描述这个角色的特点"
						rows={2}
						bind:value={formData.Description}
					/>
				</div>
			</div>

			<!-- 身份和性格 -->
			<div class="space-y-4">
				<h4 class="font-medium">身份和性格</h4>

				<div class="space-y-2">
					<Label for="identity">身份定位</Label>
					<Textarea
						id="identity"
						placeholder="描述角色的身份、定位和特点"
						rows={3}
						bind:value={formData.Identity}
					/>
				</div>

				<div class="space-y-2">
					<Label for="personality">性格特质</Label>
					<Textarea
						id="personality"
						placeholder="描述角色的性格特点"
						rows={2}
						bind:value={formData.Personality}
					/>
				</div>

				<div class="space-y-2">
					<Label for="speaking-style">说话风格</Label>
					<Textarea
						id="speaking-style"
						placeholder="描述角色的说话方式和风格"
						rows={2}
						bind:value={formData.SpeakingStyle}
					/>
				</div>
			</div>

			<!-- 专业领域 -->
			<div class="space-y-4">
				<h4 class="font-medium">专业领域</h4>
				<div class="flex gap-2">
					<Input
						placeholder="添加专业领域"
						bind:value={newExpertise}
						onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), addExpertise())}
					/>
					<Button type="button" variant="outline" size="icon" onclick={addExpertise}>
						<Plus class="h-4 w-4" />
					</Button>
				</div>
				<div class="flex flex-wrap gap-2">
					{#each formData.Expertise || [] as item, i (item)}
						<Badge variant="secondary" class="gap-1">
							{item}
							<button type="button" onclick={() => removeExpertise(i)} class="ml-1">
								<X class="h-3 w-3" />
							</button>
						</Badge>
					{/each}
				</div>
			</div>

			<!-- 行为准则 -->
			<div class="space-y-4">
				<h4 class="font-medium">行为准则</h4>
				<div class="flex gap-2">
					<Input
						placeholder="添加行为准则"
						bind:value={newGuideline}
						onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), addGuideline())}
					/>
					<Button type="button" variant="outline" size="icon" onclick={addGuideline}>
						<Plus class="h-4 w-4" />
					</Button>
				</div>
				<div class="flex flex-wrap gap-2">
					{#each formData.Guidelines || [] as item, i (item)}
						<Badge variant="secondary" class="gap-1">
							{item}
							<button type="button" onclick={() => removeGuideline(i)} class="ml-1">
								<X class="h-3 w-3" />
							</button>
						</Badge>
					{/each}
				</div>
			</div>

			<!-- 禁止事项 -->
			<div class="space-y-4">
				<h4 class="font-medium">禁止事项</h4>
				<div class="flex gap-2">
					<Input
						placeholder="添加禁止事项"
						bind:value={newForbidden}
						onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), addForbidden())}
					/>
					<Button type="button" variant="outline" size="icon" onclick={addForbidden}>
						<Plus class="h-4 w-4" />
					</Button>
				</div>
				<div class="flex flex-wrap gap-2">
					{#each formData.Forbidden || [] as item, i (item)}
						<Badge variant="destructive" class="gap-1">
							{item}
							<button type="button" onclick={() => removeForbidden(i)} class="ml-1">
								<X class="h-3 w-3" />
							</button>
						</Badge>
					{/each}
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
