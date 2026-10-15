package model

import "time"

// Setting 保存程序自己生成并需要持久化的配置，如 JWT 密钥
type Setting struct {
	Key       string    `gorm:"column:key;primaryKey"`
	Value     string    `gorm:"column:value;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}
