-- Temp Uploader Plugin for GarClaw
-- Description: 上传文件到临时文件分享服务
-- Version: 1.0.0
-- Supported services: temp.sh

-- 安全獲取表中的值
local function safe_get(tbl, key, default)
    if tbl == nil then return default end
    local val = tbl[key]
    if val == nil then return default end
    return val
end

-- URL 編碼函數
local function url_encode(str)
    if not str then return "" end
    local encoded = ""
    for i = 1, #str do
        local c = string.sub(str, i, i)
        local byte = string.byte(c)
        if (byte >= 65 and byte <= 90) or  -- A-Z
           (byte >= 97 and byte <= 122) or -- a-z
           (byte >= 48 and byte <= 57) or  -- 0-9
           c == "-" or c == "_" or c == "." or c == "~" then
            encoded = encoded .. c
        else
            encoded = encoded .. string.format("%%%02X", byte)
        end
    end
    return encoded
end

-- 上傳文件到 temp.sh
function upload_to_temp_sh(file_path)
    if not file_path or file_path == "" then
        return "錯誤: 文件路徑不能為空"
    end
    
    -- 檢查文件是否存在
    local file = io.open(file_path, "rb")
    if not file then
        return "錯誤: 文件不存在或無法打開"
    end
    file:close()
    
    garclaw.log("info", "上傳文件到 temp.sh: " .. file_path)
    
    -- 使用 curl 上傳文件
    local command = string.format(
        "curl -s -F 'file=@%s' https://temp.sh",
        file_path
    )
    
    local result, err = garclaw.shell(command)
    if not result then
        return "錯誤: 上傳失敗: " .. (err or "未知錯誤")
    end
    
    -- 檢查上傳結果
    if result.exit_code ~= 0 then
        return "錯誤: 上傳失敗 (退出碼: " .. result.exit_code .. "): " .. (result.stderr or "")
    end
    
    -- 提取上傳後的 URL
    local url = result.stdout
    if not url or url == "" then
        return "錯誤: 上傳成功但未返回 URL"
    end
    
    -- 清理 URL (移除多餘的換行符)
    url = string.gsub(url, "\n", "")
    url = string.gsub(url, "\r", "")
    
    return url
end

-- 通用上傳函數
function upload_file(file_path, service)
    service = service or "temp.sh"
    
    if service == "temp.sh" then
        return upload_to_temp_sh(file_path)
    else
        return "錯誤: 不支持的服務: " .. service
    end
end

-- 獲取支持的服務列表
function get_supported_services()
    return {
        "temp.sh"
    }
end

-- 幫助信息
function help()
    return [[📤 臨時文件上傳插件使用說明
━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📋 可用函數:

1. upload_file("文件路徑", "服務名")
   上傳文件到臨時文件分享服務
   服務名預設為 "temp.sh"
   示例: upload_file("test.txt")
         upload_file("report.pdf", "temp.sh")

2. upload_to_temp_sh("文件路徑")
   直接上傳文件到 temp.sh
   示例: upload_to_temp_sh("image.jpg")

3. get_supported_services()
   獲取支持的服務列表

4. help()
   顯示此幫助信息

━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 支持的服務:
• temp.sh - 提供短期文件存儲
💡 提示: 上傳後會返回文件的臨時 URL，請妥善保存]]
end