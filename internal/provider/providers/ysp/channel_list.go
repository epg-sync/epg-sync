package ysp

import "github.com/epg-sync/epgsync/internal/model"

var channelList = []*model.ProviderChannel{
	{
		ID:      "600001859",
		Name:    "CCTV-1综合",
		Aliases: []string{"CCTV1", "CCTV-1"},
	},
	{
		ID:      "600001800",
		Name:    "CCTV-2财经",
		Aliases: []string{"CCTV2", "CCTV-2"},
	},
	{
		ID:      "600001801",
		Name:    "CCTV-3综艺",
		Aliases: []string{"CCTV3", "CCTV-3"},
	},
	{
		ID:      "600001814",
		Name:    "CCTV-4中文国际",
		Aliases: []string{"CCTV4亚洲", "CCTV-4中文国际", "CCTV-4中文国际(亚)"},
	},
	{
		ID:      "600001818",
		Name:    "CCTV-5体育",
		Aliases: []string{"CCTV5", "CCTV-5"},
	},
	{
		ID:      "600001817",
		Name:    "CCTV-5体育赛事",
		Aliases: []string{"CCTV5+", "CCTV-5+", "CCTV5plus"},
	},
	{
		ID:      "600108442",
		Name:    "CCTV-6电影",
		Aliases: []string{"CCTV6", "CCTV-6"},
	},
	{
		ID:      "600004092",
		Name:    "CCTV-7军事农业",
		Aliases: []string{"CCTV7", "CCTV-7"},
	},
	{
		ID:      "600001803",
		Name:    "CCTV-8电视剧",
		Aliases: []string{"CCTV8", "CCTV-8"},
	},
	{
		ID:      "600004078",
		Name:    "CCTV-9纪录",
		Aliases: []string{"CCTV9", "CCTV-9"},
	},
	{
		ID:      "600001805",
		Name:    "CCTV-10科教",
		Aliases: []string{"CCTV10", "CCTV-10"},
	},
	{
		ID:      "600001806",
		Name:    "CCTV-11戏曲",
		Aliases: []string{"CCTV11", "CCTV-11"},
	},
	{
		ID:      "600001807",
		Name:    "CCTV-12社会与法",
		Aliases: []string{"CCTV12", "CCTV-12"},
	},
	{
		ID:      "600001811",
		Name:    "CCTV-13新闻",
		Aliases: []string{"CCTV13", "CCTV-13"},
	},
	{
		ID:      "600001809",
		Name:    "CCTV-14少儿",
		Aliases: []string{"CCTV14", "CCTV-14"},
	},
	{
		ID:      "600001815",
		Name:    "CCTV-15音乐",
		Aliases: []string{"CCTV15", "CCTV-15"},
	},
	{
		ID:      "600098637",
		Name:    "CCTV-16奥林匹克",
		Aliases: []string{"CCTV16", "CCTV-16"},
	},
	{
		ID:      "600001810",
		Name:    "CCTV-17农业农村",
		Aliases: []string{"CCTV17", "CCTV-17"},
	},
	{
		ID:      "600002264",
		Name:    "CCTV4K 超高清",
		Aliases: []string{"CCTV4K"},
	},
	{
		ID:      "600156816",
		Name:    "CCTV8K 超高清",
		Aliases: []string{"CCTV8K"},
	},
	{
		ID:      "600099658",
		Name:    "CCTV风云剧场",
		Aliases: []string{"风云剧场"},
	},
	{
		ID:      "600099655",
		Name:    "CCTV第一剧场",
		Aliases: []string{"第一剧场"},
	},
	{
		ID:      "600099620",
		Name:    "CCTV怀旧剧场",
		Aliases: []string{"怀旧剧场"},
	},
	{
		ID:      "600099637",
		Name:    "CCTV世界地理",
		Aliases: []string{"世界地理"},
	},
	{
		ID:      "600099660",
		Name:    "CCTV风云音乐",
		Aliases: []string{"风云音乐"},
	},
	{
		ID:      "600099649",
		Name:    "CCTV兵器科技",
		Aliases: []string{"兵器科技"},
	},
	{
		ID:      "600099636",
		Name:    "CCTV风云足球",
		Aliases: []string{"风云足球"},
	},
	{
		ID:      "600099659",
		Name:    "CCTV高尔夫网球",
		Aliases: []string{"高尔夫网球"},
	},
	{
		ID:      "600099650",
		Name:    "CCTV女性时尚",
		Aliases: []string{"女性时尚"},
	},
	{
		ID:      "600099653",
		Name:    "CCTV央视文化精品",
		Aliases: []string{"央视文化精品"},
	},
	{
		ID:      "600099652",
		Name:    "CCTV央视台球",
		Aliases: []string{"央视台球"},
	},
	{
		ID:      "600099656",
		Name:    "CCTV电视指南",
		Aliases: []string{"电视指南"},
	},
	{
		ID:      "600099651",
		Name:    "CCTV卫生健康",
		Aliases: []string{"卫生健康"},
	},
	{
		ID:   "600002309",
		Name: "北京卫视",
	},
	{
		ID:   "600002521",
		Name: "江苏卫视",
	},
	{
		ID:   "600002483",
		Name: "东方卫视",
	},
	{
		ID:   "600002520",
		Name: "浙江卫视",
	},
	{
		ID:   "600002475",
		Name: "湖南卫视",
	},
	{
		ID:   "600002508",
		Name: "湖北卫视",
	},
	{
		ID:   "600002485",
		Name: "广东卫视",
	},
	{
		ID:   "600002509",
		Name: "广西卫视",
	},
	{
		ID:   "600002498",
		Name: "黑龙江卫视",
	},
	{
		ID:   "600002506",
		Name: "海南卫视",
	},
	{
		ID:   "600002531",
		Name: "重庆卫视",
	},
	{
		ID:   "600002481",
		Name: "深圳卫视",
	},
	{
		ID:   "600002516",
		Name: "四川卫视",
	},
	{
		ID:   "600002525",
		Name: "河南卫视",
	},
	{
		ID:   "600002484",
		Name: "东南卫视",
	},
	{
		ID:   "600002490",
		Name: "贵州卫视",
	},
	{
		ID:   "600002503",
		Name: "江西卫视",
	},

	{
		ID:   "600002505",
		Name: "辽宁卫视",
	},
	{
		ID:   "600002532",
		Name: "安徽卫视",
	},
	{
		ID:   "600002493",
		Name: "河北卫视",
	},
	{
		ID:   "600002513",
		Name: "山东卫视",
	},
	{
		ID:   "600152137",
		Name: "天津卫视",
	},

	{
		ID:   "600190405",
		Name: "吉林卫视",
	},
	{
		ID:   "600190400",
		Name: "陕西卫视",
	},
	{
		ID:   "600190737",
		Name: "宁夏卫视",
	},
	{
		ID:   "600190401",
		Name: "内蒙古卫视",
	},
	{
		ID:   "600190402",
		Name: "云南卫视",
	},
	{
		ID:   "600190407",
		Name: "山西卫视",
	},
	{
		ID:   "600190406",
		Name: "青海卫视",
	},
	{
		ID:   "600190403",
		Name: "西藏卫视",
	},
	{
		ID:   "600152138",
		Name: "新疆卫视",
	},
	{
		ID:   "600171827",
		Name: "CETV1",
	},
	{
		ID:   "600014550",
		Name: "CGTN",
	},
	{
		ID:   "600084704",
		Name: "CGTN法语",
	},
	{
		ID:   "600084758",
		Name: "CGTN俄语",
	},
	{
		ID:      "600084782",
		Name:    "CGTN阿拉伯语",
		Aliases: []string{"CGTN阿语"},
	},
	{
		ID:      "600084744",
		Name:    "CGTN西班牙语",
		Aliases: []string{"CGTN西语"},
	},
	{
		ID:      "600084781",
		Name:    "CGTN外语纪录",
		Aliases: []string{"CGTN记录"},
	},
	{
		ID:   "600193252",
		Name: "兵团卫视",
	},
	{
		ID:   "600190408",
		Name: "甘肃卫视",
	},
}
