package xmltv

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const sampleXML = `<?xml version="1.0" encoding="UTF-8"?>
<tv generator-info-name="test">
  <channel id="1001">
    <display-name lang="zh">CCTV-1高清</display-name>
    <display-name>CCTV1</display-name>
    <icon src="http://logo.test/cctv1.png"/>
  </channel>
  <channel id="2002"><display-name>广东本地台</display-name></channel>
  <programme start="20251022060000 +0800" stop="20251022070000 +0800" channel="1001">
    <title lang="zh"> 朝闻天下 </title>
    <desc>早间新闻</desc>
    <category>新闻</category>
  </programme>
  <programme start="20251022070000+0800" stop="20251022080000+0800" channel="1001"><title>无空格偏移</title></programme>
  <programme start="202510220800" stop="202510220900" channel="2002"><title>无偏移按东八区</title></programme>
  <programme start="garbage" stop="20251022100000 +0800" channel="2002"><title>坏时间</title></programme>
  <programme start="20251022110000 +0800" stop="20251022100000 +0800" channel="2002"><title>stop早于start</title></programme>
  <programme start="20251022120000 +0800" stop="20251022130000 +0800" channel="2002"><title></title></programme>
  <programme start="20240101000000 +0800" stop="20240101010000 +0800" channel="2002"><title>窗口外</title></programme>
</tv>`

func cst(h, m int) time.Time {
	return time.Date(2025, 10, 22, h, m, 0, 0, DefaultLocation)
}

func TestParse(t *testing.T) {
	win := Window{From: cst(0, 0), To: cst(0, 0).AddDate(0, 0, 1)}
	res, err := Parse(strings.NewReader(sampleXML), win)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Channels) != 2 {
		t.Fatalf("channels = %d, want 2", len(res.Channels))
	}
	c := res.Channels[0]
	if c.ID != "1001" || c.IconURL != "http://logo.test/cctv1.png" ||
		len(c.DisplayNames) != 2 || c.DisplayNames[0] != "CCTV-1高清" || c.DisplayNames[1] != "CCTV1" {
		t.Errorf("channel[0] = %+v", c)
	}

	if res.Total != 7 || res.BadTime != 3 || res.OutOfWindow != 1 {
		t.Errorf("total/bad/out = %d/%d/%d, want 7/3/1", res.Total, res.BadTime, res.OutOfWindow)
	}
	if len(res.Programmes) != 3 {
		t.Fatalf("programmes = %d, want 3: %+v", len(res.Programmes), res.Programmes)
	}

	p := res.Programmes[0]
	if p.ChannelID != "1001" || p.Title != "朝闻天下" || p.Description != "早间新闻" || p.Category != "新闻" ||
		!p.Start.Equal(cst(6, 0)) || !p.Stop.Equal(cst(7, 0)) {
		t.Errorf("programme[0] = %+v", p)
	}
	if !res.Programmes[1].Start.Equal(cst(7, 0)) {
		t.Errorf("offset without space: start = %v", res.Programmes[1].Start)
	}
	if !res.Programmes[2].Start.Equal(cst(8, 0)) {
		t.Errorf("no offset should default to UTC+8: start = %v", res.Programmes[2].Start)
	}
}

func TestParseTimeKeepsOffset(t *testing.T) {
	got, err := ParseTime("20251022060000 -0500")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2025, 10, 22, 11, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("got %v, want %v", got.UTC(), want)
	}
}

// 国内 IPTV 导出的文件常用 GB2312 / GBK / GB18030 编码
func TestParseChineseEncodings(t *testing.T) {
	doc := `<?xml version="1.0" encoding="%s"?><tv>
  <channel id="1"><display-name>广东卫视</display-name></channel>
  <programme start="20251022060000 +0800" stop="20251022070000 +0800" channel="1"><title>珠江新闻</title></programme>
</tv>`
	encoders := map[string]*encoding.Encoder{
		"GB2312":  simplifiedchinese.GBK.NewEncoder(),
		"GBK":     simplifiedchinese.GBK.NewEncoder(),
		"gb18030": simplifiedchinese.GB18030.NewEncoder(),
	}
	win := Window{From: cst(0, 0), To: cst(0, 0).AddDate(0, 0, 1)}
	for label, enc := range encoders {
		raw, err := enc.String(fmt.Sprintf(doc, label))
		if err != nil {
			t.Fatal(err)
		}
		res, err := Parse(strings.NewReader(raw), win)
		if err != nil {
			t.Errorf("%s: %v", label, err)
			continue
		}
		if len(res.Channels) != 1 || res.Channels[0].DisplayNames[0] != "广东卫视" ||
			len(res.Programmes) != 1 || res.Programmes[0].Title != "珠江新闻" {
			t.Errorf("%s: decoded wrong: channels=%+v programmes=%+v", label, res.Channels, res.Programmes)
		}
	}
}

func TestParseBrokenXML(t *testing.T) {
	_, err := Parse(strings.NewReader(`<tv><programme start="x"`), Window{})
	if err == nil {
		t.Fatal("expected error for truncated xml")
	}
}

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fetchAll(t *testing.T, url string) ([]byte, error) {
	t.Helper()
	rc, err := Fetch(context.Background(), url, "test-agent")
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func TestFetch(t *testing.T) {
	plain := []byte(sampleXML)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "test-agent" {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/epg.xml":
			w.Write(plain)
		case "/epg.xml.gz":
			w.Write(gz(t, sampleXML))
		case "/no-suffix": // 后缀、响应头都看不出是 gzip，靠魔数识别
			w.Header().Set("Content-Type", "text/plain")
			w.Write(gz(t, sampleXML))
		case "/double": // 服务器声明了 Content-Encoding，内容本身还是 gz
			w.Header().Set("Content-Encoding", "gzip")
			w.Write(gz(t, string(gz(t, sampleXML))))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	for _, path := range []string{"/epg.xml", "/epg.xml.gz", "/no-suffix", "/double"} {
		got, err := fetchAll(t, srv.URL+path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if !bytes.Equal(got, plain) {
			t.Errorf("%s: content mismatch (%d bytes)", path, len(got))
		}
	}

	if _, err := fetchAll(t, srv.URL+"/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404: err = %v", err)
	}
}

func TestFetchRejectsBadScheme(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com/epg.xml", "example.com/epg.xml", ""} {
		if _, err := Fetch(context.Background(), u, ""); err == nil {
			t.Errorf("Fetch(%q) should fail", u)
		}
	}
}

func TestFetchSizeLimit(t *testing.T) {
	old := maxDecompressedBytes
	maxDecompressedBytes = 100
	defer func() { maxDecompressedBytes = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exact" {
			w.Write(bytes.Repeat([]byte("a"), 100))
			return
		}
		// 压缩后很小、解压后超限
		w.Write(gz(t, strings.Repeat("a", 1000)))
	}))
	defer srv.Close()

	if got, err := fetchAll(t, srv.URL+"/exact"); err != nil || len(got) != 100 {
		t.Errorf("exact limit: len=%d err=%v", len(got), err)
	}
	if _, err := fetchAll(t, srv.URL+"/bomb"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("bomb: err = %v, want ErrTooLarge", err)
	}
}
