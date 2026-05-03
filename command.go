package main

import (
	"log"
	"os"
	"strings"
)

// ApplyCommandResult 应用命令结果（用于没有回调的简单渠道）
func ApplyCommandResult(result CommandResult, session *GlobalSession) {
	if result.Response != "" {
		log.Printf("[Command] Response: %s", result.Response)
	}
	if result.IsStop && session != nil {
		session.CancelTask()
	}
	if result.IsPause && session != nil {
		msg := result.PauseMsg
		if msg == "" {
			msg = "已中斷。請繼續。"
		}
		session.InterruptTask(msg)
	}
	if result.IsQuit {
		log.Println("[Command] /quit: disconnecting")
		if session != nil {
			session.autoSaveHistory()
		}
		return
	}
	if result.IsExit {
		log.Println("[Command] /exit: exiting program")
		if session != nil {
			session.autoSaveHistory()
			// 保存未处理消息队列
			if err := session.SavePendingMessages(); err != nil {
				log.Printf("Failed to save pending messages: %v", err)
			}
		}
		os.Exit(0)
	}
}

// HandleSlashCommandWithDefaults 处理斜杠命令，并执行默认行为
// responder: 发送响应文本的函数
// stopFunc:  取消任务的函数（可为 nil，此时使用 session.CancelTask）
// pauseFunc: 中斷任務的函數（可為 nil，此時使用 session.InterruptTask）
// quitFunc:  断开连接/切换模式的函数（可为 nil，此时仅记录日志）
// exitFunc:  退出程序的函数（可为 nil，此时使用 os.Exit）
// 返回 true 表示命令已处理
func HandleSlashCommandWithDefaults(line string, responder func(string), stopFunc func(), pauseFunc func(string), quitFunc func(), exitFunc func()) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "/") {
		return false
	}

	result := ProcessSlashCommand(trimmed, globalRoleManager, globalActorManager, globalStage)
	if !result.Handled {
		return false
	}

	if result.Response != "" && responder != nil {
		responder(result.Response)
	}
	if result.IsStop {
		if stopFunc != nil {
			stopFunc()
		} else {
			GetGlobalSession().CancelTask()
		}
	}
	if result.IsPause {
		msg := result.PauseMsg
		if msg == "" {
			msg = "已中斷。請繼續。" // 默認中斷訊息
		}
		log.Printf("[Command] 任務已中斷（pause），唔向前端輸出提示")
		if pauseFunc != nil {
			pauseFunc(msg)
		} else {
			GetGlobalSession().InterruptTask(msg)
		}
	}
	if result.IsQuit {
		if quitFunc != nil {
			quitFunc()
		} else {
			log.Println("[Command] /quit: no quit handler, ignoring")
		}
		return true
	}
	if result.IsExit {
		if exitFunc != nil {
			exitFunc()
		} else {
			session := GetGlobalSession()
			session.autoSaveHistory()
			// 保存未处理消息队列
			if err := session.SavePendingMessages(); err != nil {
				log.Printf("Failed to save pending messages: %v", err)
			}
			os.Exit(0)
		}
	}
	return true
}
