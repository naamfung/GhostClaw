package main

import (
        "path/filepath"
        "time"

        "gorm.io/gorm"
        "github.com/glebarez/sqlite"
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
        AccessCnt int       `gorm:"default:0"`
        Score     float64   `gorm:"default:0"`
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
}

var globalDB *gorm.DB

// InitDB 初始化数据库连接并自动迁移
func InitDB(dataDir string) error {
        dbPath := filepath.Join(dataDir, "ghostclaw.db")
        db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
        if err != nil {
                return err
        }
        globalDB = db

        // 自动迁移（建表/更新列）
        return db.AutoMigrate(&Memories{}, &Sessions{}, &Experiences{}, &SessionHistories{})
}

