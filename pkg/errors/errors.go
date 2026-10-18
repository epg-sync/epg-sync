package errors

import (
	"errors"
	"fmt"
	"maps"
	"runtime"
	"runtime/debug"
	"strings"
)

type ErrorCode string

const (
	// common errors (1xxx)
	ErrCodeUnknown       ErrorCode = "1000"
	ErrCodeInvalidParam  ErrorCode = "1001"
	ErrCodeNotFound      ErrorCode = "1002"
	ErrCodeAlreadyExists ErrorCode = "1003"
	ErrCodeUnauthorized  ErrorCode = "1004"
	ErrCodeForbidden     ErrorCode = "1005"

	// provider errors (2xxx)
	ErrCodeProviderNotFound          ErrorCode = "2001"
	ErrCodeProviderFetchFailed       ErrorCode = "2002"
	ErrCodeProviderParseFailed       ErrorCode = "2003"
	ErrCodeProviderTimeout           ErrorCode = "2004"
	ErrCodeProviderInvalidConfig     ErrorCode = "2005"
	ErrCodeProviderUnavailable       ErrorCode = "2006"
	ErrCodeProviderNotSupportChannel ErrorCode = "2007"

	// channel errors (3xxx)
	ErrCodeChannelNotFound  ErrorCode = "3001"
	ErrCodeChannelInvalid   ErrorCode = "3002"
	ErrCodeChannelDuplicate ErrorCode = "3003"

	// EPG errors (4xxx)
	ErrCodeEPGNotFound    ErrorCode = "4001"
	ErrCodeEPGInvalidDate ErrorCode = "4002"
	ErrCodeEPGParseFailed ErrorCode = "4003"

	// Cache errors (5xxx)
	ErrCodeCacheMiss        ErrorCode = "5001"
	ErrCodeCacheWriteFailed ErrorCode = "5002"
	ErrCodeCacheInvalid     ErrorCode = "5003"

	// Database errors (6xxx)
	ErrCodeDatabaseConnection  ErrorCode = "6001"
	ErrCodeDatabaseQuery       ErrorCode = "6002"
	ErrCodeDatabaseTransaction ErrorCode = "6003"

	// Network errors (7xxx)
	ErrCodeNetworkTimeout     ErrorCode = "7001"
	ErrCodeNetworkUnavailable ErrorCode = "7002"
	ErrCodeNetworkDNS         ErrorCode = "7003"
	ErrCodeNetworkHTTP        ErrorCode = "7004"
)

type AppError struct {
	Code       ErrorCode      `json:"code"`
	Message    string         `json:"message"`
	Err        error          `json:"-"`
	Details    map[string]any `json:"details,omitempty"`
	StackTrace string         `json:"stack_trace,omitempty"`
	File       string         `json:"file,omitempty"`
	Line       int            `json:"line,omitempty"`
}

func (e *AppError) Error() string {
	var parts []string

	parts = append(parts, fmt.Sprintf("[%s] %s", e.Code, e.Message))

	if len(e.Details) > 0 {
		var details []string
		for k, v := range e.Details {
			details = append(details, fmt.Sprintf("%s=%v", k, v))
		}
		parts = append(parts, fmt.Sprintf("(%s)", strings.Join(details, ", ")))
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	return strings.Join(parts, " ")
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func (e *AppError) WithDetail(key string, value any) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details[key] = value
	return e
}

func (e *AppError) WithDetails(details map[string]any) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	maps.Copy(e.Details, details)
	return e
}

func New(code ErrorCode, message string) *AppError {
	return newAppError(code, message, nil, 2)
}

func Newf(code ErrorCode, format string, args ...any) *AppError {
	return newAppError(code, fmt.Sprintf(format, args...), nil, 2)
}

func Wrap(err error, code ErrorCode, message string) *AppError {
	if err == nil {
		return nil
	}
	return newAppError(code, message, err, 2)
}

func Wrapf(err error, code ErrorCode, format string, args ...any) *AppError {
	if err == nil {
		return nil
	}
	return newAppError(code, fmt.Sprintf(format, args...), err, 2)
}

func newAppError(code ErrorCode, message string, err error, skip int) *AppError {

	_, file, line, _ := runtime.Caller(skip)

	appErr := &AppError{
		Code:    code,
		Message: message,
		Err:     err,
		File:    file,
		Line:    line,
	}

	appErr.StackTrace = string(debug.Stack())

	return appErr
}

func Is(err error, code ErrorCode) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == code
	}
	return false
}

func GetCode(err error) ErrorCode {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return ErrCodeUnknown
}

func GetDetails(err error) map[string]any {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Details
	}
	return nil
}

func HTTPRequestFailed(err error, url string, statusCode int, message string) *AppError {
	if err != nil {
		return Wrap(err, ErrCodeNetworkHTTP, "HTTP request failed").
			WithDetails(map[string]any{
				"url":         url,
				"status_code": statusCode,
				"message":     message,
			})
	}
	return New(ErrCodeNetworkHTTP, "HTTP request failed").
		WithDetails(map[string]any{
			"url":         url,
			"status_code": statusCode,
			"message":     message,
		})

}

func ProviderNotFound(providerID string) *AppError {
	return New(ErrCodeProviderNotFound, "provider not found").
		WithDetail("provider_id", providerID)
}

func ProviderAPIError(providerID string, statusCode string, message string) *AppError {
	return New(ErrCodeProviderFetchFailed, "provider API error").
		WithDetails(map[string]any{
			"provider_id": providerID,
			"status_code": statusCode,
			"message":     message,
		})
}

func ProviderNotSupportChannel(providerID, providerChannelID, channelID string) *AppError {
	return New(ErrCodeProviderNotSupportChannel, "provider does not support channel").
		WithDetails(map[string]any{
			"provider_id":         providerID,
			"provider_channel_id": providerChannelID,
			"channel_id":          channelID,
		})
}

func ProviderParseFailed(providerID string, err error) *AppError {
	return Wrap(err, ErrCodeProviderParseFailed, "failed to parse provider data")
}

func ProviderTimeout(providerID string, timeout int) *AppError {
	return New(ErrCodeProviderTimeout, "provider request timeout").
		WithDetails(map[string]any{
			"provider_id": providerID,
			"timeout":     timeout,
		})
}

func ProviderInvalidConfig(providerID string, reason string) *AppError {
	return New(ErrCodeProviderInvalidConfig, "invalid provider config").
		WithDetails(map[string]any{
			"provider_id": providerID,
			"reason":      reason,
		})
}

func ProviderHealthCheckFailed(providerID string, message string) *AppError {
	return New(ErrCodeProviderUnavailable, "provider unavailable").
		WithDetails(map[string]any{
			"provider_id": providerID,
			"message":     message,
		})
}

func ChannelNotFound(channelID string) *AppError {
	return New(ErrCodeChannelNotFound, "channel not found").
		WithDetail("channel_id", channelID)
}

func ChannelInvalid(channelID string, reason string) *AppError {
	return New(ErrCodeChannelInvalid, "invalid channel").
		WithDetails(map[string]any{
			"channel_id": channelID,
			"reason":     reason,
		})
}

func EPGNotFound(channelID string, date string) *AppError {
	return New(ErrCodeEPGNotFound, "EPG data not found").
		WithDetails(map[string]any{
			"channel_id": channelID,
			"date":       date,
		})
}

func ErrProgramDateRangeProcess(err error, channelID string, date string) *AppError {
	// 保留原始错误，日志里才能看到具体是哪个时间解析失败
	return Wrap(err, ErrCodeEPGInvalidDate, "failed to process program date range").
		WithDetails(map[string]any{
			"channel_id": channelID,
			"date":       date,
		})
}

func ErrProgramLoadLocation(channelID string, err error) *AppError {
	return Wrap(err, ErrCodeEPGInvalidDate, "failed to load location").
		WithDetails(map[string]any{
			"channel_id": channelID,
		})
}

func ChannelMappingNotFound(channelID string) *AppError {
	return New(ErrCodeChannelNotFound, "channel mapping not found").
		WithDetail("channel_id", channelID)
}

func EPGInvalidDate(date string) *AppError {
	return New(ErrCodeEPGInvalidDate, "invalid date format").
		WithDetail("date", date)
}

func CacheMiss(key string) *AppError {
	return New(ErrCodeCacheMiss, "cache miss").
		WithDetail("key", key)
}

func CacheWriteFailed(key string, err error) *AppError {
	return Wrap(err, ErrCodeCacheWriteFailed, "failed to write cache").
		WithDetail("key", key)
}

func InvalidParam(param string, reason string) *AppError {
	return New(ErrCodeInvalidParam, "invalid parameter").
		WithDetails(map[string]any{
			"param":  param,
			"reason": reason,
		})
}

func NotFound(resource string, id string) *AppError {
	return New(ErrCodeNotFound, fmt.Sprintf("%s not found", resource)).
		WithDetail("id", id)
}

func AlreadyExists(resource string, id string) *AppError {
	return New(ErrCodeAlreadyExists, fmt.Sprintf("%s already exists", resource)).
		WithDetail("id", id)
}

func HTTPStatus(err error) int {
	code := GetCode(err)

	switch code {
	case ErrCodeNotFound, ErrCodeProviderNotFound,
		ErrCodeChannelNotFound, ErrCodeEPGNotFound, ErrCodeCacheMiss:
		return 404

	case ErrCodeInvalidParam, ErrCodeChannelInvalid,
		ErrCodeEPGInvalidDate, ErrCodeProviderInvalidConfig:
		return 400

	case ErrCodeAlreadyExists, ErrCodeChannelDuplicate:
		return 409

	case ErrCodeUnauthorized:
		return 401

	case ErrCodeForbidden:
		return 403

	case ErrCodeProviderTimeout, ErrCodeNetworkTimeout:
		return 504

	case ErrCodeProviderUnavailable, ErrCodeNetworkUnavailable:
		return 503

	default:
		return 500
	}
}

func IsRetryable(err error) bool {
	code := GetCode(err)

	switch code {
	case ErrCodeProviderTimeout, ErrCodeNetworkTimeout,
		ErrCodeProviderUnavailable, ErrCodeNetworkUnavailable,
		ErrCodeDatabaseConnection:
		return true
	default:
		return false
	}
}

func IsTemporary(err error) bool {
	code := GetCode(err)

	switch code {
	case ErrCodeCacheMiss, ErrCodeProviderTimeout,
		ErrCodeNetworkTimeout:
		return true
	default:
		return false
	}
}
