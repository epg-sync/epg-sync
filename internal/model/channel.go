package model

import "time"

type Channel struct {
	ID          int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement;not null"`
	ChannelID   string    `json:"channel_id" gorm:"column:channel_id;unique;not null"`
	DisplayName string    `json:"display_name" gorm:"column:display_name;default:null"`
	Category    string    `json:"category" gorm:"column:category;default:null"`
	Area        string    `json:"area" gorm:"column:area;default:CN"`
	LogoURL     string    `json:"logo_url" gorm:"column:logo_url;default:null"`
	IsActive    int       `json:"is_active" gorm:"column:is_active;default:1"`
	Regexp      string    `json:"regexp" gorm:"column:regexp;default:null"`
	Timezone    string    `json:"timezone" gorm:"column:timezone;default:Asia/Shanghai"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"column:updated_at"`
}

// ChannelRegexpMatch 是频道正则匹配某个名字的结果，Matched 为名字中被正则匹配到的部分（从开头起）
type ChannelRegexpMatch struct {
	Channel `gorm:"embedded"`
	Matched string `gorm:"column:matched"`
}

type ChannelMapping struct {
	ID                  int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement;not null"`
	CanonicalID         string    `json:"canonical_id" gorm:"column:canonical_id"`
	ProviderID          string    `json:"provider_id" gorm:"column:provider_id"`
	ProviderChannelID   string    `json:"provider_channel_id" gorm:"column:provider_channel_id"`
	ProviderChannelName string    `json:"provider_channel_name,omitempty" gorm:"column:provider_channel_name"`
	Confidence          float64   `json:"confidence" gorm:"column:confidence"`
	IsVerified          int       `json:"is_verified" gorm:"column:is_verified;default:0"`
	CreatedAt           time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt           time.Time `json:"updated_at" gorm:"column:updated_at"`
}

type ProviderChannel struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

type ChannelMappingInfo struct {
	ProviderChannelID string  `json:"provider_channel_id"`
	CanonicalID       string  `json:"canonical_id"`
	Confidence        float64 `json:"confidence"`
	ProviderID        string  `json:"provider_id"`
}
