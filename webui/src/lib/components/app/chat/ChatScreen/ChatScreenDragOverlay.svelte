<script>
        import { Upload } from '@lucide/svelte';
</script>

<div
        class="pointer-events-none fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm"
>
        <div
                class="flex flex-col items-center justify-center rounded-2xl border-2 border-dashed border-border bg-background p-12 shadow-lg"
        >
                <Upload class="mb-4 h-12 w-12 text-muted-foreground" />

                <p class="text-lg font-medium text-foreground">添加附件</p>

                <p class="text-sm text-muted-foreground">将文件拖放到此处上传</p>
        </div>
</div>
