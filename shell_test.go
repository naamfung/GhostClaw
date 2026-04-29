package main

import (
	"strings"
	"testing"
)

// ============================================================================
// ExpandAlias
// ============================================================================

func TestExpandAlias_NilAliases(t *testing.T) {
	result := ExpandAlias("ls -la", nil)
	if result != "ls -la" {
		t.Errorf("ExpandAlias with nil map = %q, want %q", result, "ls -la")
	}
}

func TestExpandAlias_EmptyAliases(t *testing.T) {
	result := ExpandAlias("ls -la", map[string]string{})
	if result != "ls -la" {
		t.Errorf("ExpandAlias with empty map = %q, want %q", result, "ls -la")
	}
}

func TestExpandAlias_NoMatch(t *testing.T) {
	aliases := map[string]string{"ll": "ls -la"}
	result := ExpandAlias("git status", aliases)
	if result != "git status" {
		t.Errorf("ExpandAlias = %q, want %q", result, "git status")
	}
}

func TestExpandAlias_SingleExpansion(t *testing.T) {
	aliases := map[string]string{"ll": "ls -la"}
	result := ExpandAlias("ll", aliases)
	if result != "ls -la" {
		t.Errorf("ExpandAlias = %q, want %q", result, "ls -la")
	}
}

func TestExpandAlias_SingleExpansionWithArgs(t *testing.T) {
	aliases := map[string]string{"ll": "ls -la"}
	result := ExpandAlias("ll /home", aliases)
	if result != "ls -la /home" {
		t.Errorf("ExpandAlias = %q, want %q", result, "ls -la /home")
	}
}

func TestExpandAlias_RecursiveExpansion(t *testing.T) {
	aliases := map[string]string{
		"l":  "ll",
		"ll": "ls -la",
	}
	result := ExpandAlias("l /tmp", aliases)
	if result != "ls -la /tmp" {
		t.Errorf("ExpandAlias recursive = %q, want %q", result, "ls -la /tmp")
	}
}

func TestExpandAlias_CycleDetection(t *testing.T) {
	aliases := map[string]string{
		"a": "b",
		"b": "a",
	}
	result := ExpandAlias("a", aliases)
	// 展開鏈：a → b → a（檢測到循環，返回展開後的值 a）
	if result != "a" {
		t.Errorf("ExpandAlias with cycle = %q, want %q", result, "a")
	}
}

func TestExpandAlias_SelfCycle(t *testing.T) {
	aliases := map[string]string{
		"x": "x arg",
	}
	result := ExpandAlias("x", aliases)
	// 展開：x → "x arg" → 檢測到循環，返回 "x arg"
	if result != "x arg" {
		t.Errorf("ExpandAlias with self-cycle = %q, want %q", result, "x arg")
	}
}

func TestExpandAlias_EmptyCommand(t *testing.T) {
	result := ExpandAlias("  ", nil)
	// 空白指令經過 TrimSpace 後為空，返回原始指令
	if result != "  " {
		t.Errorf("ExpandAlias with whitespace-only command = %q, want %q", result, "  ")
	}
}

func TestExpandAlias_WhitespaceCommand(t *testing.T) {
	aliases := map[string]string{"cmd": "expanded"}
	result := ExpandAlias("  cmd  arg  ", aliases)
	expected := "expanded arg"
	if result != expected {
		t.Errorf("ExpandAlias = %q, want %q", result, expected)
	}
}

// ============================================================================
// detectBlockingCommand
// ============================================================================

func TestDetectBlockingCommand_EmptyCommand(t *testing.T) {
	info := detectBlockingCommand("")
	if info.IsBlocking {
		t.Error("empty command should not be blocking")
	}
}

func TestDetectBlockingCommand_SafeCommand(t *testing.T) {
	tests := []string{
		"ls -la",
		"echo hello",
		"cat /etc/hosts",
		"git status",
		"go build ./...",
		"curl https://example.com",
		"grep -r pattern .",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			info := detectBlockingCommand(cmd)
			if info.IsBlocking {
				t.Errorf("detectBlockingCommand(%q).IsBlocking = true, want false", cmd)
			}
		})
	}
}

func TestDetectBlockingCommand_SSH_NoKeyFlag(t *testing.T) {
	info := detectBlockingCommand("ssh user@host")
	if !info.IsBlocking {
		t.Error("ssh without key flag should be blocking")
	}
	if info.Reason == "" {
		t.Error("ssh should have a reason")
	}
	if len(info.Suggestions) == 0 {
		t.Error("ssh should have suggestions")
	}
}

func TestDetectBlockingCommand_SSH_WithKeyFlag(t *testing.T) {
	info := detectBlockingCommand("ssh -i /path/to/key user@host")
	if info.IsBlocking {
		t.Errorf("ssh with -i flag should not be blocking, got reason: %s", info.Reason)
	}
}

func TestDetectBlockingCommand_SSH_WithSshpass(t *testing.T) {
	info := detectBlockingCommand("sshpass -p 'pass' ssh user@host ls")
	if info.IsBlocking {
		t.Errorf("ssh with sshpass should not be blocking, got reason: %s", info.Reason)
	}
}

func TestDetectBlockingCommand_SSH_BackgroundWithoutSetsid(t *testing.T) {
	info := detectBlockingCommand("ssh -i /path/to/key user@host 'long_running_task &'")
	if !info.IsBlocking {
		t.Error("ssh with background process without setsid/nohup should be blocking")
	}
}

func TestDetectBlockingCommand_SSH_BackgroundWithSetsid(t *testing.T) {
	info := detectBlockingCommand("ssh -i /path/to/key user@host 'setsid long_running_task'")
	if info.IsBlocking {
		t.Errorf("ssh with setsid should not be blocking, got reason: %s", info.Reason)
	}
}

func TestDetectBlockingCommand_SCP_NoKeyOrSshpass(t *testing.T) {
	info := detectBlockingCommand("scp file.txt user@host:/path")
	if !info.IsBlocking {
		t.Error("scp without -i or sshpass should be blocking")
	}
}

func TestDetectBlockingCommand_SCP_WithKey(t *testing.T) {
	info := detectBlockingCommand("scp -i /path/to/key file.txt user@host:/path")
	if info.IsBlocking {
		t.Errorf("scp with -i should not be blocking, got reason: %s", info.Reason)
	}
}

func TestDetectBlockingCommand_Rsync_SSH_WithoutKey(t *testing.T) {
	info := detectBlockingCommand("rsync -e ssh src/ user@host:dst/")
	if !info.IsBlocking {
		t.Error("rsync over ssh without key should be blocking")
	}
}

func TestDetectBlockingCommand_Rsync_SSH_WithKey(t *testing.T) {
	info := detectBlockingCommand("rsync -e 'ssh -i /path/to/key' src/ user@host:dst/")
	if info.IsBlocking {
		t.Errorf("rsync with key should not be blocking, got reason: %s", info.Reason)
	}
}

func TestDetectBlockingCommand_Sudo_NoSFlag(t *testing.T) {
	info := detectBlockingCommand("sudo apt install vim")
	if !info.IsBlocking {
		t.Error("sudo without -S flag should be blocking")
	}
}

func TestDetectBlockingCommand_Sudo_WithSFlag(t *testing.T) {
	info := detectBlockingCommand("sudo -S apt install vim")
	if info.IsBlocking {
		t.Errorf("sudo with -S should not be blocking, got reason: %s", info.Reason)
	}
}

func TestDetectBlockingCommand_Sudo_WithLowerS(t *testing.T) {
	info := detectBlockingCommand("echo 'pass' | sudo -s cmd")
	if info.IsBlocking {
		t.Errorf("sudo with -s should not be blocking, got reason: %s", info.Reason)
	}
}

func TestDetectBlockingCommand_SFTP_AlwaysBlocking(t *testing.T) {
	info := detectBlockingCommand("sftp user@host")
	if !info.IsBlocking {
		t.Error("sftp should always be blocking")
	}
}

func TestDetectBlockingCommand_FTP_AlwaysBlocking(t *testing.T) {
	info := detectBlockingCommand("ftp ftp.example.com")
	if !info.IsBlocking {
		t.Error("ftp should always be blocking")
	}
}

func TestDetectBlockingCommand_InteractivePrograms(t *testing.T) {
	programs := []string{"vim", "vi", "nano", "emacs", "less", "more", "top", "htop", "screen", "tmux", "mysql", "psql", "sqlite3"}

	for _, prog := range programs {
		t.Run(prog, func(t *testing.T) {
			info := detectBlockingCommand(prog)
			if !info.IsBlocking {
				t.Errorf("%s should be blocking (interactive)", prog)
			}
			if !strings.Contains(strings.ToLower(info.Reason), strings.ToLower(prog)) {
				t.Errorf("%s reason should mention the program", prog)
			}
		})
	}
}

func TestDetectBlockingCommand_FullPathInteractive(t *testing.T) {
	// 帶路徑嘅命令應該一樣檢測到（strip 路徑後判斷）
	info := detectBlockingCommand("/usr/bin/vim /etc/hosts")
	if !info.IsBlocking {
		t.Error("full path vim should be blocking")
	}
}

func TestDetectBlockingCommand_Su_Blocking(t *testing.T) {
	info := detectBlockingCommand("su - user")
	if !info.IsBlocking {
		t.Error("su should be blocking")
	}
}

func TestDetectBlockingCommand_CaseInsensitive(t *testing.T) {
	info := detectBlockingCommand("VIM file.txt")
	if !info.IsBlocking {
		t.Error("VIM (uppercase) should be blocking")
	}
}

// ============================================================================
// CheckBlockingCommand
// ============================================================================

func TestCheckBlockingCommand_WrapsDetect(t *testing.T) {
	result := CheckBlockingCommand("vim file.txt")
	if !result.IsBlocking {
		t.Error("CheckBlockingCommand should wrap detectBlockingCommand")
	}
}

// ============================================================================
// BuildConfirmMessage
// ============================================================================

func TestBuildConfirmMessage_ContainsReason(t *testing.T) {
	info := BlockingCommandInfo{
		IsBlocking:  true,
		Reason:      "test reason",
		Suggestions: []string{"suggestion 1", "suggestion 2"},
	}
	msg := BuildConfirmMessage(info, "test command")
	if !strings.Contains(msg, info.Reason) {
		t.Error("BuildConfirmMessage should contain reason")
	}
	if !strings.Contains(msg, "suggestion 1") {
		t.Error("BuildConfirmMessage should contain suggestion 1")
	}
	if !strings.Contains(msg, "suggestion 2") {
		t.Error("BuildConfirmMessage should contain suggestion 2")
	}
}

func TestBuildConfirmMessage_EmptySuggestions(t *testing.T) {
	info := BlockingCommandInfo{
		IsBlocking:  true,
		Reason:      "just a reason",
		Suggestions: nil,
	}
	msg := BuildConfirmMessage(info, "cmd")
	if !strings.Contains(msg, "just a reason") {
		t.Error("BuildConfirmMessage should contain reason even without suggestions")
	}
}

// ============================================================================
// isDangerousCommand
// ============================================================================

func TestIsDangerousCommand_DangerousPatterns(t *testing.T) {
	tests := []string{
		"rm -rf / --no-preserve-root",
		"rm -rf /*",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
		"shutdown -h now",
		"reboot",
		"halt",
		"poweroff",
		"chmod 777 /",
		"> /dev/sda",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			if !isDangerousCommand(cmd) {
				t.Errorf("isDangerousCommand(%q) = false, want true", cmd)
			}
		})
	}
}

func TestIsDangerousCommand_LuaBlocked(t *testing.T) {
	tests := []string{
		"lua script.lua",
		"luajit script.lua",
		"luarocks install",
		"/usr/bin/lua script.lua",
		"lua5.1 script.lua",
		"luajit-2.1 script.lua",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			if !isDangerousCommand(cmd) {
				t.Errorf("isDangerousCommand(%q) = false, want true (Lua blocked)", cmd)
			}
		})
	}
}

func TestIsDangerousCommand_LuaCallChains(t *testing.T) {
	tests := []string{
		"echo test; lua -e 'print(1)'",
		"ls && lua script.lua",
		"ls || lua backup.lua",
		"cat file | lua process.lua",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			if !isDangerousCommand(cmd) {
				t.Errorf("isDangerousCommand(%q) = false, want true", cmd)
			}
		})
	}
}

func TestIsDangerousCommand_SafeCommands(t *testing.T) {
	tests := []string{
		"ls -la",
		"echo hello",
		"cat README.md",
		"git status",
		"go test ./...",
		"rm file.txt", // rm without -rf /
		"chmod 644 file.txt",
		"python3 script.py",
		"ruby script.rb",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			if isDangerousCommand(cmd) {
				t.Errorf("isDangerousCommand(%q) = true, want false", cmd)
			}
		})
	}
}

func TestIsDangerousCommand_CaseInsensitive(t *testing.T) {
	if !isDangerousCommand("SHUTDOWN -h now") {
		t.Error("SHUTDOWN (uppercase) should be dangerous")
	}
}

func TestIsDangerousCommand_ForkBomb(t *testing.T) {
	if !isDangerousCommand(":(){ :|:& };:") {
		t.Error("fork bomb should be detected as dangerous")
	}
}
