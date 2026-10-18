package model

import "time"

// Log 是一条程序日志，见迁移 00005_logs.sql
type Log struct {
	ID      int64          `gorm:"column:id;primaryKey" json:"id"`
	Time    time.Time      `gorm:"column:time" json:"time"`
	Level   int8           `gorm:"column:level" json:"level"`
	Logger  string         `gorm:"column:logger" json:"logger"`
	Caller  string         `gorm:"column:caller" json:"caller"`
	Message string         `gorm:"column:message" json:"message"`
	Fields  map[string]any `gorm:"column:fields;serializer:json" json:"fields"`
	Stack   string         `gorm:"column:stack" json:"stack"`
}

// LogFilter 是日志列表的筛选条件，零值表示不限制
type LogFilter struct {
	MinLevel *int8
	Logger   *string // 指定来源；空字符串表示业务日志
	Keyword  string  // 在 message 和 fields 中不区分大小写地查找
	Since    time.Time
	Until    time.Time
	BeforeID int64 // 翻页：只返回 id 更小的（更早的）
	AfterID  int64 // 实时刷新：只返回 id 更大的（更新的）
	Limit    int
}
