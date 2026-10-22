package model

import (
	"encoding/xml"
	"time"
)

type Program struct {
	ID                int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement;not null"`
	ChannelID         string    `json:"channel_id" gorm:"column:channel_id"`
	Title             string    `json:"title" gorm:"column:title"`
	Description       string    `json:"description" gorm:"column:description"`
	StartTime         time.Time `json:"start_time" gorm:"column:start_time"`
	EndTime           time.Time `json:"end_time" gorm:"column:end_time"`
	Category          string    `json:"category" gorm:"column:category"`
	ProviderID        string    `json:"provider_id" gorm:"column:provider_id"`
	ProviderProgramID string    `json:"provider_program_id" gorm:"column:provider_program_id"`
	OriginalTimezone  string    `json:"original_timezone" gorm:"column:original_timezone;default:Asia/Shanghai"`
	CreatedAt         time.Time `json:"created_at" gorm:"column:created_at"`

	Channel *Channel `json:"channel,omitempty" gorm:"foreignKey:ChannelID;references:ChannelID"`
}

// TimeRange 是左闭右开的时间段 [From, To)
type TimeRange struct {
	From, To time.Time
}

type XMLTVEPG struct {
	XMLName    xml.Name        `xml:"tv"`
	Channels   []*XMLTVChannel `xml:"channel"`
	Programmes []*XMLTVProgram `xml:"programme"`
}

type XMLTVChannel struct {
	XMLName     xml.Name `xml:"channel"`
	ID          string   `xml:"id,attr"`
	DisplayName []string `xml:"display-name"`
}

type XMLTVProgram struct {
	XMLName xml.Name `xml:"programme"`
	Channel string   `xml:"channel,attr"`
	Start   string   `xml:"start,attr"`
	Stop    string   `xml:"stop,attr"`
	Title   string   `xml:"title"`
	Desc    string   `xml:"desc"`
}

type DIYPChannelEPG struct {
	ChannelName string         `json:"channel_name"`
	Date        string         `json:"date"`
	EPGData     []*DIYPProgram `json:"epg_data"`
}

type DIYPProgram struct {
	Start string `json:"start"` //  15:04
	End   string `json:"end"`   //  15:04
	Title string `json:"title"`
	Desc  string `json:"desc,omitempty"`
}
