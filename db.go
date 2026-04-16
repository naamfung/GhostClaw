package main

import (
        "fmt"
        "log"
        "os"
        "os/exec"
        "path/filepath"
        "strings"
        "sync"
        "sync/atomic"
        "time"

        "github.com/glebarez/sqlite"
        "gorm.io/gorm"
        "gorm.io/gorm/logger"
)

// Memories 表模型
type Memories struct {
        ID        string    `gorm:"primaryKey;type:text"`
        Category  string    `gorm:"index;default:fact;type:text"`
        Scope     string    `gorm:"index;default:user;type:text"`
        Key       string    `gorm:"index;type:text"`
        Value     string    `gorm:"type:text"`
        Tags      string    `gorm:"type:text"` // JSON array
        CreatedAt time.Time
        UpdatedAt time.Time
        AccessCnt int     `gorm:"default:0"`
        Score     float64 `gorm:"default:0"`
}

// Sessions 表模型
type Sessions struct {
        ID           string    `gorm:"primaryKey;type:text"`
        SessionKey   string    `gorm:"index;type:text"`
        StartTime    time.Time
        EndTime      time.Time
        MessageCount int
        Summary      string    `gorm:"type:text"`
        Tags         string    `gorm:"type:text"` // JSON array
        Channel      string    `gorm:"type:text"`
}

// Experiences 表模型
type Experiences struct {
        ID        string    `gorm:"primaryKey;type:text"`
        SessionID string    `gorm:"index;type:text"`
        TaskDesc  string    `gorm:"type:text"`
        Actions   string    `gorm:"type:text"` // JSON array of ExperienceAction
        Result    bool
        Summary   string    `gorm:"type:text"`
        Score     float64   `gorm:"default:0.5"`
        UsedCount int       `gorm:"default:0"`
        CreatedAt time.Time
        UpdatedAt time.Time
}

// SessionHistories 表模型 — 存储完整会话持久化数据（替代文件系统 .session.toon）
type SessionHistories struct {
        ID          string    `gorm:"primaryKey;type:text"`
        Description string    `gorm:"type:text"`
        Role        string    `gorm:"type:text"`
        Actor       string    `gorm:"type:text"`
        HistoryJSON string    `gorm:"type:text"` // 完整消息历史 JSON 序列化
        CreatedAt   time.Time
        UpdatedAt   time.Time

        // Token 追蹤欄位
        InputTokens  int `gorm:"default:0" json:"input_tokens"`   // 累計輸入 token
        OutputTokens int `gorm:"default:0" json:"output_tokens"`  // 累計輸出 token
        TotalTokens  int `gorm:"default:0" json:"total_tokens"`   // 累計總 token
        TurnCount    int `gorm:"default:0" json:"turn_count"`     // 會話輪次
}

// FTSSearchResult represents a full-text search result
type FTSSearchResult struct {
        RowID   int64   `json:"rowid"`
        Rank    float64 `json:"rank"`    // BM25 rank (lower is better)
        Snippet string  `json:"snippet"` // Highlighted snippet
}

var globalDB *gorm.DB
var dbRecoverMu sync.RWMutex // 運行時數據庫修復的全局讀寫鎖（修復期間阻塞所有讀查詢）
var dbRecovering atomic.Bool  // 標記數據庫是否正在修復中

// DBReadLock 獲取數據庫讀鎖。在修復進行中時會阻塞，直到修復完成。
// 所有對 globalDB 的讀寫操作都應該在持有此鎖的情況下進行，
// 以防止修復期間使用已關閉的連接。
func DBReadLock() {
        dbRecoverMu.RLock()
}

// DBReadUnlock 釋放數據庫讀鎖。
func DBReadUnlock() {
        dbRecoverMu.RUnlock()
}

// IsDBRecovering 檢查數據庫是否正在修復中。
// 可以用於快速跳過非關鍵的數據庫操作。
func IsDBRecovering() bool {
        return dbRecovering.Load()
}

// WaitDBRecovery 如果數據庫正在修復，則等待修復完成。
// 用於需要確保數據庫可用後再執行操作的场景。
func WaitDBRecovery() {
        if dbRecovering.Load() {
                dbRecoverMu.RLock()
                dbRecoverMu.RUnlock()
        }
}

// recoverDB 嘗試修復損壞的 SQLite 數據庫文件。
// 使用 sqlite3 的 .recover 等價操作：通過 integrity_check 檢測損壞，
// 然後 dump + 重建。返回 true 表示執行了修復。
func recoverDB(dbPath string) bool {
        // 使用 sqlite3 命令行工具執行 integrity check
        checkCmd := fmt.Sprintf("sqlite3 %q \"PRAGMA integrity_check;\"", dbPath)
        output, err := execCommandShell(checkCmd)
        if err != nil || !strings.Contains(output, "ok") {
                log.Printf("[DB] Database integrity check failed: %s (output: %s)", err, strings.TrimSpace(output))
        } else {
                // 數據庫完好，無需修復
                return false
        }

        log.Printf("[DB] Attempting database recovery for: %s", dbPath)

        // 備份損壞的數據庫
        backupPath := dbPath + ".corrupted.bak"
        if err := backupFile(dbPath, backupPath); err != nil {
                log.Printf("[DB] Failed to backup corrupted database: %v", err)
        } else {
                log.Printf("[DB] Corrupted database backed up to: %s", backupPath)
        }

        // 使用 sqlite3 .recover 命令重建（.dump 對損壞數據庫可能失敗，.recover 更強壯）
        recoverFile := dbPath + ".recover.sql"
        recoverCmd := fmt.Sprintf("sqlite3 %q \".recover\" > %q 2>/dev/null", dbPath, recoverFile)
        if _, err := execCommandShell(recoverCmd); err != nil {
                log.Printf("[DB] .recover command failed: %v, trying .dump fallback", err)
                // 回退到 .dump
                dumpCmd := fmt.Sprintf("sqlite3 %q \".dump\" > %q 2>/dev/null", dbPath, recoverFile)
                execCommandShell(dumpCmd)
        }

        // 檢查 recover 文件是否有內容
        if info, err := os.Stat(recoverFile); err != nil || info.Size() < 10 {
                log.Printf("[DB] Recovery dump is empty or missing, removing corrupted DB and starting fresh")
                os.Remove(dbPath)
                os.Remove(recoverFile)
                // 同時清理可能的 WAL 和 SHM 文件
                os.Remove(dbPath + "-wal")
                os.Remove(dbPath + "-shm")
                return true
        }

        // 刪除損壞的數據庫並用 recover 數據重建
        os.Remove(dbPath)
        os.Remove(dbPath + "-wal")
        os.Remove(dbPath + "-shm")

        rebuildCmd := fmt.Sprintf("sqlite3 %q < %q", dbPath, recoverFile)
        if _, err := execCommandShell(rebuildCmd); err != nil {
                log.Printf("[DB] Failed to rebuild database from recovery dump: %v", err)
                os.Remove(recoverFile)
                return false
        }

        os.Remove(recoverFile)
        log.Printf("[DB] Database successfully recovered")
        return true
}

// backupFile 複製文件（用於數據庫備份）
func backupFile(src, dst string) error {
        data, err := os.ReadFile(src)
        if err != nil {
                return err
        }
        return os.WriteFile(dst, data, 0644)
}

// execCommandShell 執行 shell 命令並返回輸出
func execCommandShell(cmd string) (string, error) {
        cmdObj := exec.Command("sh", "-c", cmd)
        output, err := cmdObj.CombinedOutput()
        return string(output), err
}

// InitDB 初始化数据库连接并自动迁移
// 数据库存放在 dataDir/memory/ghostclaw.db
func InitDB(dataDir string) error {
        memoryDir := filepath.Join(dataDir, "memory")
        if err := os.MkdirAll(memoryDir, 0755); err != nil {
                return fmt.Errorf("failed to create memory directory: %v", err)
        }
        dbPath := filepath.Join(memoryDir, "ghostclaw.db")

        // 在開啓連接前，先嘗試修復已損壞的數據庫文件
        if info, statErr := os.Stat(dbPath); statErr == nil && info.Size() > 0 {
                if recoverDB(dbPath) {
                        log.Printf("[DB] Database repair completed before open")
                }
        }

        db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
                Logger: logger.Default.LogMode(logger.Warn),
        })
        if err != nil {
                return err
        }
        globalDB = db

        // 在 AutoMigrate 之前設置 WAL 模式和忙等待超時
        // WAL 模式：並發讀不阻塞寫，大幅降低 SQLITE_BUSY 和損壞風險
        db.Exec("PRAGMA journal_mode=WAL")
        // 並發寫時自動重試 5 秒，避免 SQLITE_BUSY
        db.Exec("PRAGMA busy_timeout=5000")
        // NORMAL：WAL 模式下足夠安全，同時大幅提升寫入性能
        db.Exec("PRAGMA synchronous=NORMAL")

        // 自动迁移（建表/更新列）
        if err := db.AutoMigrate(&Memories{}, &Sessions{}, &Experiences{}, &SessionHistories{}); err != nil {
                return err
        }

        // 创建 FTS5 全文搜索虚拟表和同步触发器
        initFTS5(db)

        // 如果數據庫是新建的（數據庫損壞後被清空重建），
        // 嘗試從 .toon 備份文件恢復會話數據
        if RebuildFromBackups() {
                log.Printf("[DB] Session data rebuilt from .toon backup files after DB init")
        }

        return nil
}

// initFTS5 creates FTS5 virtual tables and triggers for all main tables.
// If FTS5 creation fails for a given table, the corresponding triggers are dropped
// so that normal INSERT/UPDATE/DELETE operations on the content table are not affected.
func initFTS5(db *gorm.DB) {
        // WAL 模式已在 InitDB 中提前設置，此處不再重複

        // ── FTS5 Virtual Tables ──────────────────────────────────────────────

        ftsTables := []struct {
                name    string
                columns string
        }{
                {
                        name:    "memories_fts",
                        columns: "key, value, tags",
                },
                {
                        name:    "experiences_fts",
                        columns: "task_desc, summary",
                },
                {
                        name:    "sessions_fts",
                        columns: "session_key, summary, tags",
                },
                {
                        name:    "session_histories_fts",
                        columns: "description, history_json",
                },
        }

        contentMap := map[string]string{
                "memories_fts":          "memories",
                "experiences_fts":       "experiences",
                "sessions_fts":          "sessions",
                "session_histories_fts": "session_histories",
        }

        // Track which FTS tables were successfully created
        createdTables := make(map[string]bool)
        for _, ft := range ftsTables {
                contentTable := contentMap[ft.name]
                sql := fmt.Sprintf(
                        "CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(%s, content=%s, content_rowid=rowid, tokenize='unicode61')",
                        ft.name, ft.columns, contentTable,
                )
                if err := db.Exec(sql).Error; err != nil {
                        log.Printf("[FTS5] WARNING: failed to create virtual table %s: %v", ft.name, err)
                        // Drop any stale triggers that reference the failed FTS table
                        for _, suffix := range []string{"_ai", "_ad", "_au"} {
                                contentName := strings.TrimSuffix(ft.name, "_fts")
                                db.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s%s", contentName, suffix))
                        }
                        continue
                }
                createdTables[ft.name] = true
                log.Printf("[FTS5] virtual table %s created/verified", ft.name)
        }

        // ── Triggers (only for successfully created FTS tables) ──────────────

        // triggers grouped by FTS table name
        triggerGroups := map[string][]string{
                "memories_fts": {
                        `CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(key, value, tags) VALUES(new.key, new.value, new.tags);
END;`,
                        `CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid) VALUES('delete', old.rowid);
END;`,
                        `CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid) VALUES('delete', old.rowid);
    INSERT INTO memories_fts(key, value, tags) VALUES(new.key, new.value, new.tags);
END;`,
                },
                "experiences_fts": {
                        `CREATE TRIGGER IF NOT EXISTS experiences_ai AFTER INSERT ON experiences BEGIN
    INSERT INTO experiences_fts(task_desc, summary) VALUES(new.task_desc, new.summary);
END;`,
                        `CREATE TRIGGER IF NOT EXISTS experiences_ad AFTER DELETE ON experiences BEGIN
    INSERT INTO experiences_fts(experiences_fts, rowid) VALUES('delete', old.rowid);
END;`,
                        `CREATE TRIGGER IF NOT EXISTS experiences_au AFTER UPDATE ON experiences BEGIN
    INSERT INTO experiences_fts(experiences_fts, rowid) VALUES('delete', old.rowid);
    INSERT INTO experiences_fts(task_desc, summary) VALUES(new.task_desc, new.summary);
END;`,
                },
                "sessions_fts": {
                        `CREATE TRIGGER IF NOT EXISTS sessions_ai AFTER INSERT ON sessions BEGIN
    INSERT INTO sessions_fts(session_key, summary, tags) VALUES(new.session_key, new.summary, new.tags);
END;`,
                        `CREATE TRIGGER IF NOT EXISTS sessions_ad AFTER DELETE ON sessions BEGIN
    INSERT INTO sessions_fts(sessions_fts, rowid) VALUES('delete', old.rowid);
END;`,
                        `CREATE TRIGGER IF NOT EXISTS sessions_au AFTER UPDATE ON sessions BEGIN
    INSERT INTO sessions_fts(sessions_fts, rowid) VALUES('delete', old.rowid);
    INSERT INTO sessions_fts(session_key, summary, tags) VALUES(new.session_key, new.summary, new.tags);
END;`,
                },
                "session_histories_fts": {
                        `CREATE TRIGGER IF NOT EXISTS session_histories_ai AFTER INSERT ON session_histories BEGIN
    INSERT INTO session_histories_fts(description, history_json) VALUES(new.description, new.history_json);
END;`,
                        `CREATE TRIGGER IF NOT EXISTS session_histories_ad AFTER DELETE ON session_histories BEGIN
    INSERT INTO session_histories_fts(session_histories_fts, rowid) VALUES('delete', old.rowid);
END;`,
                        `CREATE TRIGGER IF NOT EXISTS session_histories_au AFTER UPDATE ON session_histories BEGIN
    INSERT INTO session_histories_fts(session_histories_fts, rowid) VALUES('delete', old.rowid);
    INSERT INTO session_histories_fts(description, history_json) VALUES(new.description, new.history_json);
END;`,
                },
        }

        for ftsName, triggerList := range triggerGroups {
                if !createdTables[ftsName] {
                        // FTS table doesn't exist, skip triggers for it
                        log.Printf("[FTS5] skipping triggers for %s (table not available)", ftsName)
                        continue
                }
                for _, sql := range triggerList {
                        if err := db.Exec(sql).Error; err != nil {
                                log.Printf("[FTS5] WARNING: failed to create trigger: %v", err)
                        }
                }
        }

        log.Println("[FTS5] initialization complete")
}

// isDBMalformedError 檢測錯誤是否爲 SQLite 數據庫損壞相關錯誤。
// 覆蓋 GORM 層和底層 SQLite 驅動層的各種損壞錯誤信息。
func isDBMalformedError(err error) bool {
        if err == nil {
                return false
        }
        errStr := err.Error()
        // 常見的 SQLite 損壞錯誤關鍵詞
        malformedKeywords := []string{
                "database disk image is malformed",
                "database is malformed",
                "malformed database",
                "disk I/O error",
                "sqlite3 disk I/O error",
                "database or disk is full",
                "unable to open database file",
                "file is not a database",
                "file is encrypted or is not a database",
                "bad node",
                "corruption",
                "checksum mismatch",
        }
        lower := strings.ToLower(errStr)
        for _, kw := range malformedKeywords {
                if strings.Contains(lower, strings.ToLower(kw)) {
                        return true
                }
        }
        return false
}

// handleDBMalformedRuntime 在運行時檢測到數據庫損壞時進行修復。
// 採用全局讀寫鎖確保並發安全：
//   - 修復時持有寫鎖（Lock），會阻塞所有 DBReadLock 的讀查詢
//   - 正常操作持有讀鎖（RLock），多個讀操作可並行
//   - dbRecovering 標誌僅用於快速檢查，真正的等待保障來自 RWMutex
func handleDBMalformedRuntime() {
        // 快速路徑：如果已經在修復中，直接跳過
        if dbRecovering.Load() {
                log.Printf("[DB] Runtime recovery: another recovery already in progress, skipping")
                return
        }

        // 先獲取寫鎖（會阻塞所有 DBReadLock 持有者釋放後才繼續），
        // 然後再設標誌。這樣 WaitDBRecovery 中的 RLock 不會在標誌設置後
        // 但寫鎖獲取前的窗口期內誤放行。
        dbRecoverMu.Lock()
        defer dbRecoverMu.Unlock()

        // 雙重檢查：持鎖後再確認是否已有其他 goroutine 在修復
        if !dbRecovering.CompareAndSwap(false, true) {
                log.Printf("[DB] Runtime recovery: another recovery already in progress (re-check), skipping")
                return
        }
        defer dbRecovering.Store(false)

        if globalDB == nil {
                log.Printf("[DB] Runtime recovery: globalDB is nil, nothing to recover")
                return
        }

        // 嘗試獲取數據庫文件路徑
        var dbPath string
        if rawDB, err := globalDB.DB(); err == nil && rawDB != nil {
                rows, queryErr := globalDB.Raw("PRAGMA database_list").Rows()
                if queryErr == nil {
                        defer rows.Close()
                        for rows.Next() {
                                var seq int
                                var name, path string
                                rows.Scan(&seq, &name, &path)
                                if path != "" {
                                        dbPath = path
                                        break
                                }
                        }
                }
        }

        if dbPath == "" {
                log.Printf("[DB] Runtime recovery: could not determine database path, skipping recovery")
                return
        }

        log.Printf("[DB] Runtime recovery: attempting to recover corrupted database at: %s", dbPath)

        // 1. 關閉現有連接池
        if rawDB, err := globalDB.DB(); err == nil && rawDB != nil {
                rawDB.Close()
                log.Printf("[DB] Runtime recovery: closed existing database connections")
        }

        // 2. 嘗試修復
        recovered := recoverDB(dbPath)
        if !recovered {
                log.Printf("[DB] Runtime recovery: recoverDB returned false (DB may already be intact or unrecoverable)")
        }

        // 3. 重新打開數據庫連接
        db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
                Logger: logger.Default.LogMode(logger.Warn),
        })
        if err != nil {
                log.Printf("[DB] Runtime recovery: FAILED to reopen database: %v", err)
                // 修復後重新打開失敗，嘗試刪除損壞文件並重建
                os.Remove(dbPath)
                os.Remove(dbPath + "-wal")
                os.Remove(dbPath + "-shm")
                db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
                        Logger: logger.Default.LogMode(logger.Warn),
                })
                if err != nil {
                        log.Printf("[DB] Runtime recovery: FAILED to create fresh database: %v", err)
                        return
                }
                log.Printf("[DB] Runtime recovery: created fresh database after failed reopen")
        }
        globalDB = db

        // 4. 重新設置 PRAGMA
        db.Exec("PRAGMA journal_mode=WAL")
        db.Exec("PRAGMA busy_timeout=5000")
        db.Exec("PRAGMA synchronous=NORMAL")

        // 5. 重新遷移
        if err := db.AutoMigrate(&Memories{}, &Sessions{}, &Experiences{}, &SessionHistories{}); err != nil {
                log.Printf("[DB] Runtime recovery: AutoMigrate failed after recovery: %v", err)
                return
        }

        // 6. 嘗試從 .toon 備份文件恢復會話數據
        if RebuildFromBackups() {
                log.Printf("[DB] Runtime recovery: session data rebuilt from backup files")
        }

        log.Printf("[DB] Runtime recovery completed successfully")
}

// sanitizeFTSQuery sanitizes user input to prevent FTS5 syntax errors.
// Plain text queries are wrapped in double-quotes per token so that characters
// like `-`, `*`, `:` inside words are treated as literal text.
// Queries that already contain explicit FTS5 operators (AND, OR, NOT, NEAR,
// column filters, or leading quotes) are passed through as-is.
func sanitizeFTSQuery(query string) string {
        query = strings.TrimSpace(query)
        if query == "" {
                return query
        }

        upper := strings.ToUpper(query)

        // Detect explicit FTS5 boolean operators or advanced syntax
        if strings.Contains(upper, " AND ") ||
                strings.Contains(upper, " OR ") ||
                strings.Contains(upper, " NOT ") ||
                strings.Contains(upper, " NEAR ") ||
                strings.HasPrefix(query, `"`) ||
                strings.HasPrefix(query, "(") {
                return query
        }

        // Wrap each whitespace-separated token in double-quotes.
        // This makes FTS5 treat each token as a literal phrase, preventing
        // characters like -, *, : from being interpreted as operators.
        words := strings.Fields(query)
        if len(words) == 0 {
                return query
        }
        quoted := make([]string, len(words))
        for i, w := range words {
                quoted[i] = `"` + w + `"`
        }
        return strings.Join(quoted, " ")
}

// SearchMemories searches memories using FTS5 full-text search
func SearchMemories(query string, limit int) ([]FTSSearchResult, error) {
        if globalDB == nil {
                return nil, fmt.Errorf("database not initialized")
        }
        if limit <= 0 {
                limit = 10
        }

        safeQuery := sanitizeFTSQuery(query)

        sql := `SELECT rowid, rank, snippet(memories_fts, 2, '<b>', '</b>', '...', 32) as snippet
                FROM memories_fts WHERE memories_fts MATCH ? ORDER BY rank LIMIT ?`

        var results []FTSSearchResult
        if err := globalDB.Raw(sql, safeQuery, limit).Scan(&results).Error; err != nil {
                return nil, fmt.Errorf("FTS search on memories failed: %w", err)
        }
        return results, nil
}

// SearchExperiences searches experiences using FTS5 full-text search
func SearchExperiences(query string, limit int) ([]FTSSearchResult, error) {
        if globalDB == nil {
                return nil, fmt.Errorf("database not initialized")
        }
        if limit <= 0 {
                limit = 10
        }

        safeQuery := sanitizeFTSQuery(query)

        sql := `SELECT rowid, rank, snippet(experiences_fts, 2, '<b>', '</b>', '...', 32) as snippet
                FROM experiences_fts WHERE experiences_fts MATCH ? ORDER BY rank LIMIT ?`

        var results []FTSSearchResult
        if err := globalDB.Raw(sql, safeQuery, limit).Scan(&results).Error; err != nil {
                return nil, fmt.Errorf("FTS search on experiences failed: %w", err)
        }
        return results, nil
}

// SearchSessions searches sessions using FTS5 full-text search
func SearchSessions(query string, limit int) ([]FTSSearchResult, error) {
        if globalDB == nil {
                return nil, fmt.Errorf("database not initialized")
        }
        if limit <= 0 {
                limit = 10
        }

        safeQuery := sanitizeFTSQuery(query)

        sql := `SELECT rowid, rank, snippet(sessions_fts, 2, '<b>', '</b>', '...', 32) as snippet
                FROM sessions_fts WHERE sessions_fts MATCH ? ORDER BY rank LIMIT ?`

        var results []FTSSearchResult
        if err := globalDB.Raw(sql, safeQuery, limit).Scan(&results).Error; err != nil {
                return nil, fmt.Errorf("FTS search on sessions failed: %w", err)
        }
        return results, nil
}

// SearchAll searches across all tables using FTS5 full-text search.
// Returns separate result slices for memories, experiences, and sessions.
func SearchAll(query string, limit int) (memories, experiences, sessions []FTSSearchResult, err error) {
        if globalDB == nil {
                return nil, nil, nil, fmt.Errorf("database not initialized")
        }
        if limit <= 0 {
                limit = 10
        }

        perTable := limit
        if perTable < 1 {
                perTable = 10
        }

        var errs []string

        m, e := SearchMemories(query, perTable)
        if e != nil {
                errs = append(errs, e.Error())
        }

        x, e := SearchExperiences(query, perTable)
        if e != nil {
                errs = append(errs, e.Error())
        }

        s, e := SearchSessions(query, perTable)
        if e != nil {
                errs = append(errs, e.Error())
        }

        if len(errs) > 0 {
                err = fmt.Errorf("SearchAll encountered errors: %s", strings.Join(errs, "; "))
        }

        return m, x, s, err
}

// RebuildFTS rebuilds all FTS indexes from existing data.
// This should be called for initial data population or migration recovery.
func RebuildFTS() error {
        if globalDB == nil {
                return fmt.Errorf("database not initialized")
        }

        log.Println("[FTS5] rebuilding all FTS indexes...")

        // Use the rebuild command for each FTS5 content-synced table
        rebuilds := []string{
                "INSERT INTO memories_fts(memories_fts) VALUES('rebuild')",
                "INSERT INTO experiences_fts(experiences_fts) VALUES('rebuild')",
                "INSERT INTO sessions_fts(sessions_fts) VALUES('rebuild')",
                "INSERT INTO session_histories_fts(session_histories_fts) VALUES('rebuild')",
        }

        for _, sql := range rebuilds {
                if err := globalDB.Exec(sql).Error; err != nil {
                        log.Printf("[FTS5] WARNING: rebuild failed for %s: %v", sql, err)
                        return fmt.Errorf("FTS rebuild failed: %w", err)
                }
        }

        log.Println("[FTS5] all FTS indexes rebuilt successfully")
        return nil
}
