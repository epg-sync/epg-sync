package model

type Timezone struct {
	ID        int64  `json:"id" gorm:"column:id;primaryKey;autoIncrement;not null"`
	Name      string `json:"name" gorm:"column:name;not null"`
	GmtOffset int    `json:"gmt_offset" gorm:"column:gmt_offset;not null"`
	TzName    string `json:"tz_name" gorm:"column:tz_name;default:null"`
	Visible   int    `json:"visible" gorm:"column:visible;"`
}
