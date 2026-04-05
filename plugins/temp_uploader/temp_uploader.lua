-- Temp Uploader Plugin
-- This plugin provides temporary file upload functionality

local function upload_file(file_path)
    -- Check if file exists
    local file_exists = io.open(file_path, "r")
    if not file_exists then
        return {success = false, error = "File not found: " .. file_path}
    end
    file_exists:close()
    
    -- Try to upload using curl
    local command = string.format("curl -F \"file=@%s\" https://temp.sh/upload", file_path)
    local handle = io.popen(command)
    local result = handle:read("*a")
    handle:close()
    
    -- Parse result
    if result and result:match("https://temp.sh/") then
        return {success = true, url = result:match("https://temp.sh/[%w]+")}
    else
        return {success = false, error = "Upload failed: " .. (result or "Unknown error")}
    end
end

return {
    upload_file = upload_file
}