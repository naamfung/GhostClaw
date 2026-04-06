package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/toon-format/toon-go"
	lua "github.com/yuin/gopher-lua"
)

// Plugin 表示一个已加载的 Lua 插件
type Plugin struct {
	Name     string
	L        *lua.LState
	mu       sync.Mutex
	FilePath string // 持久化文件路径
	Code     string // 原始代码
	Enabled  bool
}

// PluginManager 管理所有插件
type PluginManager struct {
	plugins      map[string]*Plugin
	mu           sync.RWMutex
	pluginsDir   string
	toolExecutor func(ctx context.Context, toolName string, args map[string]interface{}) (string, error)
}

// NewPluginManager 创建并初始化插件管理器
func NewPluginManager(dir string) *PluginManager {
	pm := &PluginManager{
		plugins:    make(map[string]*Plugin),
		pluginsDir: dir,
	}
	// 只有在目录路径非空时才创建
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("Warning: failed to create plugins dir %s: %v", dir, err)
		}
	}
	return pm
}

// SetToolExecutor 设置工具执行回调，供插件调用主程序工具
func (pm *PluginManager) SetToolExecutor(executor func(ctx context.Context, toolName string, args map[string]interface{}) (string, error)) {
	pm.toolExecutor = executor
}

// LoadPluginsFromDir 加载 pluginsDir 下所有子文件夹中的 <name>.lua 入口文件
func (pm *PluginManager) LoadPluginsFromDir() error {
	entries, err := os.ReadDir(pm.pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginName := entry.Name()
		entryFilePath := filepath.Join(pm.pluginsDir, pluginName, pluginName+".lua")
		if _, err := os.Stat(entryFilePath); os.IsNotExist(err) {
			continue
		}
		code, err := os.ReadFile(entryFilePath)
		if err != nil {
			log.Printf("Failed to read plugin %s: %v", pluginName, err)
			continue
		}
		if err := pm.LoadPlugin(pluginName, string(code), entryFilePath); err != nil {
			log.Printf("Failed to load plugin %s: %v", pluginName, err)
		} else {
			log.Printf("Loaded plugin: %s", pluginName)
		}
	}
	return nil
}

// LoadPlugin 加载或覆盖插件
func (pm *PluginManager) LoadPlugin(name, code string, filePath string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// 如果已存在，先卸载
	if old, ok := pm.plugins[name]; ok {
		old.Close()
		delete(pm.plugins, name)
	}

	// 创建新的 Lua 虚拟机
	L := lua.NewState()
	pm.registerAPIs(L, name)

	// 设置 package.path 以便 require 同目录下文件
	if filePath != "" {
		pluginDir := filepath.Dir(filePath)
		L.SetGlobal("package", L.GetGlobal("package"))
		if err := L.DoString(fmt.Sprintf("package.path = package.path .. ';%s/?.lua'", pluginDir)); err != nil {
			log.Printf("Warning: failed to set package.path: %v", err)
		}
	}

	// 执行代码
	if err := L.DoString(code); err != nil {
		L.Close()
		return fmt.Errorf("execute plugin code failed: %w", err)
	}

	// 检查是否有返回值（通常是一个包含函数的表）
	top := L.GetTop()
	if top > 0 {
		// 如果返回的是一个表，将其设置为全局变量
		val := L.Get(top)
		if val.Type() == lua.LTTable {
			// 将表设置为全局变量，这样CallPluginFunction可以通过GetGlobal访问其中的函数
			L.SetGlobal(name, val)
		}
		// 弹出所有返回值，保持栈平衡
		L.Pop(top)
	}

	plugin := &Plugin{
		Name:     name,
		L:        L,
		FilePath: filePath,
		Code:     code,
		Enabled:  true,
	}
	pm.plugins[name] = plugin

	// 持久化到磁盘
	if filePath == "" {
		pluginDir := filepath.Join(pm.pluginsDir, name)
		if err := os.MkdirAll(pluginDir, 0755); err != nil {
			return fmt.Errorf("failed to create plugin directory: %w", err)
		}
		filePath = filepath.Join(pluginDir, name+".lua")
		plugin.FilePath = filePath
	}
	if err := os.WriteFile(filePath, []byte(code), 0644); err != nil {
		log.Printf("Warning: failed to write plugin %s to disk: %v", name, err)
	}

	return nil
}

// UnloadPlugin 卸载插件（仅内存）
func (pm *PluginManager) UnloadPlugin(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	p, ok := pm.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %s not found", name)
	}
	p.Close()
	delete(pm.plugins, name)
	return nil
}

// DeletePlugin 完全删除插件（包括文件夹和文件）
func (pm *PluginManager) DeletePlugin(name string) error {
	// 先卸载
	if err := pm.UnloadPlugin(name); err != nil {
		// 如果插件未加载，仍尝试删除文件夹
	}
	// 删除文件夹
	pluginDir := filepath.Join(pm.pluginsDir, name)
	if err := os.RemoveAll(pluginDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete plugin directory: %w", err)
	}
	return nil
}

// ReloadPlugin 重载插件（从原始文件）
func (pm *PluginManager) ReloadPlugin(name string) error {
	pm.mu.RLock()
	p, ok := pm.plugins[name]
	pm.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin %s not found", name)
	}
	// 从磁盘重新读取文件
	if p.FilePath == "" {
		return fmt.Errorf("plugin %s has no file path", name)
	}
	data, err := os.ReadFile(p.FilePath)
	if err != nil {
		return fmt.Errorf("failed to read plugin file: %w", err)
	}
	return pm.LoadPlugin(name, string(data), p.FilePath)
}

// CallPluginFunction 调用插件中的函数
func (pm *PluginManager) CallPluginFunction(ctx context.Context, name, funcName string, args ...interface{}) (string, error) {
	pm.mu.RLock()
	p, ok := pm.plugins[name]
	pm.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("plugin %s not found", name)
	}
	if !p.Enabled {
		return "", fmt.Errorf("plugin %s is disabled", name)
	}

	// 从插件返回的表中获取函数
	p.mu.Lock()
	defer p.mu.Unlock()

	// 直接调用插件函数
	// 先获取插件表
	pluginTable := p.L.GetGlobal(name)
	if pluginTable.Type() != lua.LTTable {
		return "", fmt.Errorf("plugin %s not found or not a table", name)
	}

	// 再获取函数
	funcValue := p.L.GetField(pluginTable, funcName)
	if funcValue.Type() != lua.LTFunction {
		return "", fmt.Errorf("function %s not found in plugin %s", funcName, name)
	}

	// 压入函数
	p.L.Push(funcValue)

	// 压入参数
	for _, arg := range args {
		p.L.Push(toLuaValue(p.L, arg))
	}

	// 调用函数
	err := p.L.PCall(len(args), lua.MultRet, nil)
	if err != nil {
		return "", err
	}

	// 收集返回值
	nRet := p.L.GetTop()
	result := make([]lua.LValue, nRet)
	for i := 0; i < nRet; i++ {
		result[i] = p.L.Get(i + 1)
	}

	// 弹出返回值
	p.L.Pop(nRet)

	// 构建返回字符串
	if len(result) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for i, v := range result {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(luaValueToString(v))
	}
	return sb.String(), nil
}

// ListPlugins 返回所有插件信息
func (pm *PluginManager) ListPlugins() []map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	list := make([]map[string]interface{}, 0, len(pm.plugins))
	for name, p := range pm.plugins {
		list = append(list, map[string]interface{}{
			"name":    name,
			"enabled": p.Enabled,
			"file":    p.FilePath,
		})
	}
	return list
}

// CompilePlugin 编译Lua代码进行语法检查（不实际执行）
func (pm *PluginManager) CompilePlugin(name, code string) error {
	L := lua.NewState()
	defer L.Close()
	fn, err := L.LoadString(code)
	if err != nil {
		return fmt.Errorf("compilation failed: %w", err)
	}
	_ = fn
	return nil
}

// registerAPIs 向 Lua VM 注册 ghostclaw 命名空间的函数
func (pm *PluginManager) registerAPIs(L *lua.LState, pluginName string) {
	ghostclaw := L.NewTable()
	L.SetGlobal("ghostclaw", ghostclaw)

	// ==================== 基础功能 ====================

	// ghostclaw.log(level, msg) - 日志输出
	L.SetField(ghostclaw, "log", L.NewFunction(func(L *lua.LState) int {
		level := L.CheckString(1)
		msg := L.CheckString(2)
		log.Printf("[plugin %s] %s: %s", pluginName, level, msg)
		return 0
	}))

	// ghostclaw.call_tool(name, args) - 调用主程序工具
	L.SetField(ghostclaw, "call_tool", L.NewFunction(func(L *lua.LState) int {
		toolName := L.CheckString(1)
		argsTable := L.CheckTable(2)
		args := make(map[string]interface{})
		argsTable.ForEach(func(k, v lua.LValue) {
			key := luaValueToString(k)
			args[key] = luaValueToGo(v)
		})
		if pm.toolExecutor == nil {
			L.Push(lua.LString("error: tool executor not available"))
			return 1
		}
		result, err := pm.toolExecutor(context.Background(), toolName, args)
		if err != nil {
			L.Push(lua.LString(fmt.Sprintf("error: %v", err)))
		} else {
			L.Push(lua.LString(result))
		}
		return 1
	}))

	// ==================== 文件系统 ====================

	// ghostclaw.read_file(path) - 读取文件内容
	L.SetField(ghostclaw, "read_file", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		data, err := os.ReadFile(path)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LString(string(data)))
		return 1
	}))

	// ghostclaw.write_file(path, content) - 写入文件
	L.SetField(ghostclaw, "write_file", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		content := L.CheckString(2)
		err := os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LBool(true))
		return 1
	}))

	// ghostclaw.list_dir(path) - 列出目录内容
	L.SetField(ghostclaw, "list_dir", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		entries, err := os.ReadDir(path)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		table := L.CreateTable(len(entries), 0)
		for i, entry := range entries {
			item := L.CreateTable(0, 3)
			item.RawSetString("name", lua.LString(entry.Name()))
			item.RawSetString("is_dir", lua.LBool(entry.IsDir()))
			info, _ := entry.Info()
			if info != nil {
				item.RawSetString("size", lua.LNumber(info.Size()))
			}
			table.RawSetInt(i+1, item)
		}
		L.Push(table)
		return 1
	}))

	// ghostclaw.upload_multipart(path, url, method, file_field) - 使用multipart/form-data上传文件
	L.SetField(ghostclaw, "upload_multipart", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		url := L.CheckString(2)
		method := L.CheckString(3)
		file_field := L.CheckString(4)

		// 检查文件是否存在
		_, err := os.Stat(path)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("File not found: " + path))
			return 2
		}

		// 读取文件内容
		data, err := os.ReadFile(path)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error reading file: " + err.Error()))
			return 2
		}

		// 使用 multipart/form-data 格式
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		fw, err := w.CreateFormFile(file_field, filepath.Base(path))
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error creating form file: " + err.Error()))
			return 2
		}
		if _, err := fw.Write(data); err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error writing file data: " + err.Error()))
			return 2
		}
		w.Close()

		req, err := http.NewRequest(method, url, &buf)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error creating request: " + err.Error()))
			return 2
		}
		req.Header.Set("Content-Type", w.FormDataContentType())

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error uploading: " + err.Error()))
			return 2
		}
		defer resp.Body.Close()

		// 读取响应
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error reading response: " + err.Error()))
			return 2
		}

		// 直接返回完整的响应
		L.Push(lua.LBool(true))
		L.Push(lua.LString(string(body)))
		return 2
	}))

	// ghostclaw.upload_raw(path, url, method, content_type) - 使用原始数据上传文件
	L.SetField(ghostclaw, "upload_raw", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		url := L.CheckString(2)
		method := L.CheckString(3)
		content_type := L.CheckString(4)

		// 检查文件是否存在
		_, err := os.Stat(path)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("File not found: " + path))
			return 2
		}

		// 读取文件内容
		data, err := os.ReadFile(path)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error reading file: " + err.Error()))
			return 2
		}

		// 使用原始数据上传
		client := &http.Client{}
		req, err := http.NewRequest(method, url, bytes.NewReader(data))
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error creating request: " + err.Error()))
			return 2
		}
		req.Header.Set("Content-Type", content_type)

		resp, err := client.Do(req)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error uploading: " + err.Error()))
			return 2
		}
		defer resp.Body.Close()

		// 读取响应
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error reading response: " + err.Error()))
			return 2
		}

		// 直接返回完整的响应
		L.Push(lua.LBool(true))
		L.Push(lua.LString(string(body)))
		return 2
	}))

	// ghostclaw.upload_file(path, url, method, content_type, file_field) - 合并的上传函数，根据参数自动选择上传方式
	L.SetField(ghostclaw, "upload_file", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		url := L.CheckString(2)
		method := L.CheckString(3)
		content_type := L.CheckString(4)
		var file_field string
		if L.GetTop() >= 5 && L.Get(5).Type() != lua.LTNil {
			file_field = L.CheckString(5)
		}

		// 检查文件是否存在
		_, err := os.Stat(path)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("File not found: " + path))
			return 2
		}

		// 读取文件内容
		data, err := os.ReadFile(path)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error reading file: " + err.Error()))
			return 2
		}

		var resp *http.Response
		if content_type == "multipart/form-data" && file_field != "" {
			// 使用 multipart/form-data 格式
			var buf bytes.Buffer
			w := multipart.NewWriter(&buf)
			fw, err := w.CreateFormFile(file_field, filepath.Base(path))
			if err != nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString("Error creating form file: " + err.Error()))
				return 2
			}
			if _, err := fw.Write(data); err != nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString("Error writing file data: " + err.Error()))
				return 2
			}
			w.Close()
			req, err := http.NewRequest(method, url, &buf)
			if err != nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString("Error creating request: " + err.Error()))
				return 2
			}
			req.Header.Set("Content-Type", w.FormDataContentType())
			client := &http.Client{}
			resp, err = client.Do(req)
			if err != nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString("Error uploading: " + err.Error()))
				return 2
			}
		} else {
			// 使用原始数据上传
			client := &http.Client{}
			req, err := http.NewRequest(method, url, bytes.NewReader(data))
			if err != nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString("Error creating request: " + err.Error()))
				return 2
			}
			req.Header.Set("Content-Type", content_type)
			resp, err = client.Do(req)
			if err != nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString("Error uploading: " + err.Error()))
				return 2
			}
		}

		defer resp.Body.Close()

		// 读取响应
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error reading response: " + err.Error()))
			return 2
		}

		// 直接返回完整的响应
		L.Push(lua.LBool(true))
		L.Push(lua.LString(string(body)))
		return 2
	}))

	// ghostclaw.download_file(url, save_path, headers) - 下载文件到本地
	L.SetField(ghostclaw, "download_file", L.NewFunction(func(L *lua.LState) int {
		url := L.CheckString(1)
		save_path := L.CheckString(2)

		// 创建HTTP请求（固定使用GET方法）
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error creating request: " + err.Error()))
			return 2
		}

		// 设置自定义HTTP头部（如果有）
		if L.GetTop() >= 3 && L.Get(3).Type() == lua.LTTable {
			headersTable := L.CheckTable(3)
			headersTable.ForEach(func(key, value lua.LValue) {
				if k, ok := key.(lua.LString); ok {
					if v, ok := value.(lua.LString); ok {
						req.Header.Set(string(k), string(v))
					}
				}
			})
		}

		// 发送请求
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error downloading: " + err.Error()))
			return 2
		}
		defer resp.Body.Close()

		// 检查响应状态
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(fmt.Sprintf("HTTP error: %d", resp.StatusCode)))
			return 2
		}

		// 创建保存文件的目录（如果需要）
		save_dir := filepath.Dir(save_path)
		if save_dir != "." && save_dir != "" {
			if err := os.MkdirAll(save_dir, 0755); err != nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString("Error creating directory: " + err.Error()))
				return 2
			}
		}

		// 创建保存文件
		out, err := os.Create(save_path)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error creating file: " + err.Error()))
			return 2
		}
		defer out.Close()

		// 写入文件内容
		_, err = io.Copy(out, resp.Body)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("Error writing file: " + err.Error()))
			return 2
		}

		L.Push(lua.LBool(true))
		L.Push(lua.LString("File downloaded successfully"))
		return 2
	}))

	// ghostclaw.stat(path) - 获取文件信息
	L.SetField(ghostclaw, "stat", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		info, err := os.Stat(path)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		table := L.CreateTable(0, 5)
		table.RawSetString("name", lua.LString(info.Name()))
		table.RawSetString("size", lua.LNumber(info.Size()))
		table.RawSetString("is_dir", lua.LBool(info.IsDir()))
		table.RawSetString("mod_time", lua.LString(info.ModTime().Format(time.RFC3339)))
		table.RawSetString("mode", lua.LString(info.Mode().String()))
		L.Push(table)
		return 1
	}))

	// ghostclaw.exists(path) - 检查文件是否存在
	L.SetField(ghostclaw, "exists", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		_, err := os.Stat(path)
		L.Push(lua.LBool(err == nil))
		return 1
	}))

	// ghostclaw.mkdir(path) - 创建目录
	L.SetField(ghostclaw, "mkdir", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		err := os.MkdirAll(path, 0755)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LBool(true))
		return 1
	}))

	// ghostclaw.remove(path) - 删除文件或目录
	L.SetField(ghostclaw, "remove", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		err := os.RemoveAll(path)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LBool(true))
		return 1
	}))

	// ghostclaw.rename(old_path, new_path) - 重命名/移动
	L.SetField(ghostclaw, "rename", L.NewFunction(func(L *lua.LState) int {
		oldPath := L.CheckString(1)
		newPath := L.CheckString(2)
		err := os.Rename(oldPath, newPath)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LBool(true))
		return 1
	}))

	// ==================== JSON ====================

	// ghostclaw.json_encode(table) - 编码为JSON
	L.SetField(ghostclaw, "json_encode", L.NewFunction(func(L *lua.LState) int {
		table := L.CheckTable(1)
		data := luaValueToGo(table)
		bytes, err := json.Marshal(data)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LString(string(bytes)))
		return 1
	}))

	// ghostclaw.json_decode(str) - 解码JSON
	L.SetField(ghostclaw, "json_decode", L.NewFunction(func(L *lua.LState) int {
		str := L.CheckString(1)
		var data interface{}
		err := json.Unmarshal([]byte(str), &data)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(toLuaValue(L, data))
		return 1
	}))

	// ==================== HTTP ====================

	// ghostclaw.http_get(url) - HTTP GET请求（自动 SSRF 检查）
	L.SetField(ghostclaw, "http_get", L.NewFunction(func(L *lua.LState) int {
		url := L.CheckString(1)
		// 使用安全 HTTP 客户端（自动 SSRF 检查），超时使用配置值
		timeout := globalTimeoutConfig.Plugin
		if timeout <= 0 {
			timeout = 30
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
		defer cancel()
		resp, err := SafeHTTPGet(ctx, url)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		result := L.CreateTable(0, 3)
		result.RawSetString("status_code", lua.LNumber(resp.StatusCode))
		result.RawSetString("status", lua.LString(resp.Status))
		result.RawSetString("body", lua.LString(string(body)))
		L.Push(result)
		return 1
	}))

	// ghostclaw.http_post(url, body, content_type) - HTTP POST请求（自动 SSRF 检查）
	L.SetField(ghostclaw, "http_post", L.NewFunction(func(L *lua.LState) int {
		url := L.CheckString(1)
		body := L.CheckString(2)
		contentType := L.OptString(3, "application/json")
		timeout := globalTimeoutConfig.Plugin
		if timeout <= 0 {
			timeout = 30
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
		defer cancel()
		resp, err := SafeHTTPPost(ctx, url, strings.NewReader(body), contentType)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		result := L.CreateTable(0, 3)
		result.RawSetString("status_code", lua.LNumber(resp.StatusCode))
		result.RawSetString("status", lua.LString(resp.Status))
		result.RawSetString("body", lua.LString(string(respBody)))
		L.Push(result)
		return 1
	}))

	// ==================== 时间 ====================

	// ghostclaw.time() - 当前时间戳
	L.SetField(ghostclaw, "time", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LNumber(time.Now().Unix()))
		return 1
	}))

	// ghostclaw.time_format(timestamp, layout) - 格式化时间
	L.SetField(ghostclaw, "time_format", L.NewFunction(func(L *lua.LState) int {
		timestamp := L.CheckNumber(1)
		layout := L.OptString(2, "2006-01-02 15:04:05")
		t := time.Unix(int64(timestamp), 0)
		L.Push(lua.LString(t.Format(layout)))
		return 1
	}))

	// ghostclaw.time_parse(str, layout) - 解析时间字符串
	L.SetField(ghostclaw, "time_parse", L.NewFunction(func(L *lua.LState) int {
		str := L.CheckString(1)
		layout := L.OptString(2, "2006-01-02 15:04:05")
		t, err := time.Parse(layout, str)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LNumber(t.Unix()))
		return 1
	}))

	// ghostclaw.sleep(seconds) - 休眠
	L.SetField(ghostclaw, "sleep", L.NewFunction(func(L *lua.LState) int {
		seconds := L.CheckNumber(1)
		time.Sleep(time.Duration(float64(seconds) * float64(time.Second)))
		return 0
	}))

	// ==================== 加密/哈希 ====================

	// ghostclaw.hash(algo, data) - 哈希计算
	L.SetField(ghostclaw, "hash", L.NewFunction(func(L *lua.LState) int {
		algo := L.CheckString(1)
		data := L.CheckString(2)
		var hash []byte
		switch strings.ToLower(algo) {
		case "md5":
			h := md5.Sum([]byte(data))
			hash = h[:]
		case "sha1":
			h := sha1.Sum([]byte(data))
			hash = h[:]
		case "sha256":
			h := sha256.Sum256([]byte(data))
			hash = h[:]
		default:
			L.Push(lua.LNil)
			L.Push(lua.LString("unsupported algorithm: " + algo))
			return 2
		}
		L.Push(lua.LString(hex.EncodeToString(hash)))
		return 1
	}))

	// ==================== 随机数/UUID ====================

	// ghostclaw.random(min, max) - 随机数
	L.SetField(ghostclaw, "random", L.NewFunction(func(L *lua.LState) int {
		min := float64(L.OptNumber(1, 0))
		max := float64(L.OptNumber(2, 1))
		r := float64(time.Now().UnixNano()%1000000) / 1000000.0
		result := min + r*(max-min)
		L.Push(lua.LNumber(result))
		return 1
	}))

	// ghostclaw.uuid() - 生成UUID
	L.SetField(ghostclaw, "uuid", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString(uuid.New().String()))
		return 1
	}))

	// ==================== 环境变量 ====================

	// ghostclaw.getenv(name) - 获取环境变量
	L.SetField(ghostclaw, "getenv", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		value := os.Getenv(name)
		if value == "" {
			L.Push(lua.LNil)
		} else {
			L.Push(lua.LString(value))
		}
		return 1
	}))

	// ghostclaw.setenv(name, value) - 设置环境变量
	L.SetField(ghostclaw, "setenv", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		value := L.CheckString(2)
		err := os.Setenv(name, value)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LBool(true))
		return 1
	}))

	// ==================== 工作目录 ====================

	// ghostclaw.getcwd() - 获取当前工作目录
	L.SetField(ghostclaw, "getcwd", L.NewFunction(func(L *lua.LState) int {
		dir, err := os.Getwd()
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LString(dir))
		return 1
	}))

	// ghostclaw.chdir(path) - 切换工作目录
	L.SetField(ghostclaw, "chdir", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		err := os.Chdir(path)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LBool(true))
		return 1
	}))

	// ==================== 路径操作 ====================

	// ghostclaw.join_path(parts...) - 连接路径
	L.SetField(ghostclaw, "join_path", L.NewFunction(func(L *lua.LState) int {
		n := L.GetTop()
		parts := make([]string, n)
		for i := 1; i <= n; i++ {
			parts[i-1] = L.CheckString(i)
		}
		L.Push(lua.LString(filepath.Join(parts...)))
		return 1
	}))

	// ghostclaw.split_path(path) - 分割路径
	L.SetField(ghostclaw, "split_path", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		dir, file := filepath.Split(path)
		table := L.CreateTable(0, 2)
		table.RawSetString("dir", lua.LString(dir))
		table.RawSetString("file", lua.LString(file))
		L.Push(table)
		return 1
	}))

	// ghostclaw.abs_path(path) - 获取绝对路径
	L.SetField(ghostclaw, "abs_path", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		abs, err := filepath.Abs(path)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LString(abs))
		return 1
	}))

	// ==================== 字符串增强 ====================

	// ghostclaw.split(str, sep) - 分割字符串
	L.SetField(ghostclaw, "split", L.NewFunction(func(L *lua.LState) int {
		str := L.CheckString(1)
		sep := L.CheckString(2)
		parts := strings.Split(str, sep)
		table := L.CreateTable(len(parts), 0)
		for i, part := range parts {
			table.RawSetInt(i+1, lua.LString(part))
		}
		L.Push(table)
		return 1
	}))

	// ghostclaw.trim(str) - 去除首尾空白
	L.SetField(ghostclaw, "trim", L.NewFunction(func(L *lua.LState) int {
		str := L.CheckString(1)
		L.Push(lua.LString(strings.TrimSpace(str)))
		return 1
	}))

	// ghostclaw.contains(str, substr) - 检查是否包含子串
	L.SetField(ghostclaw, "contains", L.NewFunction(func(L *lua.LState) int {
		str := L.CheckString(1)
		substr := L.CheckString(2)
		L.Push(lua.LBool(strings.Contains(str, substr)))
		return 1
	}))

	// ghostclaw.replace(str, old, new) - 替换字符串
	L.SetField(ghostclaw, "replace", L.NewFunction(func(L *lua.LState) int {
		str := L.CheckString(1)
		old := L.CheckString(2)
		newStr := L.CheckString(3)
		L.Push(lua.LString(strings.ReplaceAll(str, old, newStr)))
		return 1
 	}))

	// ==================== TOON 格式处理 ====================

	// ghostclaw.toon_encode(table) - 将 Lua 表编码为 TOON 格式
	L.SetField(ghostclaw, "toon_encode", L.NewFunction(func(L *lua.LState) int {
		table := L.CheckTable(1)
		data := luaValueToGo(table)
		bytes, err := toon.Marshal(data)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LString(string(bytes)))
		return 1
	}))

	// ghostclaw.toon_decode(str) - 将 TOON 格式解码为 Lua 表
	L.SetField(ghostclaw, "toon_decode", L.NewFunction(func(L *lua.LState) int {
		str := L.CheckString(1)
		var data interface{}
		err := toon.Unmarshal([]byte(str), &data)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(toLuaValue(L, data))
		return 1
	}))

	// ghostclaw.toon_read_file(path) - 读取 TOON 文件并解码为 Lua 表
	L.SetField(ghostclaw, "toon_read_file", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		data, err := os.ReadFile(path)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		var result interface{}
		err = toon.Unmarshal(data, &result)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(toLuaValue(L, result))
		return 1
	}))

	// ghostclaw.toon_write_file(path, table) - 将 Lua 表编码为 TOON 格式并写入文件
	L.SetField(ghostclaw, "toon_write_file", L.NewFunction(func(L *lua.LState) int {
		path := L.CheckString(1)
		table := L.CheckTable(2)
		data := luaValueToGo(table)
		bytes, err := toon.Marshal(data)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(err.Error()))
			return 2
		}
		err = os.WriteFile(path, bytes, 0644)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LBool(true))
		return 1
	}))
}

// Close 关闭所有插件 VM
func (pm *PluginManager) Close() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, p := range pm.plugins {
		p.Close()
	}
	pm.plugins = make(map[string]*Plugin)
}

// Close 关闭插件自身
func (p *Plugin) Close() {
	if p.L != nil {
		p.L.Close()
		p.L = nil
	}
}

// ==================== 辅助函数 ====================

func toLuaValue(L *lua.LState, v interface{}) lua.LValue {
	switch val := v.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(val)
	case int:
		return lua.LNumber(val)
	case int64:
		return lua.LNumber(val)
	case float64:
		return lua.LNumber(val)
	case string:
		return lua.LString(val)
	case []interface{}:
		table := L.CreateTable(len(val), 0)
		for i, item := range val {
			table.RawSetInt(i+1, toLuaValue(L, item))
		}
		return table
	case map[string]interface{}:
		table := L.CreateTable(0, len(val))
		for k, item := range val {
			table.RawSetString(k, toLuaValue(L, item))
		}
		return table
	default:
		return lua.LString(fmt.Sprintf("%v", val))
	}
}

func luaValueToGo(v lua.LValue) interface{} {
	switch v.Type() {
	case lua.LTNil:
		return nil
	case lua.LTBool:
		return lua.LVAsBool(v)
	case lua.LTNumber:
		return float64(lua.LVAsNumber(v))
	case lua.LTString:
		return lua.LVAsString(v)
	case lua.LTTable:
		tbl := v.(*lua.LTable)
		isArray := true
		maxIdx := 0
		tbl.ForEach(func(k, val lua.LValue) {
			if k.Type() != lua.LTNumber {
				isArray = false
			} else {
				if idx := int(lua.LVAsNumber(k)); idx > maxIdx {
					maxIdx = idx
				}
			}
		})
		if isArray && maxIdx > 0 {
			arr := make([]interface{}, maxIdx)
			tbl.ForEach(func(k, val lua.LValue) {
				idx := int(lua.LVAsNumber(k)) - 1
				if idx >= 0 && idx < maxIdx {
					arr[idx] = luaValueToGo(val)
				}
			})
			return arr
		}
		m := make(map[string]interface{})
		tbl.ForEach(func(k, val lua.LValue) {
			key := luaValueToString(k)
			m[key] = luaValueToGo(val)
		})
		return m
	default:
		return luaValueToString(v)
	}
}

func luaValueToString(v lua.LValue) string {
	switch v.Type() {
	case lua.LTNil:
		return "nil"
	case lua.LTBool:
		if lua.LVAsBool(v) {
			return "true"
		}
		return "false"
	case lua.LTNumber:
		return fmt.Sprintf("%v", float64(lua.LVAsNumber(v)))
	case lua.LTString:
		return lua.LVAsString(v)
	case lua.LTTable:
		tbl := v.(*lua.LTable)
		var sb strings.Builder
		sb.WriteString("{")
		first := true
		tbl.ForEach(func(k, val lua.LValue) {
			if !first {
				sb.WriteString(", ")
			}
			first = false
			sb.WriteString(luaValueToString(k))
			sb.WriteString(": ")
			sb.WriteString(luaValueToString(val))
		})
		sb.WriteString("}")
		return sb.String()
	case lua.LTFunction:
		return "<function>"
	default:
		return fmt.Sprintf("%v", v)
	}
}
