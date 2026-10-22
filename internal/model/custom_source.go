package model

import (
	"strconv"
	"time"
)

const (
	CustomSourceStatusRunning = "running"
	CustomSourceStatusSuccess = "success"
	CustomSourceStatusFailed  = "failed"
)

// CustomSourceProviderPrefix 是自定义源写入节目时 provider_id 的前缀，完整形式为 "custom:<id>"
const CustomSourceProviderPrefix = "custom:"

// CustomSource 是用户配置的 XMLTV 文件源（.xml / .xml.gz）。Priority 与 provider 一致，数字越小优先级越高
type CustomSource struct {
	ID           int64      `json:"id" gorm:"column:id;primaryKey;autoIncrement;not null"`
	Name         string     `json:"name" gorm:"column:name"`
	URL          string     `json:"url" gorm:"column:url"`
	Enabled      int        `json:"enabled" gorm:"column:enabled"`
	Priority     int        `json:"priority" gorm:"column:priority"`
	LastSyncAt   *time.Time `json:"last_sync_at" gorm:"column:last_sync_at"`
	LastStatus   string     `json:"last_status" gorm:"column:last_status"`
	LastError    string     `json:"last_error" gorm:"column:last_error"`
	ProgramCount int        `json:"program_count" gorm:"column:program_count"`
	CreatedAt    time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt    time.Time  `json:"updated_at" gorm:"column:updated_at"`
}

// ProviderID 返回该源写入的节目所用的 provider_id
func (s *CustomSource) ProviderID() string {
	return CustomSourceProviderPrefix + strconv.FormatInt(s.ID, 10)
}
