<script lang="ts">
        import { Input } from '$lib/components/ui/input';
        import { Label } from '$lib/components/ui/label';
        import { Button } from '$lib/components/ui/button';
        import { Badge } from '$lib/components/ui/badge';
        import { Checkbox } from '$lib/components/ui/checkbox';
        import { ScrollArea } from '$lib/components/ui/scroll-area';
        import { DialogConfirmation, DialogError } from '$lib/components/app/dialogs';
        import { onMount } from 'svelte';
        import { Pencil, Trash2, Plus, Search, Cpu, ChevronLeft } from '@lucide/svelte';
        import { config } from '$lib/stores/settings.svelte';

        interface ModelConfig {
                Name: string;
                APIType: string;
                BaseURL: string;
                APIKey: string;
                Model: string;
                Temperature: number;
                MaxTokens: number;
                RateLimit: number;
                Stream: boolean;
                Thinking: boolean;
                BlockDangerousCommands: boolean;
                Description: string;
                IsDefault: boolean;
        }

        let models = $state<ModelConfig[]>([]);
        let filteredModels = $state<ModelConfig[]>([]);
        let searchQuery = $state('');
        let isLoading = $state(false);
        let selectedModel = $state<ModelConfig | null>(null);
        let showEditor = $state(false);
        let editingModel = $state<ModelConfig | null>(null);
        let showDeleteConfirm = $state(false);
        let modelToDelete = $state<ModelConfig | null>(null);
        let showErrorDialog = $state(false);
        let errorTitle = $state('');
        let errorMessage = $state('');

        function showError(title: string, message: string) {
                errorTitle = title;
                errorMessage = message;
                showErrorDialog = true;
        }

        // Main model name from backend
        let mainModelName = $state('main');

        // 编辑表单字段
        let editForm = $state({
                Name: '',
                APIType: 'openai',
                BaseURL: '',
                APIKey: '',
                Model: '',
                Temperature: 0.7,
                MaxTokens: 4096,
                RateLimit: 0,
                Stream: true,
                Thinking: false,
                BlockDangerousCommands: true,
                Description: ''
        });

        const apiTypes = [
                { value: 'openai', label: 'OpenAI' },
                { value: 'anthropic', label: 'Anthropic' },
                { value: 'ollama', label: 'Ollama' },
                { value: 'deepseek', label: 'DeepSeek' }
        ];

        async function loadModels() {
                isLoading = true;
                try {
                        const response = await fetch('/api/models');
                        if (response.ok) {
                                const data = await response.json();
                                models = data.Models || [];
                                // 获取主模型名称
                                if (data.MainModel) {
                                        mainModelName = data.MainModel;
                                }
                                filterModels();
                        }
                } catch (error) {
                        console.error('加载模型列表失败:', error);
                } finally {
                        isLoading = false;
                }
        }



        async function loadMainModelName() {
                try {
                        // 从 ActorManager 获取主模型名称
                        // 这里需要修改后端 API，添加获取主模型名称的接口
                        // 暂时通过设置主模型后返回的信息来更新
                } catch (error) {
                        console.error('加载主模型名称失败:', error);
                }
        }

        function filterModels() {
                if (!searchQuery.trim()) {
                        filteredModels = models;
                } else {
                        const query = searchQuery.toLowerCase();
                        filteredModels = models.filter(
                                (m) =>
                                        m.Name.toLowerCase().includes(query) ||
                                        m.Model.toLowerCase().includes(query) ||
                                        m.Description.toLowerCase().includes(query)
                        );
                }
        }

        function handleCreate() {
                editingModel = null;
                editForm = {
                        Name: '',
                        APIType: 'openai',
                        BaseURL: '',
                        APIKey: '',
                        Model: '',
                        Temperature: 0.7,
                        MaxTokens: 4096,
                        RateLimit: 0,
                        Stream: true,
                        Thinking: false,
                        BlockDangerousCommands: true,
                        Description: ''
                };
                showEditor = true;
        }

        function handleEdit(model: ModelConfig) {
                editingModel = model;
                editForm = {
                        Name: model.Name,
                        APIType: model.APIType,
                        BaseURL: model.BaseURL,
                        APIKey: '', // 不显示已有的 key
                        Model: model.Model,
                        Temperature: model.Temperature,
                        MaxTokens: model.MaxTokens,
                        RateLimit: model.RateLimit ?? 0,
                        Stream: model.Stream ?? true,
                        Thinking: model.Thinking ?? false,
                        BlockDangerousCommands: model.BlockDangerousCommands ?? true,
                        Description: model.Description
                };
                showEditor = true;
        }

        function handleDeleteClick(model: ModelConfig) {
                if (model.Name === 'main') return; // 不允许删除 main 模型
                modelToDelete = model;
                showDeleteConfirm = true;
        }

        async function confirmDelete() {
                if (!modelToDelete) return;

                try {
                        const response = await fetch(`/api/models/${encodeURIComponent(modelToDelete.Name)}`, {
                                method: 'DELETE'
                        });

                        if (response.ok) {
                                models = models.filter((m) => m.Name !== modelToDelete.Name);
                                filterModels();
                                if (selectedModel?.Name === modelToDelete.Name) {
                                        selectedModel = null;
                                }
                        } else {
                                const error = await response.json();
                                showError('删除失败', error.error || '删除失败');
                        }
                } catch (error) {
                        console.error('删除模型失败:', error);
                        showError('删除失败', '删除模型时发生错误');
                } finally {
                        showDeleteConfirm = false;
                        modelToDelete = null;
                }
        }

        async function handleSetMainModel(model) {
                try {
                        const response = await fetch(`/api/models/${encodeURIComponent(model.Name)}/set-main`, {
                                method: 'PATCH'
                        });

                        if (response.ok) {
                                // 更新主模型名称
                                mainModelName = model.Name;
                                // 重新加载模型列表
                                await loadModels();
                        } else {
                                const error = await response.json();
                                console.error('设置主模型失败:', error.error || '设置主模型失败');
                        }
                } catch (error) {
                        console.error('设置主模型失败:', error);
                }
        }

        async function handleEditorSave() {
                if (!editForm.Name.trim()) {
                        showError('输入错误', '模型名称不能为空');
                        return;
                }

                // 确保数值字段为有效数字（修复 bind:value 通过组件传递时可能未正确转型的问题）
                const payload = {
                        ...editForm,
                        Temperature: Number(editForm.Temperature) || 0.7,
                        MaxTokens: Number.isFinite(editForm.MaxTokens) ? Math.round(editForm.MaxTokens) : 4096
                };

                try {
                        let response;
                        if (editingModel) {
                                // 更新
                                response = await fetch(`/api/models/${encodeURIComponent(editingModel.Name)}`, {
                                        method: 'PUT',
                                        headers: { 'Content-Type': 'application/json' },
                                        body: JSON.stringify(payload)
                                });
                        } else {
                                // 创建
                                response = await fetch('/api/models', {
                                        method: 'POST',
                                        headers: { 'Content-Type': 'application/json' },
                                        body: JSON.stringify(payload)
                                });
                        }

                        if (response.ok) {
                                showEditor = false;
                                // 重命名时清理选中状态，避免残留旧名称
                                if (editingModel && editForm.Name !== editingModel.Name) {
                                        selectedModel = null;
                                }
                                editingModel = null;
                                loadModels();
                        } else {
                                const error = await response.json();
                                showError('保存失败', error.error || '保存失败');
                        }
                } catch (error) {
                        console.error('保存模型失败:', error);
                        showError('保存失败', '保存模型时发生错误');
                }
        }

        function handleEditorCancel() {
                showEditor = false;
                editingModel = null;
        }

        function handleApiTypeChange(type: string) {
                editForm.APIType = type;
                // 根据类型设置默认 base URL
                switch (type) {
                        case 'openai':
                                if (!editForm.BaseURL) editForm.BaseURL = 'https://api.openai.com/v1';
                                break;
                        case 'anthropic':
                                if (!editForm.BaseURL) editForm.BaseURL = 'https://api.anthropic.com/v1';
                                break;
                        case 'ollama':
                                if (!editForm.BaseURL) editForm.BaseURL = 'http://localhost:11434/api';
                                break;
                        case 'deepseek':
                                if (!editForm.BaseURL) editForm.BaseURL = 'https://api.deepseek.com/v1';
                                break;
                }
        }



        $effect(() => {
                searchQuery;
                filterModels();
        });

        onMount(() => {
                loadModels();
        });
</script>

<div class="flex h-full flex-col">
        {#if showEditor}
                <!-- 编辑器 -->
                <div class="flex h-full flex-col">
                        <div class="mb-4 flex items-center justify-between border-b pb-3">
                                <h3 class="font-semibold">{editingModel ? '编辑模型' : '新建模型'}</h3>
                        </div>

                        <ScrollArea class="flex-1 pr-4">
                                <div class="space-y-4">
                                        <div class="space-y-2">
                                                <Label for="name">模型名称</Label>
                                                <Input
                                                        id="name"
                                                        bind:value={editForm.Name}
                                                        placeholder="如：gpt4、claude"
                                                />
                                                <p class="text-xs text-muted-foreground">模型的名称标识</p>
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="api-type">API 类型</Label>
                                                <select
                                                        id="api-type"
                                                        class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                                                        bind:value={editForm.APIType}
                                                        onchange={() => handleApiTypeChange(editForm.APIType)}
                                                >
                                                        {#each apiTypes as type (type.value)}
                                                                <option value={type.value}>{type.label}</option>
                                                        {/each}
                                                </select>
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="base-url">Base URL</Label>
                                                <Input
                                                        id="base-url"
                                                        bind:value={editForm.BaseURL}
                                                        placeholder="API 基础地址"
                                                />
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="api-key">API 密钥</Label>
                                                <Input
                                                        id="api-key"
                                                        type="password"
                                                        bind:value={editForm.APIKey}
                                                        placeholder={editingModel ? '留空保持不变' : '输入 API 密钥'}
                                                />
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="model">模型 ID</Label>
                                                <Input
                                                        id="model"
                                                        bind:value={editForm.Model}
                                                        placeholder="如：gpt-4、claude-3-opus"
                                                />
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="temperature">温度</Label>
                                                <Input
                                                        id="temperature"
                                                        type="number"
                                                        min={0}
                                                        max={2}
                                                        step={0.01}
                                                        bind:value={editForm.Temperature}
                                                />
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="max-tokens">最大词元数</Label>
                                                <Input
                                                        id="max-tokens"
                                                        type="number"
                                                        min={1}
                                                        step={1}
                                                        bind:value={editForm.MaxTokens}
                                                />
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="rate-limit">请求速率限制（次/分钟）</Label>
                                                <Input
                                                        id="rate-limit"
                                                        type="number"
                                                        min={0}
                                                        step={1}
                                                        bind:value={editForm.RateLimit}
                                                />
                                                <p class="text-xs text-muted-foreground">限制每分钟请求数，0 表示不限制。适用于有速率限制的服务商。</p>
                                        </div>

                                        <!-- 新增：Stream、Thinking、BlockDangerousCommands -->
                                        <div class="flex items-center space-x-3">
                                                <Checkbox
                                                        id="stream"
                                                        checked={editForm.Stream}
                                                        onCheckedChange={(checked) => (editForm.Stream = Boolean(checked))}
                                                        class="mt-0.5"
                                                />
                                                <div class="space-y-0.5">
                                                        <label for="stream" class="cursor-pointer text-sm font-medium leading-none">
                                                                流式输出
                                                        </label>
                                                        <p class="text-xs text-muted-foreground">启用流式传输，逐字显示 AI 回复</p>
                                                </div>
                                        </div>

                                        <div class="flex items-center space-x-3">
                                                <Checkbox
                                                        id="thinking"
                                                        checked={editForm.Thinking}
                                                        onCheckedChange={(checked) => (editForm.Thinking = Boolean(checked))}
                                                        class="mt-0.5"
                                                />
                                                <div class="space-y-0.5">
                                                        <label for="thinking" class="cursor-pointer text-sm font-medium leading-none">
                                                                思考模式
                                                        </label>
                                                        <p class="text-xs text-muted-foreground">启用模型思考过程，模型会在回复前展示推理步骤</p>
                                                </div>
                                        </div>

                                        <div class="flex items-center space-x-3">
                                                <Checkbox
                                                        id="block-dangerous"
                                                        checked={editForm.BlockDangerousCommands}
                                                        onCheckedChange={(checked) => (editForm.BlockDangerousCommands = Boolean(checked))}
                                                        class="mt-0.5"
                                                />
                                                <div class="space-y-0.5">
                                                        <label for="block-dangerous" class="cursor-pointer text-sm font-medium leading-none">
                                                                阻止危险命令
                                                        </label>
                                                        <p class="text-xs text-muted-foreground">阻止模型执行潜在危险的 Shell 命令</p>
                                                </div>
                                        </div>

                                        <div class="space-y-2">
                                                <Label for="description">描述</Label>
                                                <Input
                                                        id="description"
                                                        bind:value={editForm.Description}
                                                        placeholder="模型的简短描述..."
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
                                        placeholder="搜索模型..."
                                        class="pl-9"
                                        bind:value={searchQuery}
                                />
                        </div>
                        <Button onclick={handleCreate}>
                                <Plus class="mr-1 h-4 w-4" />
                                新建
                        </Button>
                </div>

                <!-- 模型列表 -->
                <div class="flex flex-1 gap-4 overflow-hidden md:flex-row flex-col">
                        <!-- 左侧列表 -->
                        <ScrollArea class="h-full w-full md:w-56 md:flex-shrink-0 border rounded-lg {selectedModel ? 'hidden md:block' : ''}">
                                {#if isLoading}
                                        <div class="flex items-center justify-center p-4">
                                                <span class="text-muted-foreground">加载中...</span>
                                        </div>
                                {:else if filteredModels.length === 0}
                                        <div class="flex flex-col items-center justify-center p-8 text-center">
                                                <Cpu class="mb-2 h-8 w-8 text-muted-foreground" />
                                                <p class="text-muted-foreground">没有找到模型</p>
                                        </div>
                                {:else}
                                        <div class="space-y-1 p-2">
                                                {#each filteredModels as model (model.Name)}
                                                        <button
                                                                class="flex w-full cursor-pointer items-center gap-3 rounded-lg p-3 text-left transition-colors hover:bg-accent {selectedModel?.Name ===
                                                                model.Name
                                                                        ? 'bg-accent'
                                                                        : ''}"
                                                                onclick={() => (selectedModel = model)}
                                                        >
                                                                <div class="flex-1 overflow-hidden">
                                                                        <div class="flex items-center gap-2">
                                                                                <span class="truncate font-medium">{model.Name}</span>
                                                                                {#if model.IsDefault}
                                                                                        <Badge variant="secondary" class="text-xs">默认</Badge>
                                                                                {/if}
                                                                        </div>
                                                                        <p class="truncate text-xs text-muted-foreground">{model.Model || '未配置'}</p>
                                                                </div>
                                                        </button>
                                                {/each}
                                        </div>
                                {/if}
                        </ScrollArea>

                        <!-- 右侧详情 -->
                        <div class="min-w-0 flex-1 overflow-hidden rounded-lg border {selectedModel ? '' : 'hidden md:block'}">
                                {#if selectedModel}
                                        {#key selectedModel.Name}
                                                <div class="flex h-full flex-col">
                                                <!-- 头部 -->
                                                <div class="flex flex-col gap-3 border-b p-4">
                                                        <!-- 移动端返回按钮 -->
                                                        <button
                                                                class="md:hidden flex items-center gap-1 text-sm text-muted-foreground mb-2 self-start"
                                                                onclick={() => (selectedModel = null)}
                                                        >
                                                                <ChevronLeft class="h-4 w-4" />
                                                                返回列表
                                                        </button>
                                                        <div class="flex items-center gap-3 min-w-0">
                                                                <div class="min-w-0 flex-1">
                                                                        <h3 class="font-semibold">
                                                                {selectedModel.Name}
                                                                {#if selectedModel.IsDefault}
                                                                        <Badge variant="secondary" class="ml-2">默认</Badge>
                                                                {/if}
                                                        </h3>
                                                                        <p class="truncate text-sm text-muted-foreground">{selectedModel.Model}</p>
                                                                </div>
                                                        </div>
                                                        <div class="flex flex-wrap gap-2">
                                                                {#if !selectedModel.IsDefault}
                                                                        <Button
                                                                                variant="outline"
                                                                                size="sm"
                                                                                class="mr-2"
                                                                                onclick={() => handleSetMainModel(selectedModel)}
                                                                        >
                                                                                设为主模
                                                                        </Button>
                                                                {/if}
                                                                <Button variant="outline" size="sm" onclick={() => handleEdit(selectedModel)}>
                                                                        <Pencil class="mr-1 h-4 w-4" />
                                                                        编辑
                                                                </Button>
                                                                {#if !selectedModel.IsDefault}
                                                                        <Button
                                                                                variant="outline"
                                                                                size="sm"
                                                                                class="text-destructive hover:bg-destructive hover:text-white"
                                                                                onclick={() => handleDeleteClick(selectedModel)}
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
                                                                        <h4 class="mb-1 text-xs font-medium text-muted-foreground">API 类型</h4>
                                                                        <Badge variant="outline">{selectedModel.APIType}</Badge>
                                                                </div>

                                                                <div>
                                                                        <h4 class="mb-1 text-xs font-medium text-muted-foreground">Base URL</h4>
                                                                        <p class="text-sm break-all">{selectedModel.BaseURL || '未配置'}</p>
                                                                </div>

                                                                <div>
                                                                        <h4 class="mb-1 text-xs font-medium text-muted-foreground">模型 ID</h4>
                                                                        <p class="text-sm">{selectedModel.Model || '未配置'}</p>
                                                                </div>

                                                                <div>
                                                                        <h4 class="mb-1 text-xs font-medium text-muted-foreground">温度</h4>
                                                                        <p class="text-sm">{selectedModel.Temperature}</p>
                                                                </div>

                                                                <div>
                                                                        <h4 class="mb-1 text-xs font-medium text-muted-foreground">最大词元数</h4>
                                                                        <p class="text-sm">{selectedModel.MaxTokens}</p>
                                                                </div>

                                                                <div>
                                                                        <h4 class="mb-1 text-xs font-medium text-muted-foreground">请求速率限制</h4>
                                                                        <p class="text-sm">{selectedModel.RateLimit > 0 ? selectedModel.RateLimit + ' 次/分钟' : '不限制'}</p>
                                                                </div>

                                                                <div class="flex items-center gap-4">
                                                                        <div>
                                                                                <h4 class="mb-1 text-xs font-medium text-muted-foreground">流式输出</h4>
                                                                                <Badge variant={selectedModel.Stream ? 'default' : 'outline'}>
                                                                                        {selectedModel.Stream ? '已启用' : '已禁用'}
                                                                                </Badge>
                                                                        </div>
                                                                        <div>
                                                                                <h4 class="mb-1 text-xs font-medium text-muted-foreground">思考模式</h4>
                                                                                <Badge variant={selectedModel.Thinking ? 'default' : 'outline'}>
                                                                                        {selectedModel.Thinking ? '已启用' : '已禁用'}
                                                                                </Badge>
                                                                        </div>
                                                                </div>

                                                                <div>
                                                                        <h4 class="mb-1 text-xs font-medium text-muted-foreground">阻止危险命令</h4>
                                                                        <Badge variant={selectedModel.BlockDangerousCommands ? 'default' : 'outline'}>
                                                                                {selectedModel.BlockDangerousCommands ? '已启用' : '已禁用'}
                                                                        </Badge>
                                                                </div>

                                                                {#if selectedModel.Description}
                                                                        <div>
                                                                                <h4 class="mb-1 text-xs font-medium text-muted-foreground">描述</h4>
                                                                                <p class="text-sm">{selectedModel.Description}</p>
                                                                        </div>
                                                                {/if}
                                                        </div>
                                                </ScrollArea>
                                                </div>
                                        {/key}
                                {:else}
                                        <div class="flex h-full flex-col items-center justify-center text-center">
                                                <Cpu class="mb-2 h-12 w-12 text-muted-foreground" />
                                                <p class="text-muted-foreground">选择一个模型查看详情</p>
                                        </div>
                                {/if}
                        </div>
                </div>
        {/if}
</div>

<DialogConfirmation
        open={showDeleteConfirm}
        title="确认删除"
        description="确定要删除模型「{modelToDelete?.Name}」吗？此操作无法撤销。"
        confirmText="删除"
        onConfirm={confirmDelete}
        onCancel={() => {
                showDeleteConfirm = false;
                modelToDelete = null;
        }}
/>

<DialogError
        bind:open={showErrorDialog}
        title={errorTitle}
        message={errorMessage}
/>
