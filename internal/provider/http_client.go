package provider

import (
	"bytes"
	"context"
	stderrors "errors"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/logger"
)

// defaultRetryBackoff 是第一次重试前的等待时间，之后每次翻倍
const defaultRetryBackoff = 500 * time.Millisecond

type HTTPClient struct {
	client       *http.Client
	baseURL      string
	headers      map[string]string
	timeout      time.Duration
	maxRetries   int
	retryBackoff time.Duration
	mu           sync.Mutex
}

func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		baseURL:      baseURL,
		headers:      make(map[string]string),
		timeout:      timeout,
		retryBackoff: defaultRetryBackoff,
	}
}

// SetMaxRetries 设置网络层错误（DNS 解析失败、连接被拒绝或重置）的重试次数，0 表示不重试
func (c *HTTPClient) SetMaxRetries(n int) {
	c.maxRetries = max(n, 0)
}

func (c *HTTPClient) SetHeader(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.headers[key] = value
}

func (c *HTTPClient) SetHeaders(headers map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	maps.Copy(c.headers, headers)
}

func (c *HTTPClient) Get(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	return c.GetWithHeaders(ctx, path, params, nil)
}

func (c *HTTPClient) GetWithHeaders(ctx context.Context, path string, params map[string]string, headers map[string]string) ([]byte, error) {
	if len(params) > 0 {
		values := url.Values{}
		for k, v := range params {
			values.Add(k, v)
		}
		path += "?" + values.Encode()
	}
	return c.doRequest(ctx, http.MethodGet, path, nil, headers)
}

func (c *HTTPClient) Post(ctx context.Context, path string, body io.Reader) ([]byte, error) {
	return c.PostWithHeaders(ctx, path, body, nil)
}

func (c *HTTPClient) PostWithHeaders(ctx context.Context, path string, body io.Reader, headers map[string]string) ([]byte, error) {
	return c.doRequest(ctx, http.MethodPost, path, body, headers)
}

func (c *HTTPClient) doRequest(ctx context.Context, method, path string, body io.Reader, headers map[string]string) ([]byte, error) {
	var requestURL string
	if !strings.HasPrefix(path, "http") {
		requestURL = c.baseURL + path
	} else {
		requestURL = path
	}

	// 请求体要在重试时重新发送，先读到内存里
	var payload []byte
	if body != nil {
		var err error
		payload, err = io.ReadAll(body)
		if err != nil {
			return nil, errors.HTTPRequestFailed(err, requestURL, 0, "failed to read request body")
		}
	}

	var resp *http.Response
	for attempt := 0; ; attempt++ {
		req, err := c.newRequest(ctx, method, requestURL, payload, headers)
		if err != nil {
			return nil, errors.HTTPRequestFailed(err, requestURL, 0, "failed to create request")
		}

		logger.Debug("HTTP Request",
			logger.String("method", method),
			logger.String("url", requestURL),
		)
		resp, err = c.client.Do(req)
		if err == nil {
			break
		}
		if attempt >= c.maxRetries || ctx.Err() != nil || !isRetryableNetError(err) {
			return nil, errors.HTTPRequestFailed(err, requestURL, 0, "failed to do request")
		}

		wait := c.retryBackoff << attempt
		logger.Warn("HTTP request failed, retrying",
			logger.String("url", requestURL),
			logger.Int("attempt", attempt+1),
			logger.Duration("backoff", wait),
			logger.Err(err),
		)
		select {
		case <-ctx.Done():
			return nil, errors.HTTPRequestFailed(ctx.Err(), requestURL, 0, "failed to do request")
		case <-time.After(wait):
		}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.HTTPRequestFailed(err, requestURL, resp.StatusCode, "failed to read response body")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.HTTPRequestFailed(nil, requestURL, resp.StatusCode, string(data))
	}

	return data, nil
}

func (c *HTTPClient) newRequest(ctx context.Context, method, requestURL string, payload []byte, headers map[string]string) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	c.mu.Unlock()

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if payload != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// isRetryableNetError 只对还没拿到响应的网络层错误返回 true：
// DNS 超时/临时失败、建连失败、连接被对端重置或提前关闭。
// 整体请求超时（http.Client.Timeout）不重试，否则慢的 provider 会把同步时间拖长数倍；
// 域名不存在、证书错误这类重试也不会好的错误同样不重试。
func isRetryableNetError(err error) bool {
	var dnsErr *net.DNSError
	if stderrors.As(err, &dnsErr) {
		return !dnsErr.IsNotFound
	}

	var netErr net.Error
	if stderrors.As(err, &netErr) && netErr.Timeout() {
		return false
	}

	var opErr *net.OpError
	if stderrors.As(err, &opErr) && opErr.Op == "dial" {
		return true
	}

	return stderrors.Is(err, syscall.ECONNRESET) ||
		stderrors.Is(err, io.EOF) ||
		stderrors.Is(err, io.ErrUnexpectedEOF)
}

func (c *HTTPClient) Close() {
	c.client.CloseIdleConnections()
}
