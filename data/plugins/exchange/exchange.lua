-- Exchange Rate Plugin for GarClaw
-- Description: 查詢全球貨幣匯率，使用 Frankfurter 免費 API
-- Version: 2.1.0
-- API: Frankfurter (https://www.frankfurter.app) - 免費、無需 API Key

-- 常用貨幣信息
local CURRENCIES = {
    USD = { name = "美元", symbol = "$" },
    CNY = { name = "人民幣", symbol = "¥" },
    HKD = { name = "港幣", symbol = "HK$" },
    TWD = { name = "新台幣", symbol = "NT$" },
    EUR = { name = "歐元", symbol = "€" },
    GBP = { name = "英鎊", symbol = "£" },
    JPY = { name = "日圓", symbol = "¥" },
    KRW = { name = "韓圓", symbol = "₩" },
    AUD = { name = "澳元", symbol = "A$" },
    CAD = { name = "加元", symbol = "C$" },
    CHF = { name = "瑞士法郎", symbol = "Fr" },
    SGD = { name = "新加坡元", symbol = "S$" },
    THB = { name = "泰銖", symbol = "฿" },
    MYR = { name = "馬來西亞令吉", symbol = "RM" },
    INR = { name = "印度盧比", symbol = "₹" },
    RUB = { name = "俄羅斯盧布", symbol = "₽" },
    BRL = { name = "巴西雷亞爾", symbol = "R$" },
    ZAR = { name = "南非蘭特", symbol = "R" },
    NZD = { name = "新西蘭元", symbol = "NZ$" },
    SEK = { name = "瑞典克朗", symbol = "kr" },
    NOK = { name = "挪威克朗", symbol = "kr" },
    DKK = { name = "丹麥克朗", symbol = "kr" },
    PHP = { name = "菲律賓披索", symbol = "₱" },
    IDR = { name = "印尼盾", symbol = "Rp" },
    VND = { name = "越南盾", symbol = "₫" }
}

-- 匯率快取
local rate_cache = {
    data = nil,
    timestamp = 0,
    ttl = 3600  -- 快取有效期：1小時
}

-- 安全獲取表中的值
local function safe_get(tbl, key, default)
    if tbl == nil then return default end
    local val = tbl[key]
    if val == nil then return default end
    return val
end

-- 獲取貨幣名稱
local function get_currency_name(code)
    local info = CURRENCIES[code]
    if info then
        return info.name .. " (" .. code .. ")"
    end
    return code
end

-- 獲取貨幣符號
local function get_currency_symbol(code)
    local info = CURRENCIES[code]
    if info then
        return info.symbol
    end
    return ""
end

-- 驗證貨幣代碼格式
local function is_valid_currency_code(code)
    if not code or type(code) ~= "string" then return false end
    return string.match(string.upper(code), "^[A-Z][A-Z][A-Z]$") ~= nil
end

-- 獲取最新匯率
function get_latest(base_currency)
    base_currency = string.upper(base_currency or "USD")
    
    -- 驗證貨幣代碼
    if not is_valid_currency_code(base_currency) then
        return "錯誤: 無效的貨幣代碼格式，應為3個字母（如 USD, CNY, EUR）"
    end
    
    garclaw.log("info", "查詢最新匯率，基準貨幣: " .. base_currency)
    
    -- 構建 URL
    local url
    if base_currency == "EUR" then
        url = "https://api.frankfurter.app/latest"
    else
        url = string.format("https://api.frankfurter.app/latest?from=%s", base_currency)
    end
    
    garclaw.log("info", "Exchange API: " .. url)
    
    local resp = garclaw.http_get(url)
    if not resp then
        return "錯誤: 無法連接匯率服務"
    end
    
    if resp.status_code ~= 200 then
        return "錯誤: 匯率服務返回錯誤: " .. tostring(resp.status_code)
    end
    
    local data = garclaw.json_decode(resp.body)
    if not data then
        return "錯誤: 無法解析匯率響應"
    end
    
    if not data.rates then
        return "錯誤: 匯率數據格式不正確"
    end
    
    -- 更新快取
    rate_cache.data = data
    rate_cache.timestamp = os.time()
    
    -- 格式化輸出
    local result = string.format([[💱 最新匯率查詢
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 基準貨幣: %s
📅 更新時間: %s
━━━━━━━━━━━━━━━━━━━━━━━━━━━━

主要貨幣匯率:
]],
        get_currency_name(safe_get(data, "base", base_currency)),
        safe_get(data, "date", os.date("%Y-%m-%d"))
    )
    
    -- 主要貨幣列表
    local main_currencies = {"USD", "CNY", "EUR", "GBP", "JPY", "HKD", "TWD", "KRW", "SGD", "AUD", "CAD", "CHF"}
    
    for _, code in ipairs(main_currencies) do
        if code ~= data.base then
            local rate = data.rates[code]
            if rate then
                result = result .. string.format("  %-8s %s = %.4f %s\n", 
                    "1 " .. data.base, get_currency_name(code), rate, code)
            end
        end
    end
    
    result = result .. [[
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 數據來源: Frankfurter API
💡 使用 convert("金額", "來源貨幣", "目標貨幣") 進行換算]]
    
    return result
end

-- 貨幣換算
function convert(amount, from_currency, to_currency)
    amount = tonumber(amount) or 1
    from_currency = string.upper(from_currency or "USD")
    to_currency = string.upper(to_currency or "CNY")
    
    -- 驗證貨幣代碼
    if not is_valid_currency_code(from_currency) then
        return "錯誤: 無效的來源貨幣代碼: " .. tostring(from_currency)
    end
    if not is_valid_currency_code(to_currency) then
        return "錯誤: 無效的目標貨幣代碼: " .. tostring(to_currency)
    end
    
    if from_currency == to_currency then
        return string.format("💱 %g %s = %g %s", amount, get_currency_name(from_currency), amount, get_currency_name(to_currency))
    end
    
    garclaw.log("info", string.format("換算: %g %s -> %s", amount, from_currency, to_currency))
    
    -- 構建 URL
    local url = string.format(
        "https://api.frankfurter.app/latest?amount=%g&from=%s&to=%s",
        amount, from_currency, to_currency
    )
    
    garclaw.log("info", "Convert API: " .. url)
    
    local resp = garclaw.http_get(url)
    if not resp then
        return "錯誤: 無法連接匯率服務"
    end
    
    if resp.status_code ~= 200 then
        return "錯誤: 匯率服務返回錯誤: " .. tostring(resp.status_code)
    end
    
    local data = garclaw.json_decode(resp.body)
    if not data then
        return "錯誤: 無法解析匯率響應"
    end
    
    if not data.rates then
        return "錯誤: 匯率數據格式不正確"
    end
    
    local result_rate = data.rates[to_currency]
    if not result_rate then
        return "錯誤: 不支持的貨幣代碼: " .. to_currency
    end
    
    local from_symbol = get_currency_symbol(from_currency)
    local to_symbol = get_currency_symbol(to_currency)
    
    local result = string.format([[💱 貨幣換算
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
💰 %g %s %s
   ↓
💰 %.4f %s %s
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📈 匯率: 1 %s = %.4f %s
📅 日期: %s
📊 數據來源: Frankfurter API]],
        amount, from_symbol, get_currency_name(from_currency),
        result_rate, to_symbol, get_currency_name(to_currency),
        from_currency, result_rate / amount, to_currency,
        safe_get(data, "date", os.date("%Y-%m-%d"))
    )
    
    return result
end

-- 獲取歷史匯率
function get_historical(date_str, base_currency)
    base_currency = string.upper(base_currency or "USD")
    
    -- 驗證貨幣代碼
    if not is_valid_currency_code(base_currency) then
        return "錯誤: 無效的貨幣代碼格式，應為3個字母（如 USD, CNY, EUR）"
    end
    
    -- 驗證日期格式
    if not date_str or not string.match(date_str, "^%d%d%d%d%-%d%d%-%d%d$") then
        return "錯誤: 日期格式應為 YYYY-MM-DD，例如: 2024-01-15"
    end
    
    garclaw.log("info", string.format("查詢歷史匯率: %s, 基準: %s", date_str, base_currency))
    
    -- 構建 URL
    local url = string.format(
        "https://api.frankfurter.app/%s?from=%s",
        date_str, base_currency
    )
    
    garclaw.log("info", "Historical API: " .. url)
    
    local resp = garclaw.http_get(url)
    if not resp then
        return "錯誤: 無法連接匯率服務"
    end
    
    if resp.status_code ~= 200 then
        return "錯誤: 匯率服務返回錯誤: " .. tostring(resp.status_code) .. "，可能是日期超出範圍"
    end
    
    local data = garclaw.json_decode(resp.body)
    if not data then
        return "錯誤: 無法解析歷史匯率響應"
    end
    
    if not data.rates then
        return "錯誤: 歷史匯率數據格式不正確，可能是日期超出範圍"
    end
    
    local result = string.format([[💱 歷史匯率查詢
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📅 日期: %s
📊 基準貨幣: %s
━━━━━━━━━━━━━━━━━━━━━━━━━━━━

主要貨幣匯率:
]],
        date_str,
        get_currency_name(safe_get(data, "base", base_currency))
    )
    
    local main_currencies = {"USD", "CNY", "EUR", "GBP", "JPY", "HKD", "TWD", "KRW", "SGD"}
    
    for _, code in ipairs(main_currencies) do
        if code ~= data.base then
            local rate = data.rates[code]
            if rate then
                result = result .. string.format("  %-8s %s = %.4f %s\n", 
                    "1 " .. data.base, get_currency_name(code), rate, code)
            end
        end
    end
    
    result = result .. [[━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 數據來源: Frankfurter API
💡 提示: 可查詢 1999 年至今的歷史匯率]]
    
    return result
end

-- 獲取匯率走勢（一段時間內的匯率變化）
function get_trend(from_currency, to_currency, days)
    from_currency = string.upper(from_currency or "USD")
    to_currency = string.upper(to_currency or "CNY")
    days = tonumber(days) or 7
    if days > 30 then days = 30 end
    if days < 1 then days = 1 end
    
    -- 驗證貨幣代碼
    if not is_valid_currency_code(from_currency) then
        return "錯誤: 無效的來源貨幣代碼: " .. tostring(from_currency)
    end
    if not is_valid_currency_code(to_currency) then
        return "錯誤: 無效的目標貨幣代碼: " .. tostring(to_currency)
    end
    
    garclaw.log("info", string.format("查詢匯率走勢: %s/%s, %d 天", from_currency, to_currency, days))
    
    -- 計算日期範圍
    local end_date = os.time()
    local start_date = end_date - (days * 24 * 60 * 60)
    
    local start_str = os.date("%Y-%m-%d", start_date)
    local end_str = os.date("%Y-%m-%d", end_date)
    
    -- 構建 URL
    local url = string.format(
        "https://api.frankfurter.app/%s..%s?from=%s&to=%s",
        start_str, end_str, from_currency, to_currency
    )
    
    garclaw.log("info", "Trend API: " .. url)
    
    local resp = garclaw.http_get(url)
    if not resp then
        return "錯誤: 無法連接匯率服務"
    end
    
    if resp.status_code ~= 200 then
        return "錯誤: 匯率服務返回錯誤: " .. tostring(resp.status_code)
    end
    
    local data = garclaw.json_decode(resp.body)
    if not data then
        return "錯誤: 無法解析匯率走勢響應"
    end
    
    if not data.rates then
        return "錯誤: 匯率走勢數據格式不正確"
    end
    
    -- 收集匯率數據
    local rates = {}
    local dates = {}
    for date, rate_data in pairs(data.rates) do
        if type(rate_data) == "table" then
            table.insert(dates, date)
        end
    end
    table.sort(dates)
    
    for _, date in ipairs(dates) do
        local rate_data = data.rates[date]
        if rate_data and rate_data[to_currency] then
            table.insert(rates, {date = date, rate = rate_data[to_currency]})
        end
    end
    
    if #rates == 0 then
        return "錯誤: 無匯率數據，請檢查貨幣代碼是否正確"
    end
    
    -- 計算統計
    local min_rate = rates[1].rate
    local max_rate = rates[1].rate
    local sum = 0
    
    for _, r in ipairs(rates) do
        if r.rate < min_rate then min_rate = r.rate end
        if r.rate > max_rate then max_rate = r.rate end
        sum = sum + r.rate
    end
    
    local avg_rate = sum / #rates
    local first_rate = rates[1].rate
    local last_rate = rates[#rates].rate
    local change = last_rate - first_rate
    local change_percent = (change / first_rate) * 100
    
    local trend_emoji = change >= 0 and "📈" or "📉"
    local change_sign = change >= 0 and "+" or ""
    
    local result = string.format([[💱 匯率走勢分析
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 貨幣對: %s / %s
📅 時間範圍: %s 至 %s (%d 天)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
%s 走勢: %s%.4f (%s%.2f%%)

統計數據:
  • 期初: %.4f
  • 期末: %.4f
  • 最高: %.4f
  • 最低: %.4f
  • 平均: %.4f
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 數據來源: Frankfurter API]],
        get_currency_name(from_currency),
        get_currency_name(to_currency),
        start_str, end_str, #rates,
        trend_emoji, change_sign, change, change_sign, change_percent,
        first_rate, last_rate, max_rate, min_rate, avg_rate
    )
    
    return result
end

-- 列出支持的貨幣
function list_currencies()
    local result = [[💱 支持的貨幣列表
━━━━━━━━━━━━━━━━━━━━━━━━━━━━

常用貨幣:
]]
    
    local codes = {}
    for code, _ in pairs(CURRENCIES) do
        table.insert(codes, code)
    end
    table.sort(codes)
    
    for i, code in ipairs(codes) do
        local info = CURRENCIES[code]
        result = result .. string.format("  %-4s %-15s %s\n", code, info.name, info.symbol)
        if i % 5 == 0 then
            result = result .. "\n"
        end
    end
    
    result = result .. [[
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
💡 提示: 更多貨幣請參考 ISO 4217 標準
📊 數據來源: Frankfurter API (支持 30+ 種貨幣)]]
    
    return result
end

-- 幫助信息
function help()
    return [[💱 匯率插件使用說明
━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📋 可用函數:

1. get_latest("基準貨幣")
   獲取最新匯率
   示例: get_latest("USD")
         get_latest("EUR")

2. convert(金額, "來源貨幣", "目標貨幣")
   貨幣換算
   示例: convert(100, "USD", "CNY")
         convert(1000, "HKD", "TWD")

3. get_historical("日期", "基準貨幣")
   查詢歷史匯率
   示例: get_historical("2024-01-15", "USD")

4. get_trend("來源貨幣", "目標貨幣", 天數)
   獲取匯率走勢 (最多30天)
   示例: get_trend("USD", "CNY", 7)

5. list_currencies()
   列出支持的貨幣

6. help()
   顯示此幫助信息

━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 數據來源: Frankfurter API (免費)
🌐 支持全球 30+ 種主要貨幣
📅 歷史數據可追溯至 1999 年]]
end
