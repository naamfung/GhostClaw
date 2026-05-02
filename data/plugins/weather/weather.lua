-- Weather Plugin for GarClaw
-- Description: 查詢全球城市天氣資訊，使用 Open-Meteo 免費 API
-- Version: 2.1.0
-- API: Open-Meteo (https://open-meteo.com) - 免費、無需 API Key

-- 城市地理編碼快取
local city_cache = {}

-- 天氣代碼描述 (WMO 天氣代碼) - 必須在其他函數之前定義
local weather_descriptions = {
    [0] = "☀️ 晴天",
    [1] = "🌤️ 大部晴朗",
    [2] = "⛅ 多雲",
    [3] = "☁️ 陰天",
    [45] = "🌫️ 霧",
    [48] = "🌫️ 凍霧",
    [51] = "🌧️ 小毛毛雨",
    [53] = "🌧️ 毛毛雨",
    [55] = "🌧️ 大毛毛雨",
    [56] = "🌨️ 凍毛毛雨",
    [57] = "🌨️ 凍毛毛雨",
    [61] = "🌧️ 小雨",
    [63] = "🌧️ 中雨",
    [65] = "🌧️ 大雨",
    [66] = "🌨️ 凍雨",
    [67] = "🌨️ 凍雨",
    [71] = "🌨️ 小雪",
    [73] = "🌨️ 中雪",
    [75] = "🌨️ 大雪",
    [77] = "🌨️ 雪粒",
    [80] = "🌧️ 小陣雨",
    [81] = "🌧️ 陣雨",
    [82] = "🌧️ 大陣雨",
    [85] = "🌨️ 小陣雪",
    [86] = "🌨️ 大陣雪",
    [95] = "⛈️ 雷暴",
    [96] = "⛈️ 雷暴伴小冰雹",
    [99] = "⛈️ 雷暴伴大冰雹"
}

-- 風向描述
local wind_directions = {"北", "東北", "東", "東南", "南", "西南", "西", "西北"}

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

-- 天氣代碼轉描述 (WMO 天氣代碼)
local function get_weather_description(code)
    if code == nil then return "❓ 未知" end
    return weather_descriptions[code] or "❓ 未知"
end

-- 風向角度轉描述
local function get_wind_direction(degrees)
    if degrees == nil then return "" end
    -- 確保角度在 0-360 範圍內
    degrees = degrees % 360
    local index = math.floor((degrees + 22.5) / 45)
    if index > 7 then index = 0 end
    index = index + 1  -- Lua 數組從 1 開始
    return "(" .. wind_directions[index] .. "風)"
end

-- EAQI 等級描述
local function get_aqi_level(aqi)
    if aqi == nil then return "未知" end
    if aqi <= 20 then return "👍 優"
    elseif aqi <= 40 then return "😊 良"
    elseif aqi <= 60 then return "😐 中等"
    elseif aqi <= 80 then return "😷 差"
    elseif aqi <= 100 then return "😷 很差"
    else return "🚨 極差" end
end

-- 安全獲取表中的值
local function safe_get(tbl, key, default)
    if tbl == nil then return default end
    local val = tbl[key]
    if val == nil then return default end
    return val
end

-- 地理編碼：將城市名轉換為經緯度
local function geocode(city_name)
    if not city_name or city_name == "" then
        return nil, "城市名不能為空"
    end
    
    -- 檢查快取
    if city_cache[city_name] then
        return city_cache[city_name]
    end
    
    -- URL 編碼城市名
    local encoded_city = url_encode(city_name)
    
    -- 呼叫 Open-Meteo 地理編碼 API
    local url = string.format(
        "https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1&language=zh&format=json",
        encoded_city
    )
    
    garclaw.log("info", "Geocoding: " .. url)
    
    local resp = garclaw.http_get(url)
    if not resp then
        return nil, "無法連接地理編碼服務"
    end
    
    if resp.status_code ~= 200 then
        return nil, "地理編碼服務返回錯誤: " .. tostring(resp.status_code)
    end
    
    local data = garclaw.json_decode(resp.body)
    if not data then
        return nil, "無法解析地理編碼響應"
    end
    
    local results = data.results
    if not results or type(results) ~= "table" or #results == 0 then
        return nil, "找不到城市: " .. city_name
    end
    
    local result = results[1]
    if not result then
        return nil, "城市結果格式錯誤"
    end
    
    local location = {
        name = safe_get(result, "name", city_name),
        country = safe_get(result, "country", ""),
        latitude = safe_get(result, "latitude", 0),
        longitude = safe_get(result, "longitude", 0),
        timezone = safe_get(result, "timezone", "auto")
    }
    
    -- 存入快取
    city_cache[city_name] = location
    
    return location
end

-- 獲取當前天氣
function get_weather(city_name)
    city_name = city_name or "北京"
    
    garclaw.log("info", "查詢天氣: " .. city_name)
    
    -- 地理編碼
    local location, err = geocode(city_name)
    if not location then
        return "錯誤: " .. (err or "未知錯誤")
    end
    
    -- 呼叫天氣 API
    local url = string.format(
        "https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,relative_humidity_2m,apparent_temperature,weather_code,wind_speed_10m,wind_direction_10m&timezone=%s",
        location.latitude,
        location.longitude,
        url_encode(location.timezone)
    )
    
    garclaw.log("info", "Weather API: " .. url)
    
    local resp = garclaw.http_get(url)
    if not resp then
        return "錯誤: 無法連接天氣服務"
    end
    
    if resp.status_code ~= 200 then
        return "錯誤: 天氣服務返回錯誤: " .. tostring(resp.status_code)
    end
    
    local data = garclaw.json_decode(resp.body)
    if not data then
        return "錯誤: 無法解析天氣數據"
    end
    
    local current = data.current
    if not current then
        return "錯誤: 天氣數據格式不正確"
    end
    
    -- 安全獲取各項數據
    local temp = safe_get(current, "temperature_2m", 0)
    local apparent = safe_get(current, "apparent_temperature", temp)
    local humidity = safe_get(current, "relative_humidity_2m", 0)
    local wind_speed = safe_get(current, "wind_speed_10m", 0)
    local wind_dir_deg = current.wind_direction_10m  -- 可為 nil
    local weather_code = safe_get(current, "weather_code", 0)
    
    -- 天氣代碼描述
    local weather_desc = get_weather_description(weather_code)
    
    -- 風向轉換
    local wind_dir = get_wind_direction(wind_dir_deg)
    
    -- 格式化輸出
    local result = string.format([[🌤️ %s 天氣查詢結果
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 位置: %s, %s
🌡️ 溫度: %.1f°C (體感 %.1f°C)
💧 濕度: %d%%
🌬️ 風速: %.1f km/h %s
☁️ 天氣: %s
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 數據來源: Open-Meteo
⏰ 更新時間: %s]],
        city_name,
        location.name, location.country,
        temp,
        apparent,
        humidity,
        wind_speed,
        wind_dir,
        weather_desc,
        os.date("%Y-%m-%d %H:%M:%S")
    )
    
    return result
end

-- 獲取天氣預報
function get_forecast(city_name, days)
    city_name = city_name or "北京"
    days = days or 3
    if type(days) ~= "number" then days = tonumber(days) or 3 end
    if days > 7 then days = 7 end
    if days < 1 then days = 1 end
    
    garclaw.log("info", string.format("查詢 %s 未來 %d 天預報", city_name, days))
    
    -- 地理編碼
    local location, err = geocode(city_name)
    if not location then
        return "錯誤: " .. (err or "未知錯誤")
    end
    
    -- 呼叫天氣 API (包含每日預報)
    local url = string.format(
        "https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&daily=weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum,wind_speed_10m_max&timezone=%s&forecast_days=%d",
        location.latitude,
        location.longitude,
        url_encode(location.timezone),
        days
    )
    
    garclaw.log("info", "Forecast API: " .. url)
    
    local resp = garclaw.http_get(url)
    if not resp then
        return "錯誤: 無法連接天氣服務"
    end
    
    if resp.status_code ~= 200 then
        return "錯誤: 天氣服務返回錯誤: " .. tostring(resp.status_code)
    end
    
    local data = garclaw.json_decode(resp.body)
    if not data then
        return "錯誤: 無法解析預報數據"
    end
    
    local daily = data.daily
    if not daily then
        return "錯誤: 預報數據格式不正確"
    end
    
    -- 檢查數據完整性
    local time_arr = daily.time or {}
    local max_temps = daily.temperature_2m_max or {}
    local min_temps = daily.temperature_2m_min or {}
    local precips = daily.precipitation_sum or {}
    local winds = daily.wind_speed_10m_max or {}
    local codes = daily.weather_code or {}
    
    local actual_days = math.min(days, #time_arr)
    if actual_days == 0 then
        return "錯誤: 沒有可用的預報數據"
    end
    
    local result = string.format([[📅 %s 未來 %d 天天氣預報
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 位置: %s, %s
]],
        city_name, actual_days,
        location.name, location.country
    )
    
    for i = 1, actual_days do
        local date = safe_get(time_arr, i, "未知")
        local max_temp = safe_get(max_temps, i, 0)
        local min_temp = safe_get(min_temps, i, 0)
        local precip = safe_get(precips, i, 0)
        local wind = safe_get(winds, i, 0)
        local code = safe_get(codes, i, 0)
        local weather = get_weather_description(code)
        
        result = result .. string.format([[
📆 %s
   🌡️ %.1f°C ~ %.1f°C
   ☁️ %s | 🌧️ 降水: %.1fmm | 🌬️ 風速: %.1f km/h
]],
            date,
            min_temp, max_temp,
            weather, precip, wind
        )
    end
    
    result = result .. [[━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 數據來源: Open-Meteo]]
    
    return result
end

-- 獲取空氣質量 (需要另外的 API)
function get_air_quality(city_name)
    city_name = city_name or "北京"
    
    garclaw.log("info", "查詢空氣質量: " .. city_name)
    
    -- 地理編碼
    local location, err = geocode(city_name)
    if not location then
        return "錯誤: " .. (err or "未知錯誤")
    end
    
    -- 呼叫空氣質量 API
    local url = string.format(
        "https://air-quality-api.open-meteo.com/v1/air-quality?latitude=%.4f&longitude=%.4f&current=european_aqi,pm10,pm2_5,carbon_monoxide,nitrogen_dioxide,sulphur_dioxide,ozone&timezone=%s",
        location.latitude,
        location.longitude,
        url_encode(location.timezone)
    )
    
    garclaw.log("info", "Air Quality API: " .. url)
    
    local resp = garclaw.http_get(url)
    if not resp then
        return "錯誤: 無法連接空氣質量服務"
    end
    
    if resp.status_code ~= 200 then
        return "錯誤: 空氣質量服務返回錯誤: " .. tostring(resp.status_code)
    end
    
    local data = garclaw.json_decode(resp.body)
    if not data then
        return "錯誤: 無法解析空氣質量數據"
    end
    
    local current = data.current
    if not current then
        return "錯誤: 空氣質量數據格式不正確"
    end
    
    -- 安全獲取各項數據
    local eaqi = safe_get(current, "european_aqi", 0)
    local pm25 = safe_get(current, "pm2_5", 0)
    local pm10 = safe_get(current, "pm10", 0)
    local co = safe_get(current, "carbon_monoxide", 0)
    local no2 = safe_get(current, "nitrogen_dioxide", 0)
    local so2 = safe_get(current, "sulphur_dioxide", 0)
    local o3 = safe_get(current, "ozone", 0)
    
    local aqi_level = get_aqi_level(eaqi)
    
    local result = string.format([[🌬️ %s 空氣質量報告
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 位置: %s, %s
📊 歐洲空氣質量指數 (EAQI): %.0f - %s
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
微粒物:
  • PM2.5: %.1f μg/m³
  • PM10: %.1f μg/m³
氣體污染物:
  • 一氧化碳 (CO): %.1f μg/m³
  • 二氧化氮 (NO₂): %.1f μg/m³
  • 二氧化硫 (SO₂): %.1f μg/m³
  • 臭氧 (O₃): %.1f μg/m³
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 數據來源: Open-Meteo Air Quality
⏰ 更新時間: %s]],
        city_name,
        location.name, location.country,
        eaqi, aqi_level,
        pm25,
        pm10,
        co,
        no2,
        so2,
        o3,
        os.date("%Y-%m-%d %H:%M:%S")
    )
    
    return result
end

-- 幫助信息
function help()
    return [[🌤️ 天氣插件使用說明
━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📋 可用函數:

1. get_weather("城市名")
   查詢當前天氣
   示例: get_weather("北京")
         get_weather("Hong Kong")
         get_weather("廣州")

2. get_forecast("城市名", 天數)
   查詢未來天氣預報 (最多7天)
   示例: get_forecast("上海", 5)

3. get_air_quality("城市名")
   查詢空氣質量
   示例: get_air_quality("廣州")

4. help()
   顯示此幫助信息

━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 數據來源: Open-Meteo (免費開源API)
🌐 支援全球城市查詢
💡 提示: 可使用中英文城市名]]
end
