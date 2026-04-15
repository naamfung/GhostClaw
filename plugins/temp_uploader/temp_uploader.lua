-- Temp Uploader Plugin
-- This plugin provides temporary file upload functionality

-- 配置
local config = {
    -- 上传服务列表，按照优先级排序
    upload_services = {
        {
            name = "temp.sh",
            url = "https://temp.sh/upload",
            method = "POST",
            content_type = "multipart/form-data",
            file_field = "file"
        },
        {
            name = "0x0.st",
            url = "https://0x0.st",
            method = "POST",
            content_type = "application/octet-stream",
            file_field = nil
        },
        {
            name = "file.io",
            url = "https://file.io",
            method = "POST",
            content_type = "application/octet-stream",
            file_field = nil
        }
    }
}

-- 上传文件
local function upload_file(file_path, options)
    -- 合并配置
    local opts = options or {}
    local upload_services = config.upload_services
    if opts.upload_services then
        upload_services = opts.upload_services
    end
    
    -- 检查文件是否存在
    local file = io.open(file_path, "r")
    if not file then
        return {success = false, error = "File not found: " .. file_path}
    end
    file:close()
    
    -- 尝试上传到每个服务
    for _, service in ipairs(upload_services) do
        print("Uploading file to " .. service.name)
        
        local success, response
        if service.content_type == "multipart/form-data" and service.file_field then
            -- 使用 ghostclaw.upload_multipart 函数
            success, response = ghostclaw.upload_multipart(file_path, service.url, service.method, service.file_field)
        else
            -- 使用 ghostclaw.upload_raw 函数
            success, response = ghostclaw.upload_raw(file_path, service.url, service.method, service.content_type)
        end
        
        if success then
            print("Upload successful, response: " .. response)
            return {success = true, url = response, service = service.name}
        else
            print("Upload failed: " .. response)
        end
    end
    
    return {success = false, error = "Upload failed: All services failed"}
end

-- 列出可用的上传服务
local function list_services()
    local services = {}
    for i, service in ipairs(config.upload_services) do
        services[i] = {
            name = service.name,
            url = service.url,
            method = service.method,
            content_type = service.content_type,
            file_field = service.file_field
        }
    end
    return {success = true, services = services}
end

-- 更新上传服务配置
local function update_config(new_config)
    if new_config.upload_services then
        config.upload_services = new_config.upload_services
    end
    return {success = true, message = "Config updated successfully"}
end

return {
    upload_file = upload_file,
    list_services = list_services,
    update_config = update_config
}