package xmltv

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

// DefaultLocation 是时间字符串不带时区偏移时采用的时区（东八区）。
// 用固定偏移而不是 LoadLocation，避免精简镜像里没有 tzdata
var DefaultLocation = time.FixedZone("Asia/Shanghai", 8*3600)

// Window 限定要保留的节目：与 [From, To) 有重叠的才保留
type Window struct {
	From, To time.Time
}

type Channel struct {
	ID           string
	DisplayNames []string
	IconURL      string
}

type Programme struct {
	ChannelID   string
	Title       string
	Description string
	Category    string
	Start, Stop time.Time
}

type Result struct {
	Channels   []*Channel
	Programmes []*Programme // 只含落在窗口内的节目

	Total       int // 文件里的 <programme> 总数
	BadTime     int // 时间无法解析或 stop 不晚于 start 而被丢弃的数量
	OutOfWindow int // 落在窗口外而被丢弃的数量
}

type xmlChannel struct {
	ID           string   `xml:"id,attr"`
	DisplayNames []string `xml:"display-name"`
	Icon         struct {
		Src string `xml:"src,attr"`
	} `xml:"icon"`
}

type xmlProgramme struct {
	Channel    string   `xml:"channel,attr"`
	Start      string   `xml:"start,attr"`
	Stop       string   `xml:"stop,attr"`
	Titles     []string `xml:"title"`
	Descs      []string `xml:"desc"`
	Categories []string `xml:"category"`
}

// Parse 流式解析 XMLTV，逐个元素解码并立刻按窗口过滤，内存只与窗口内的节目量相关
func Parse(r io.Reader, win Window) (*Result, error) {
	dec := xml.NewDecoder(r)
	// 抓来的文件常有未转义的 & 之类的小毛病，宽松模式下尽量继续解析
	dec.Strict = false
	// 国内 IPTV 导出的文件常声明 encoding="GB2312" / "GBK"，标准库只认 UTF-8
	dec.CharsetReader = charset.NewReaderLabel

	res := &Result{}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("xmltv: parse: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		switch start.Name.Local {
		case "channel":
			var c xmlChannel
			if err := dec.DecodeElement(&c, &start); err != nil {
				return nil, fmt.Errorf("xmltv: parse channel: %w", err)
			}
			res.Channels = append(res.Channels, toChannel(&c))
		case "programme":
			var p xmlProgramme
			if err := dec.DecodeElement(&p, &start); err != nil {
				return nil, fmt.Errorf("xmltv: parse programme: %w", err)
			}
			res.Total++
			prog, ok := toProgramme(&p)
			switch {
			case !ok:
				res.BadTime++
			case !prog.Stop.After(win.From) || !prog.Start.Before(win.To):
				res.OutOfWindow++
			default:
				res.Programmes = append(res.Programmes, prog)
			}
		}
	}

	return res, nil
}

func toChannel(c *xmlChannel) *Channel {
	out := &Channel{ID: strings.TrimSpace(c.ID), IconURL: strings.TrimSpace(c.Icon.Src)}
	for _, name := range c.DisplayNames {
		if name = strings.TrimSpace(name); name != "" {
			out.DisplayNames = append(out.DisplayNames, name)
		}
	}
	return out
}

func toProgramme(p *xmlProgramme) (*Programme, bool) {
	title := firstNonEmpty(p.Titles)
	if title == "" || strings.TrimSpace(p.Channel) == "" {
		return nil, false
	}
	start, err := ParseTime(p.Start)
	if err != nil {
		return nil, false
	}
	stop, err := ParseTime(p.Stop)
	if err != nil || !stop.After(start) {
		return nil, false
	}
	return &Programme{
		ChannelID:   strings.TrimSpace(p.Channel),
		Title:       title,
		Description: firstNonEmpty(p.Descs),
		Category:    firstNonEmpty(p.Categories),
		Start:       start,
		Stop:        stop,
	}, true
}

func firstNonEmpty(ss []string) string {
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

// 带偏移的格式按偏移解析；不带偏移的按 DefaultLocation
var (
	layoutsWithOffset = []string{"20060102150405 -0700", "20060102150405-0700", "200601021504 -0700", "200601021504-0700"}
	layoutsNoOffset   = []string{"20060102150405", "200601021504"}
)

// ParseTime 解析 XMLTV 的时间，如 "20251022060000 +0800"。
// 也兼容偏移前没有空格、没有秒、没有偏移（按东八区）的写法
func ParseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range layoutsWithOffset {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	for _, layout := range layoutsNoOffset {
		if t, err := time.ParseInLocation(layout, s, DefaultLocation); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("xmltv: unrecognized time %q", s)
}
