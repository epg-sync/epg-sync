// Package xmltv 下载并解析 XMLTV 格式的节目单文件（支持 gzip 压缩）。
package xmltv

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const fetchTimeout = 120 * time.Second

// maxDecompressedBytes 限制解压后的数据量，防止异常文件或压缩炸弹耗尽内存/磁盘。测试里会调小
var maxDecompressedBytes int64 = 256 << 20

var ErrTooLarge = errors.New("xmltv: file too large after decompression")

// ValidateURL 只接受带主机名的 http/https 地址。不限制内网地址：文件常放在局域网的文件服务器上
func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid url %q, only http/https is supported", rawURL)
	}
	return nil
}

// Fetch 下载 rawURL。响应体以 gzip 魔数 1f 8b 开头就自动解压，不依赖 URL 后缀或响应头，
// 所以 .xml 和 .xml.gz 都能处理，服务器多套一层 Content-Encoding 也没问题
func Fetch(ctx context.Context, rawURL, userAgent string) (io.ReadCloser, error) {
	if err := ValidateURL(rawURL); err != nil {
		return nil, fmt.Errorf("xmltv: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("xmltv: build request: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	client := &http.Client{Timeout: fetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xmltv: download: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("xmltv: download: unexpected status %s", resp.Status)
	}

	return openBody(resp.Body)
}

// openBody 按魔数决定是否套一层 gzip，并限制解压后的大小。返回的 Closer 会关闭底层 body
func openBody(body io.ReadCloser) (io.ReadCloser, error) {
	br := bufio.NewReader(body)
	magic, err := br.Peek(2)
	if err != nil && err != io.EOF {
		body.Close()
		return nil, fmt.Errorf("xmltv: read response: %w", err)
	}

	var r io.Reader = br
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			body.Close()
			return nil, fmt.Errorf("xmltv: open gzip: %w", err)
		}
		r = gz
	}

	return &limitedReadCloser{r: r, left: maxDecompressedBytes, closer: body}, nil
}

type limitedReadCloser struct {
	r      io.Reader
	left   int64
	closer io.Closer
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	if l.left <= 0 {
		// 读满上限后还有数据才算超限，刚好等于上限的文件正常结束
		var one [1]byte
		if n, _ := l.r.Read(one[:]); n > 0 {
			return 0, ErrTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	return n, err
}

func (l *limitedReadCloser) Close() error { return l.closer.Close() }
