package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/toon-format/toon-go"
)

// executeTool 执行单个工具调用，返回增强消息
func executeTool(ctx context.Context, toolID, toolName string, argsMap map[string]interface{}, ch Channel, role *Role) EnrichedMessage {
	var content string
	status := TaskStatusSuccess

	if ctx.Err() == context.Canceled {
		return CancelToolResult(toolID, CancelByUser, "User cancelled before execution", toolName)
	}

	if role != nil && !role.IsToolAllowed(toolName) {
		errMsg := fmt.Sprintf("❌ 权限拒绝：当前角色「%s」无权使用工具「%s」。\n\n可用工具：%v",
			role.DisplayName, toolName, getAllowedToolsList(role))
		argsJSON, _ := json.Marshal(map[string]interface{}{"error": "permission denied"})
		sendToolCallStart(ch, toolName, string(argsJSON))
		ch.WriteChunk(StreamChunk{Content: errMsg + "\n"})
		sendToolCallStatus(ch, TaskStatusFailed)
		sendToolCallEnd(ch)
		return NewToolResultMessage(toolID, errMsg, TaskStatusFailed, toolName)
	}

	argsJSON, _ := json.Marshal(argsMap)
	sendToolCallStart(ch, toolName, string(argsJSON))
	defer sendToolCallEnd(ch)
	// 注意：defer 按注册逆序执行，所以 status 标签会在 END 标记之前写入
	// 使用闭包捕获 status 变量引用，确保返回时读取的是最终值
	defer func() {
		sendToolCallStatus(ch, status)
	}()

	switch toolName {
	case "smart_shell":
		content, _ = handleSmartShell(ctx, argsMap, ch)

	case "shell":
		command, ok := argsMap["command"].(string)
		if !ok || command == "" {
			content = "Error: Invalid or empty command"
			status = TaskStatusFailed
		} else {
			force := false
			if forceVal, ok := argsMap["force"].(bool); ok {
				force = forceVal
			}
			isBlockingConfirmed := false
			if confirmedVal, ok := argsMap["is_blocking_confirmed"].(bool); ok {
				isBlockingConfirmed = confirmedVal
			}

			result := runShellWithTimeout(ctx, command, force, isBlockingConfirmed)

			if result.ConfirmRequired {
				var confirmResult strings.Builder
				confirmResult.WriteString("⚠️ **确认请求**\n\n")
				confirmResult.WriteString(result.ConfirmMessage)
				confirmResult.WriteString("\n\n---\n")
				confirmResult.WriteString("要强制执行此命令，请使用: `shell(command=\"...\", force=true)`\n")
				confirmResult.WriteString("或使用建议的替代命令。")

				content = confirmResult.String()
				status = TaskStatusSuccess
			} else if result.Err != nil {
				if ctx.Err() == context.Canceled {
					return CancelToolResult(toolID, CancelByUser, "Command cancelled by user", toolName)
				} else {
					content = fmt.Sprintf("Error: %v", result.Err)
					status = TaskStatusFailed
				}
			} else {
				content = result.Stdout
				if result.ExitCode != 0 && result.Stderr != "" {
					content += "\n" + result.Stderr
					status = TaskStatusFailed
				}
			}
			fmt.Println(content)
		}

	case "ssh_connect":
		var err error
		content, err = handleSSHConnect(argsMap)
		if err != nil {
			content = err.Error()
			status = TaskStatusFailed
		}
	case "ssh_exec":
		content, status = handleSSHExec(ctx, argsMap, ch)
	case "ssh_list":
		var err error
		content, err = handleSSHList()
		if err != nil {
			content = err.Error()
			status = TaskStatusFailed
		}
	case "ssh_close":
		var err error
		content, err = handleSSHClose(argsMap)
		if err != nil {
			content = err.Error()
			status = TaskStatusFailed
		}

	case "read_file_line":
		filename, ok1 := argsMap["filename"].(string)
		lineNumFloat, ok2 := argsMap["line_num"].(float64)
		if !ok1 || !ok2 || filename == "" || lineNumFloat < 1 {
			content = "Error: Invalid arguments for read_file_line"
		} else {
			lineNum := int(lineNumFloat)
			c, err := ReadFileLine(filename, lineNum)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				// 检查是否需要详细信息
				verbose := false
				if v, ok := argsMap["verbose"].(bool); ok {
					verbose = v
				}

				if verbose {
					// 获取文件信息
					info, _ := os.Stat(filename)
					result := map[string]interface{}{
						"content":   c,
						"line":      lineNum,
						"filename":  filename,
						"encoding":  "utf-8", // 假设 UTF-8 编码
						"file_size": info.Size(),
						"modified":  info.ModTime().Format(time.RFC3339),
					}
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				} else {
					// 默认只返回内容
					content = c
				}
			}
			fmt.Println(TruncateString(content, 200))
		}

	case "write_file_line":
		filename, ok1 := argsMap["filename"].(string)
		lineNumFloat, ok2 := argsMap["line_num"].(float64)
		text, ok3 := argsMap["content"].(string)
		if !ok1 || !ok2 || !ok3 || filename == "" {
			content = "Error: Invalid arguments for write_file_line"
		} else {
			lineNum := int(lineNumFloat)
			if lineNum == 0 {
				// 创建空文件
				file, err := os.Create(filename)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					file.Close()
					content = "Successfully created empty file: " + filename
				}
			} else if lineNum < 0 {
				// 追加到文件末尾
				err := AppendFileLine(filename, text)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					content = "Successfully appended to end of file"
				}
			} else {
				// 写入指定行
				err := WriteFileLine(filename, lineNum, text)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					content = "Successfully wrote to line " + strconv.Itoa(lineNum)
				}
			}
			fmt.Println(content)
		}

	case "read_all_lines":
		filename, ok := argsMap["filename"].(string)
		if !ok || filename == "" {
			content = "Error: Invalid arguments for read_all_lines"
		} else {
			lines, err := ReadAllLines(filename)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				// 检查是否需要详细信息
				verbose := false
				if v, ok := argsMap["verbose"].(bool); ok {
					verbose = v
				}

				if verbose {
					// 获取文件信息
					info, _ := os.Stat(filename)

					// 构建带有行号的结果
					linedContent := make([]map[string]interface{}, len(lines))
					for i, line := range lines {
						linedContent[i] = map[string]interface{}{
							"line":    i + 1,
							"content": line,
						}
					}

					result := map[string]interface{}{
						"lines":       linedContent,
						"total_lines": len(lines),
						"filename":    filename,
						"encoding":    "utf-8", // 假设 UTF-8 编码
						"file_size":   info.Size(),
						"modified":    info.ModTime().Format(time.RFC3339),
					}

					resultTOON, err := toon.Marshal(result)
					if err != nil {
						content = "Error: " + err.Error()
					} else {
						content = string(resultTOON)
					}
				} else {
					// 默认只返回内容列表
					resultTOON, err := toon.Marshal(lines)
					if err != nil {
						content = "Error: " + err.Error()
					} else {
						content = string(resultTOON)
					}
				}
			}
			fmt.Println(TruncateString(content, 200))
		}

	case "write_all_lines":
		filename, ok1 := argsMap["filename"].(string)
		linesInterface, ok2 := argsMap["lines"].([]interface{})
		if !ok1 || !ok2 || filename == "" {
			content = "Error: Invalid arguments for write_all_lines"
		} else {
			lines := make([]string, len(linesInterface))
			valid := true
			for i, line := range linesInterface {
				if lineStr, ok := line.(string); ok {
					lines[i] = lineStr
				} else {
					content = fmt.Sprintf("Error: line %d is not a string", i)
					valid = false
					break
				}
			}
			if valid {
				appendMode := false
				if appendVal, ok := argsMap["append"].(bool); ok {
					appendMode = appendVal
				}

				var err error
				if appendMode {
					err = AppendAllLines(filename, lines)
				} else {
					err = WriteAllLines(filename, lines)
				}

				if err != nil {
					content = "Error: " + err.Error()
				} else {
					if appendMode {
						content = "Successfully appended " + strconv.Itoa(len(lines)) + " lines to " + filename
					} else {
						content = "Successfully wrote " + strconv.Itoa(len(lines)) + " lines to " + filename
					}
				}
				fmt.Println(content)
			}
		}

	case "append_to_file":
		filename, ok1 := argsMap["filename"].(string)
		contentStr, ok2 := argsMap["content"].(string)
		if !ok1 || !ok2 || filename == "" {
			content = "Error: Invalid arguments for append_to_file"
		} else {
			lineBreak := true
			if lineBreakVal, ok := argsMap["line_break"].(bool); ok {
				lineBreak = lineBreakVal
			}

			file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				defer file.Close()
				writer := bufio.NewWriter(file)
				_, err := writer.WriteString(contentStr)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					if lineBreak {
						_, err = writer.WriteString("\n")
						if err != nil {
							content = "Error: " + err.Error()
						}
					}
					if err := writer.Flush(); err != nil {
						content = "Error: " + err.Error()
					} else {
						content = "Successfully appended content to " + filename
					}
				}
			}
			fmt.Println(content)
		}

	case "write_file_range":
		filename, ok1 := argsMap["filename"].(string)
		startLineFloat, ok2 := argsMap["start_line"].(float64)
		contentStr, ok3 := argsMap["content"].(string)
		if !ok1 || !ok2 || !ok3 || filename == "" || startLineFloat < 1 {
			content = "Error: Invalid arguments for write_file_range"
		} else {
			startLine := int(startLineFloat)
			endLine := startLine
			if endLineFloat, ok := argsMap["end_line"].(float64); ok && endLineFloat >= float64(startLine) {
				endLine = int(endLineFloat)
			}

			err := WriteFileRange(filename, startLine, endLine, contentStr)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				if startLine == endLine {
					content = "Successfully wrote to line " + strconv.Itoa(startLine)
				} else {
					content = "Successfully wrote to lines " + strconv.Itoa(startLine) + "-" + strconv.Itoa(endLine)
				}
			}
			fmt.Println(content)
		}

	case "browser_search":
		keyword, ok := argsMap["keyword"].(string)
		if !ok || keyword == "" {
			content = "Error: Empty keyword in browser_search tool call"
		} else {
			resultsList, err := Search(keyword)
			if err != nil {
				content = "Error: " + err.Error()
			} else if resultsList != nil {
				resultsTOON, err := toon.Marshal(resultsList)
				if err != nil {
					content = "Error: Failed to marshal search results"
					log.Printf("Failed to marshal search results: %v", err)
				} else {
					content = string(resultsTOON)
				}
			} else {
				content = "No search results found"
			}
			fmt.Println("Browser search completed")
		}

	case "browser_visit":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_visit tool call"
		} else {
			result, err := Visit(url)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				resultTOON, err := toon.Marshal(result)
				if err != nil {
					content = "Error: Failed to marshal visit result"
				} else {
					content = string(resultTOON)
				}
			}
			fmt.Println("Browser visit completed")
		}

	case "browser_download":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_download tool call"
		} else {
			fileName, err := Download(url)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				content = "Browser download completed, saved to: " + fileName
			}
			fmt.Println(content)
		}

	// ========== 浏览器增强工具 ==========
	case "browser_click":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_click tool call"
		} else {
			selector, ok := argsMap["selector"].(string)
			if !ok || selector == "" {
				content = "Error: Empty selector in browser_click tool call"
			} else {
				timeout := 0
				if t, ok := argsMap["timeout"].(float64); ok {
					timeout = int(t)
				}
				// 修正：传递 sessionID
				result, err := BrowserClick(ch.GetSessionID(), url, selector, timeout)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser click completed")
			}
		}

	case "browser_type":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_type tool call"
		} else {
			selector, ok := argsMap["selector"].(string)
			if !ok || selector == "" {
				content = "Error: Empty selector in browser_type tool call"
			} else {
				text, ok := argsMap["text"].(string)
				if !ok {
					content = "Error: Empty text in browser_type tool call"
				} else {
					submit, _ := argsMap["submit"].(bool)
					timeout := 0
					if t, ok := argsMap["timeout"].(float64); ok {
						timeout = int(t)
					}
					// 修正：传递 sessionID
					result, err := BrowserType(ch.GetSessionID(), url, selector, text, submit, timeout)
					if err != nil {
						content = "Error: " + err.Error()
					} else {
						resultTOON, _ := toon.Marshal(result)
						content = string(resultTOON)
					}
					fmt.Println("Browser type completed")
				}
			}
		}

	case "browser_scroll":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_scroll tool call"
		} else {
			direction, ok := argsMap["direction"].(string)
			if !ok || direction == "" {
				content = "Error: Empty direction in browser_scroll tool call"
			} else {
				amount := 500
				if a, ok := argsMap["amount"].(float64); ok {
					amount = int(a)
				}
				timeout := 0
				if t, ok := argsMap["timeout"].(float64); ok {
					timeout = int(t)
				}
				// 修正：传递 sessionID
				result, err := BrowserScroll(ch.GetSessionID(), url, direction, amount, timeout)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser scroll completed")
			}
		}

	case "browser_wait_element":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_wait_element tool call"
		} else {
			selector, ok := argsMap["selector"].(string)
			if !ok || selector == "" {
				content = "Error: Empty selector in browser_wait_element tool call"
			} else {
				timeout := 10
				if t, ok := argsMap["timeout"].(float64); ok {
					timeout = int(t)
				}
				// 修正：传递 sessionID
				result, err := BrowserWaitElement(ch.GetSessionID(), url, selector, timeout)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser wait element completed")
			}
		}

	case "browser_extract_links":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_extract_links tool call"
		} else {
			timeout := 0
			if t, ok := argsMap["timeout"].(float64); ok {
				timeout = int(t)
			}
			// 修正：传递 sessionID
			result, err := BrowserExtractLinks(ch.GetSessionID(), url, timeout)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				resultTOON, _ := toon.Marshal(result)
				content = string(resultTOON)
			}
			fmt.Println("Browser extract links completed")
		}

	case "browser_extract_images":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_extract_images tool call"
		} else {
			timeout := 0
			if t, ok := argsMap["timeout"].(float64); ok {
				timeout = int(t)
			}
			// 修正：传递 sessionID
			result, err := BrowserExtractImages(ch.GetSessionID(), url, timeout)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				resultTOON, _ := toon.Marshal(result)
				content = string(resultTOON)
			}
			fmt.Println("Browser extract images completed")
		}

	case "browser_extract_elements":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_extract_elements tool call"
		} else {
			selector, ok := argsMap["selector"].(string)
			if !ok || selector == "" {
				content = "Error: Empty selector in browser_extract_elements tool call"
			} else {
				includeHTML, _ := argsMap["include_html"].(bool)
				timeout := 0
				if t, ok := argsMap["timeout"].(float64); ok {
					timeout = int(t)
				}
				// 修正：传递 sessionID
				result, err := BrowserExtractElements(ch.GetSessionID(), url, selector, includeHTML, timeout)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser extract elements completed")
			}
		}

	case "browser_screenshot":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_screenshot tool call"
		} else {
			fullPage, _ := argsMap["full_page"].(bool)
			timeout := 0
			if t, ok := argsMap["timeout"].(float64); ok {
				timeout = int(t)
			}
			// 修正：传递 sessionID
			result, err := BrowserScreenshot(ch.GetSessionID(), url, fullPage, timeout)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				resultTOON, _ := toon.Marshal(result)
				content = string(resultTOON)
			}
			fmt.Println("Browser screenshot completed")
		}

	case "browser_execute_js":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_execute_js tool call"
		} else {
			script, ok := argsMap["script"].(string)
			if !ok || script == "" {
				content = "Error: Empty script in browser_execute_js tool call"
			} else {
				timeout := 0
				if t, ok := argsMap["timeout"].(float64); ok {
					timeout = int(t)
				}
				// 修正：传递 sessionID
				result, err := BrowserExecuteJS(ch.GetSessionID(), url, script, timeout)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser execute JS completed")
			}
		}

	case "browser_fill_form":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_fill_form tool call"
		} else {
			formDataRaw, ok := argsMap["form_data"].(map[string]interface{})
			if !ok {
				content = "Error: Invalid form_data in browser_fill_form tool call"
			} else {
				formData := make(FormData)
				for k, v := range formDataRaw {
					if strVal, ok := v.(string); ok {
						formData[k] = strVal
					}
				}
				submitSelector, _ := argsMap["submit_selector"].(string)
				timeout := 0
				if t, ok := argsMap["timeout"].(float64); ok {
					timeout = int(t)
				}
				// 修正：传递 sessionID
				result, err := BrowserFillForm(ch.GetSessionID(), url, formData, submitSelector, timeout)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser fill form completed")
			}
		}

	// ========== 浏览器高级工具 ==========
	case "browser_hover":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_hover tool call"
		} else {
			selector, ok := argsMap["selector"].(string)
			if !ok || selector == "" {
				content = "Error: Empty selector in browser_hover tool call"
			} else {
				// 修正：传递 sessionID
				result, err := BrowserHover(ch.GetSessionID(), url, selector)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser hover completed")
			}
		}

	case "browser_double_click":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_double_click tool call"
		} else {
			selector, ok := argsMap["selector"].(string)
			if !ok || selector == "" {
				content = "Error: Empty selector in browser_double_click tool call"
			} else {
				// 修正：传递 sessionID
				result, err := BrowserDoubleClick(ch.GetSessionID(), url, selector)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser double click completed")
			}
		}

	case "browser_right_click":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_right_click tool call"
		} else {
			selector, ok := argsMap["selector"].(string)
			if !ok || selector == "" {
				content = "Error: Empty selector in browser_right_click tool call"
			} else {
				// 修正：传递 sessionID
				result, err := BrowserRightClick(ch.GetSessionID(), url, selector)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser right click completed")
			}
		}

	case "browser_drag":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_drag tool call"
		} else {
			sourceSelector, ok := argsMap["source_selector"].(string)
			if !ok || sourceSelector == "" {
				content = "Error: Empty source_selector in browser_drag tool call"
			} else {
				targetSelector, ok := argsMap["target_selector"].(string)
				if !ok || targetSelector == "" {
					content = "Error: Empty target_selector in browser_drag tool call"
				} else {
					// 修正：传递 sessionID
					result, err := BrowserDrag(ch.GetSessionID(), url, sourceSelector, targetSelector)
					if err != nil {
						content = "Error: " + err.Error()
					} else {
						resultTOON, _ := toon.Marshal(result)
						content = string(resultTOON)
					}
					fmt.Println("Browser drag completed")
				}
			}
		}

	case "browser_wait_smart":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_wait_smart tool call"
		} else {
			selector, ok := argsMap["selector"].(string)
			if !ok || selector == "" {
				content = "Error: Empty selector in browser_wait_smart tool call"
			} else {
				opts := BrowserWaitForOptions{
					Visible: true,
				}
				if v, ok := argsMap["visible"].(bool); ok {
					opts.Visible = v
				}
				if v, ok := argsMap["interactable"].(bool); ok {
					opts.Interactable = v
				}
				if v, ok := argsMap["stable"].(bool); ok {
					opts.Stable = v
				}
				if t, ok := argsMap["timeout"].(float64); ok {
					opts.Timeout = int(t)
				}
				// 修正：传递 sessionID
				result, err := BrowserWaitForSmart(ch.GetSessionID(), url, selector, opts)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser smart wait completed")
			}
		}

	case "browser_navigate":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_navigate tool call"
		} else {
			action, ok := argsMap["action"].(string)
			if !ok || action == "" {
				content = "Error: Empty action in browser_navigate tool call"
			} else {
				var result *BrowserNavigateResult
				var err error
				switch action {
				case "back":
					result, err = BrowserNavigateBack(ch.GetSessionID(), url)
				case "forward":
					result, err = BrowserNavigateForward(ch.GetSessionID(), url)
				case "refresh":
					result, err = BrowserRefresh(ch.GetSessionID(), url)
				default:
					content = "Error: Invalid action: " + action
				}
				if err != nil {
					content = "Error: " + err.Error()
				} else if result != nil {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser navigate completed:", action)
			}
		}

	case "browser_get_cookies":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_get_cookies tool call"
		} else {
			// 修正：传递 sessionID
			result, err := BrowserGetCookies(ch.GetSessionID(), url)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				resultTOON, _ := toon.Marshal(result)
				content = string(resultTOON)
			}
			fmt.Println("Browser get cookies completed")
		}

	case "browser_cookie_save":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_cookie_save tool call"
		} else {
			filePath, _ := argsMap["file_path"].(string)
			// 修正：传递 sessionID
			result, err := BrowserCookieSave(ch.GetSessionID(), url, filePath)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				resultTOON, _ := toon.Marshal(result)
				content = string(resultTOON)
			}
			fmt.Println("Browser cookie save completed")
		}

	case "browser_cookie_load":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_cookie_load tool call"
		} else {
			filePath, ok := argsMap["file_path"].(string)
			if !ok || filePath == "" {
				content = "Error: Empty file_path in browser_cookie_load tool call"
			} else {
				// 修正：传递 sessionID
				result, err := BrowserCookieLoad(ch.GetSessionID(), url, filePath)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser cookie load completed")
			}
		}

	case "browser_snapshot":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_snapshot tool call"
		} else {
			maxDepth := 5
			if d, ok := argsMap["max_depth"].(float64); ok {
				maxDepth = int(d)
			}
			// 修正：传递 sessionID
			result, err := BrowserSnapshot(ch.GetSessionID(), url, maxDepth)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				resultTOON, _ := toon.Marshal(result)
				content = string(resultTOON)
			}
			fmt.Println("Browser snapshot completed")
		}

	case "browser_upload_file":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_upload_file tool call"
		} else {
			selector, ok := argsMap["selector"].(string)
			if !ok || selector == "" {
				content = "Error: Empty selector in browser_upload_file tool call"
			} else {
				filePathsRaw, ok := argsMap["file_paths"].([]interface{})
				if !ok {
					content = "Error: Invalid file_paths in browser_upload_file tool call"
				} else {
					var filePaths []string
					for _, p := range filePathsRaw {
						if s, ok := p.(string); ok {
							filePaths = append(filePaths, s)
						}
					}
					// 修正：传递 sessionID
					result, err := BrowserUploadFile(ch.GetSessionID(), url, selector, filePaths)
					if err != nil {
						content = "Error: " + err.Error()
					} else {
						resultTOON, _ := toon.Marshal(result)
						content = string(resultTOON)
					}
					fmt.Println("Browser upload file completed")
				}
			}
		}

	case "browser_select_option":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_select_option tool call"
		} else {
			selector, ok := argsMap["selector"].(string)
			if !ok || selector == "" {
				content = "Error: Empty selector in browser_select_option tool call"
			} else {
				valuesRaw, ok := argsMap["values"].([]interface{})
				if !ok {
					content = "Error: Invalid values in browser_select_option tool call"
				} else {
					var values []string
					for _, v := range valuesRaw {
						if s, ok := v.(string); ok {
							values = append(values, s)
						}
					}
					// 修正：传递 sessionID
					result, err := BrowserSelectOption(ch.GetSessionID(), url, selector, values)
					if err != nil {
						content = "Error: " + err.Error()
					} else {
						resultTOON, _ := toon.Marshal(result)
						content = string(resultTOON)
					}
					fmt.Println("Browser select option completed")
				}
			}
		}

	case "browser_key_press":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_key_press tool call"
		} else {
			keysRaw, ok := argsMap["keys"].([]interface{})
			if !ok {
				content = "Error: Invalid keys in browser_key_press tool call"
			} else {
				var keys []string
				for _, k := range keysRaw {
					if s, ok := k.(string); ok {
						keys = append(keys, s)
					}
				}
				// 修正：传递 sessionID
				result, err := BrowserKeyPress(ch.GetSessionID(), url, keys)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser key press completed")
			}
		}

	case "browser_element_screenshot":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_element_screenshot tool call"
		} else {
			selector, ok := argsMap["selector"].(string)
			if !ok || selector == "" {
				content = "Error: Empty selector in browser_element_screenshot tool call"
			} else {
				// 修正：传递 sessionID
				result, err := BrowserElementScreenshot(ch.GetSessionID(), url, selector)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser element screenshot completed")
			}
		}

	case "browser_pdf":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_pdf tool call"
		} else {
			timeout := 0
			if t, ok := argsMap["timeout"].(float64); ok {
				timeout = int(t)
			}
			// 修正：传递 sessionID
			result, err := BrowserPDF(ch.GetSessionID(), url, timeout)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				resultTOON, _ := toon.Marshal(result)
				content = string(resultTOON)
			}
			fmt.Println("Browser PDF export completed")
		}

	case "browser_pdf_from_file":
		filePath, ok := argsMap["file_path"].(string)
		if !ok || filePath == "" {
			content = "Error: Empty file_path in browser_pdf_from_file tool call"
		} else {
			// 修正：传递 sessionID
			result, err := BrowserPDFFromFile(ch.GetSessionID(), filePath)
			if err != nil {
				content = "Error: " + err.Error()
			} else {
				resultTOON, _ := toon.Marshal(result)
				content = string(resultTOON)
			}
			fmt.Println("Browser PDF from file completed")
		}

	case "browser_set_headers":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_set_headers tool call"
		} else {
			headersInterface, ok := argsMap["headers"].([]interface{})
			if !ok {
				content = "Error: Invalid headers in browser_set_headers tool call"
			} else {
				var headers []string
				for _, h := range headersInterface {
					if hStr, ok := h.(string); ok {
						headers = append(headers, hStr)
					}
				}
				// 修正：传递 sessionID
				result, err := BrowserSetHeaders(ch.GetSessionID(), url, headers)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser set headers completed")
			}
		}

	case "browser_set_user_agent":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_set_user_agent tool call"
		} else {
			userAgent, ok := argsMap["user_agent"].(string)
			if !ok || userAgent == "" {
				content = "Error: Empty user_agent in browser_set_user_agent tool call"
			} else {
				// 修正：传递 sessionID
				result, err := BrowserSetUserAgent(ch.GetSessionID(), url, userAgent)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser set user agent completed")
			}
		}

	case "browser_emulate_device":
		url, ok := argsMap["url"].(string)
		if !ok || url == "" {
			content = "Error: Empty url in browser_emulate_device tool call"
		} else {
			device, ok := argsMap["device"].(string)
			if !ok || device == "" {
				content = "Error: Empty device in browser_emulate_device tool call"
			} else {
				// 修正：传递 sessionID
				result, err := BrowserEmulateDevice(ch.GetSessionID(), url, device)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					resultTOON, _ := toon.Marshal(result)
					content = string(resultTOON)
				}
				fmt.Println("Browser emulate device completed")
			}
		}

	case "todo":
		itemsInterface, ok := argsMap["items"].([]interface{})
		if !ok {
			content = "Error: Invalid items in todo tool call"
		} else {
			var items []TodoItem
			valid := true
			for _, itemInterface := range itemsInterface {
				itemMap, ok := itemInterface.(map[string]interface{})
				if !ok {
					content = "Error: Invalid item format"
					valid = false
					break
				}
				item := TodoItem{}
				if id, ok := itemMap["id"].(string); ok {
					item.ID = id
				}
				if text, ok := itemMap["text"].(string); ok {
					item.Text = text
				} else {
					content = "Error: Item missing text"
					valid = false
					break
				}
				if status, ok := itemMap["status"].(string); ok {
					item.Status = status
				} else {
					content = "Error: Item missing status"
					valid = false
					break
				}
				items = append(items, item)
			}
			if valid {
				fmt.Println("Updating todo list...")
				output, err := TODO.Update(items)
				if err != nil {
					content = "Error: " + err.Error()
				} else {
					content = output
				}
				fmt.Println(content)
			}
		}

	case "cron_add":
		content, _ = handleCronAdd(ctx, argsMap, ch)
	case "cron_remove":
		content, _ = handleCronRemove(ctx, argsMap, ch)
	case "cron_list":
		content, _ = handleCronList(ctx, argsMap, ch)
	case "cron_status":
		content, _ = handleCronStatus(ctx, argsMap, ch)

	case "memory_save":
		content, _ = handleMemorySave(ctx, argsMap, ch)
	case "memory_recall":
		content, _ = handleMemoryRecall(ctx, argsMap, ch)
	case "memory_forget":
		content, _ = handleMemoryForget(ctx, argsMap, ch)
	case "memory_list":
		content, _ = handleMemoryList(ctx, argsMap, ch)

	case "profile_check":
		content, _ = handleProfileCheck(ctx, argsMap, ch)
	case "actor_identity_set":
		content, _ = handleActorIdentitySet(ctx, argsMap, ch)
	case "actor_identity_clear":
		content, _ = handleActorIdentityClear(ctx, argsMap, ch)
	case "profile_reload":
		content, _ = handleProfileReload(ctx, argsMap, ch)

	case "skill_list":
		if globalSkillManagerV2 == nil {
			content = "Error: skill manager v2 not initialized"
			status = TaskStatusFailed
		} else {
			req := SkillListRequest{}
			if page, ok := argsMap["page"].(float64); ok {
				req.Page = int(page)
			}
			if pageSize, ok := argsMap["page_size"].(float64); ok {
				req.PageSize = int(pageSize)
			}
			if search, ok := argsMap["search"].(string); ok {
				req.Search = search
			}
			if sortBy, ok := argsMap["sort_by"].(string); ok {
				req.SortBy = sortBy
			}
			if sortOrder, ok := argsMap["sort_order"].(string); ok {
				req.SortOrder = sortOrder
			}
			if context, ok := argsMap["context"].(string); ok {
				req.Context = context
			}
			if tags, ok := argsMap["tags"].([]interface{}); ok {
				for _, tag := range tags {
					if tagStr, ok := tag.(string); ok {
						req.Tags = append(req.Tags, tagStr)
					}
				}
			}
			if suggestOnly, ok := argsMap["suggest_only"].(bool); ok {
				req.SuggestOnly = suggestOnly
			}

			resp, err := globalSkillManagerV2.ListSkills(req)
			if err != nil {
				content = "Error: " + err.Error()
				status = TaskStatusFailed
			} else {
				// 如果有上下文，添加推荐
				if req.Context != "" {
					suggestions, _ := globalSkillManagerV2.EvolutionOptimizer().SuggestSkills(req.Context, 5)
					for _, s := range suggestions {
						resp.Suggestions = append(resp.Suggestions, s.SkillName)
					}
				}
				skillsTOON, err := toon.Marshal(resp)
				if err != nil {
					content = "Error: failed to marshal skills"
					status = TaskStatusFailed
				} else {
					content = string(skillsTOON)
				}
			}
			fmt.Println("Skill list completed")
		}

	case "skill_create":
		if globalSkillManagerV2 == nil {
			content = "Error: skill manager v2 not initialized"
			status = TaskStatusFailed
		} else {
			name, ok1 := argsMap["name"].(string)
			systemPrompt, ok2 := argsMap["system_prompt"].(string)
			if !ok1 || !ok2 || name == "" || systemPrompt == "" {
				content = "Error: missing required parameters (name and system_prompt)"
				status = TaskStatusFailed
			} else {
				skill := &Skill{
					Name:         name,
					DisplayName:  name,
					SystemPrompt: systemPrompt,
				}

				if description, ok := argsMap["description"].(string); ok {
					skill.Description = description
				}
				if triggerWords, ok := argsMap["trigger_words"].([]interface{}); ok {
					for _, tw := range triggerWords {
						if twStr, ok := tw.(string); ok && twStr != "" {
							skill.TriggerWords = append(skill.TriggerWords, twStr)
						}
					}
				}
				if tags, ok := argsMap["tags"].([]interface{}); ok {
					for _, tag := range tags {
						if tagStr, ok := tag.(string); ok && tagStr != "" {
							skill.Tags = append(skill.Tags, tagStr)
						}
					}
				}

				if err := globalSkillManagerV2.CreateSkill(skill); err != nil {
					content = "Error: " + err.Error()
					status = TaskStatusFailed
				} else {
					content = "Skill created successfully: " + name
				}
			}
		}

	case "skill_delete":
		if globalSkillManagerV2 == nil {
			content = "Error: skill manager v2 not initialized"
			status = TaskStatusFailed
		} else {
			name, ok := argsMap["name"].(string)
			if !ok || name == "" {
				content = "Error: missing required parameter 'name'"
				status = TaskStatusFailed
			} else {
				if err := globalSkillManagerV2.DeleteSkill(name); err != nil {
					content = "Error: " + err.Error()
					status = TaskStatusFailed
				} else {
					content = "Skill deleted successfully: " + name
				}
				fmt.Println("Skill delete completed")
			}
		}

	case "skill_get":
		if globalSkillManagerV2 == nil {
			content = "Error: skill manager v2 not initialized"
			status = TaskStatusFailed
		} else {
			name, ok := argsMap["name"].(string)
			if !ok || name == "" {
				content = "Error: missing required parameter 'name'"
				status = TaskStatusFailed
			} else {
				skill, err := globalSkillManagerV2.GetSkill(name)
				if err != nil {
					content = "Error: skill not found"
					status = TaskStatusFailed
				} else {
					skillTOON, err := toon.Marshal(skill)
					if err != nil {
						content = "Error: failed to marshal skill"
						status = TaskStatusFailed
					} else {
						content = string(skillTOON)
					}
				}
				fmt.Println("Skill get completed")
			}
		}

	case "skill_reload":
		if globalSkillManagerV2 == nil {
			content = "Error: skill manager v2 not initialized"
			status = TaskStatusFailed
		} else {
			if err := globalSkillManagerV2.Reload(); err != nil {
				content = "Error: " + err.Error()
				status = TaskStatusFailed
			} else {
				content = "Skills reloaded successfully"
			}
			fmt.Println("Skill reload completed")
		}

	case "skill_update":
		if globalSkillManagerV2 == nil {
			content = "Error: skill manager v2 not initialized"
			status = TaskStatusFailed
		} else {
			name, ok := argsMap["name"].(string)
			if !ok || name == "" {
				content = "Error: missing required parameter 'name'"
				status = TaskStatusFailed
			} else {
				updates := make(map[string]interface{})

				if displayName, ok := argsMap["display_name"].(string); ok && displayName != "" {
					updates["display_name"] = displayName
				}
				if description, ok := argsMap["description"].(string); ok && description != "" {
					updates["description"] = description
				}
				if systemPrompt, ok := argsMap["system_prompt"].(string); ok && systemPrompt != "" {
					updates["system_prompt"] = systemPrompt
				}
				if triggerWords, ok := argsMap["trigger_words"].([]interface{}); ok && len(triggerWords) > 0 {
					var triggers []string
					for _, tw := range triggerWords {
						if twStr, ok := tw.(string); ok && twStr != "" {
							triggers = append(triggers, twStr)
						}
					}
					updates["trigger_words"] = triggers
				}
				if tags, ok := argsMap["tags"].([]interface{}); ok && len(tags) > 0 {
					var tagList []string
					for _, tag := range tags {
						if tagStr, ok := tag.(string); ok && tagStr != "" {
							tagList = append(tagList, tagStr)
						}
					}
					updates["tags"] = tagList
				}

				if len(updates) == 0 {
					content = "No changes provided for skill: " + name
				} else {
					if err := globalSkillManagerV2.UpdateSkill(name, updates); err != nil {
						content = "Error: " + err.Error()
						status = TaskStatusFailed
					} else {
						content = "Skill updated successfully: " + name
					}
				}
				fmt.Println("Skill update completed")
			}
		}

	case "skill_suggest":
		if globalSkillManagerV2 == nil {
			content = "Error: skill manager v2 not initialized"
			status = TaskStatusFailed
		} else {
			context, ok := argsMap["context"].(string)
			if !ok || context == "" {
				content = "Error: missing required parameter 'context'"
				status = TaskStatusFailed
			} else {
				topK := 5
				if k, ok := argsMap["top_k"].(float64); ok {
					topK = int(k)
				}

				suggestions, err := globalSkillManagerV2.EvolutionOptimizer().SuggestSkills(context, topK)
				if err != nil {
					content = "Error: " + err.Error()
					status = TaskStatusFailed
				} else {
					suggestionsTOON, err := toon.Marshal(suggestions)
					if err != nil {
						content = "Error: failed to marshal suggestions"
						status = TaskStatusFailed
					} else {
						content = string(suggestionsTOON)
					}
				}
				fmt.Println("Skill suggest completed")
			}
		}

	case "skill_stats":
		if globalSkillManagerV2 == nil {
			content = "Error: skill manager v2 not initialized"
			status = TaskStatusFailed
		} else {
			stats, err := globalSkillManagerV2.EvolutionOptimizer().GetSkillStats()
			if err != nil {
				content = "Error: " + err.Error()
				status = TaskStatusFailed
			} else {
				statsTOON, err := toon.Marshal(stats)
				if err != nil {
					content = "Error: failed to marshal stats"
					status = TaskStatusFailed
				} else {
					content = string(statsTOON)
				}
			}
			fmt.Println("Skill stats completed")
		}

	case "skill_evaluate":
		if globalSkillManagerV2 == nil {
			content = "Error: skill manager v2 not initialized"
			status = TaskStatusFailed
		} else {
			name, ok := argsMap["name"].(string)
			if !ok || name == "" {
				content = "Error: missing required parameter 'name'"
				status = TaskStatusFailed
			} else {
				report, err := globalSkillManagerV2.EvolutionOptimizer().EvaluateSkillQuality(name)
				if err != nil {
					content = "Error: " + err.Error()
					status = TaskStatusFailed
				} else {
					reportTOON, err := toon.Marshal(report)
					if err != nil {
						content = "Error: failed to marshal report"
						status = TaskStatusFailed
					} else {
						content = string(reportTOON)
					}
				}
				fmt.Println("Skill evaluate completed")
			}
		}

	case "text_search":
		keyword, ok := argsMap["keyword"].(string)
		if !ok || keyword == "" {
			content = "Error: Empty keyword in text_search tool call"
			status = TaskStatusFailed
		} else {
			opts := TextSearchOptions{}
			if rootDir, ok := argsMap["root_dir"].(string); ok && rootDir != "" {
				opts.RootDir = rootDir
			}
			if filePattern, ok := argsMap["file_pattern"].(string); ok {
				opts.FilePattern = filePattern
			}
			if ignoreCase, ok := argsMap["ignore_case"].(bool); ok {
				opts.IgnoreCase = ignoreCase
			}
			if useRegex, ok := argsMap["use_regex"].(bool); ok {
				opts.UseRegex = useRegex
			}
			if maxDepth, ok := argsMap["max_depth"].(float64); ok {
				opts.MaxDepth = int(maxDepth)
			}
			if maxResults, ok := argsMap["max_results"].(float64); ok {
				opts.MaxResults = int(maxResults)
			}

			results, err := TextSearch(keyword, opts)
			if err != nil {
				content = "Error: " + err.Error()
				status = TaskStatusFailed
			} else if len(results) == 0 {
				content = "No matches found"
			} else {
				resultsTOON, err := toon.Marshal(results)
				if err != nil {
					content = "Error: Failed to marshal search results"
					status = TaskStatusFailed
				} else {
					content = string(resultsTOON)
				}
			}
			fmt.Printf("Text search completed: %d results\n", len(results))
		}

	case "text_replace":
		content, _ = handleTextReplace(ctx, argsMap, ch)
	case "text_grep":
		content, _ = handleTextSearch(ctx, argsMap, ch)
	case "text_transform":
		content, _ = handleTextTransform(ctx, argsMap, ch)

	case "plugin_create":
		content, _ = handlePluginCreate(ctx, argsMap, ch)
	case "plugin_list":
		content, _ = handlePluginList(ctx, argsMap, ch)
	case "plugin_load":
		content, _ = handlePluginLoad(ctx, argsMap, ch)
	case "plugin_unload":
		content, _ = handlePluginUnload(ctx, argsMap, ch)
	case "plugin_reload":
		content, _ = handlePluginReload(ctx, argsMap, ch)
	case "plugin_call":
		content, _ = handlePluginCall(ctx, argsMap, ch)
	case "plugin_compile":
		content, _ = handlePluginCompile(ctx, argsMap, ch)
	case "plugin_delete":
		content, _ = handlePluginDelete(ctx, argsMap, ch)

	case "shell_delayed":
		content, _ = handleDelayedExec(ctx, argsMap, ch)
	case "shell_delayed_check":
		content, _ = handleTaskCheck(ctx, argsMap, ch)
	case "shell_delayed_terminate":
		content, _ = handleTaskTerminate(ctx, argsMap, ch)
	case "shell_delayed_list":
		content, _ = handleTaskList(ctx, argsMap, ch)
	case "shell_delayed_wait":
		content, _ = handleTaskWait(ctx, argsMap, ch)
	case "shell_delayed_remove":
		content, _ = handleTaskRemove(ctx, argsMap, ch)

	case "spawn":
		content, _ = handleSpawn(ctx, argsMap, ch)
	case "spawn_check":
		content, _ = handleSpawnCheck(ctx, argsMap, ch)
	case "spawn_list":
		content, _ = handleSpawnList(ctx, argsMap, ch)
	case "spawn_cancel":
		content, _ = handleSpawnCancel(ctx, argsMap, ch)

	case "consolidate_memory":
		content, _ = HandleConsolidateMemory(argsMap)

	case "scheme_eval":
		expression, ok := argsMap["expression"].(string)
		if !ok || expression == "" {
			content = "Error: Invalid or empty expression"
			status = TaskStatusFailed
		} else {
			result, err := schemeEval(ctx, expression)
			if err != nil {
				content = fmt.Sprintf("Error: %v", err)
				status = TaskStatusFailed
			} else {
				content = result
			}
		}

	default:
		if strings.HasPrefix(toolName, "mcp_") && globalMCPClientManager != nil {
			result, err := globalMCPClientManager.CallTool(ctx, toolName, argsMap)
			if err != nil {
				content = fmt.Sprintf("Error: %v", err)
				status = TaskStatusFailed
			} else {
				content = result
			}
		} else {
			content = "Error: Unknown tool name"
			status = TaskStatusFailed
		}
	}

	if status == TaskStatusSuccess && (strings.HasPrefix(content, "Error:") || strings.HasPrefix(content, "error:")) {
		status = TaskStatusFailed
	}

	content = sanitizeContent(content)
	if content != "" {
		ch.WriteChunk(StreamChunk{Content: content + "\n"})
	}

	return NewToolResultMessage(toolID, content, status, toolName)
}
