package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

type Level = zapcore.Level

const (
	DebugLevel = zapcore.DebugLevel
	InfoLevel  = zapcore.InfoLevel
	WarnLevel  = zapcore.WarnLevel
	ErrorLevel = zapcore.ErrorLevel
	PanicLevel = zapcore.PanicLevel
	FatalLevel = zapcore.FatalLevel
)

type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	Panic(msg string, fields ...Field)

	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
	Panicf(format string, args ...interface{})

	With(fields ...Field) Logger
	Named(name string) Logger
	WithContext(ctx context.Context) Logger
	Sync() error
}

var (
	String     = zap.String
	Int        = zap.Int
	Int64      = zap.Int64
	Float64    = zap.Float64
	Bool       = zap.Bool
	Time       = zap.Time
	Duration   = zap.Duration
	Err        = zap.Error
	Any        = zap.Any
	Strings    = zap.Strings
	Ints       = zap.Ints
	ByteString = zap.ByteString
	Reflect    = zap.Reflect
	Namespace  = zap.Namespace
	Skip       = zap.Skip
)

type Field = zap.Field

type zapLogger struct {
	logger *zap.Logger
	sugar  *zap.SugaredLogger
	level  Level
}

type Config struct {
	Level            string `json:"level" yaml:"level"`
	Format           string `json:"format" yaml:"format"`
	Output           string `json:"output" yaml:"output"`
	Filename         string `json:"filename" yaml:"filename"`
	MaxSize          int    `json:"max_size" yaml:"max_size"`
	MaxBackups       int    `json:"max_backups" yaml:"max_backups"`
	MaxAge           int    `json:"max_age" yaml:"max_age"`
	Compress         bool   `json:"compress" yaml:"compress"`
	EnableCaller     bool   `json:"enable_caller" yaml:"enable_caller"`
	EnableStackTrace bool   `json:"enable_stack_trace" yaml:"enable_stack_trace"`
	StackTraceLevel  Level  `json:"stack_trace_level" yaml:"stack_trace_level"`

	// 管理后台日志（保存在数据库中）的保留天数
	RetentionDays int `json:"retention_days" yaml:"retention_days"`
}

func DefaultConfig() *Config {
	return &Config{
		Level:            "info",
		Format:           "json",
		Output:           "stdout",
		MaxSize:          100,
		MaxBackups:       10,
		MaxAge:           30,
		Compress:         true,
		EnableCaller:     true,
		EnableStackTrace: true,
		StackTraceLevel:  ErrorLevel,
	}
}

var (
	globalLogger Logger
	once         sync.Once
)

// Init 初始化全局 logger，extra 是额外并列的输出（例如 Hub.Core）
func Init(cfg *Config, extra ...zapcore.Core) error {
	var err error
	once.Do(func() {
		globalLogger, err = NewLogger(cfg, extra...)
	})
	return err
}

func NewLogger(cfg *Config, extra ...zapcore.Core) (Logger, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     customTimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   customCallerEncoder,
	}

	var encoder zapcore.Encoder
	if cfg.Format == "console" {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	var writers []zapcore.WriteSyncer

	switch cfg.Output {
	case "stdout":
		writers = append(writers, zapcore.AddSync(os.Stdout))
	case "stderr":
		writers = append(writers, zapcore.AddSync(os.Stderr))
	case "file":
		if cfg.Filename == "" {
			cfg.Filename = "logs/app.log"
		}

		dir := filepath.Dir(cfg.Filename)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}

		fileWriter := &lumberjack.Logger{
			Filename:   cfg.Filename,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAge,
			Compress:   cfg.Compress,
		}
		writers = append(writers, zapcore.AddSync(fileWriter))
	default:
		writers = append(writers, zapcore.AddSync(os.Stdout))

		if cfg.Filename != "" {
			dir := filepath.Dir(cfg.Filename)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create log directory: %w", err)
			}

			fileWriter := &lumberjack.Logger{
				Filename:   cfg.Filename,
				MaxSize:    cfg.MaxSize,
				MaxBackups: cfg.MaxBackups,
				MaxAge:     cfg.MaxAge,
				Compress:   cfg.Compress,
			}
			writers = append(writers, zapcore.AddSync(fileWriter))
		}
	}
	core := zapcore.NewCore(
		encoder,
		zapcore.NewMultiWriteSyncer(writers...),
		ParseLevel(cfg.Level),
	)
	if len(extra) > 0 {
		core = zapcore.NewTee(append([]zapcore.Core{core}, extra...)...)
	}

	opts := []zap.Option{
		zap.AddCallerSkip(1),
	}

	if cfg.EnableCaller {
		opts = append(opts, zap.AddCaller())
	}

	if cfg.EnableStackTrace {
		opts = append(opts, zap.AddStacktrace(cfg.StackTraceLevel))
	}

	zapLog := zap.New(core, opts...)

	return &zapLogger{
		logger: zapLog,
		sugar:  zapLog.Sugar(),
		level:  ParseLevel(cfg.Level),
	}, nil
}

func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
}

func customCallerEncoder(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(caller.TrimmedPath())
}

func (l *zapLogger) Debug(msg string, fields ...Field) {
	l.logger.Debug(msg, fields...)
}

func (l *zapLogger) Info(msg string, fields ...Field) {
	l.logger.Info(msg, fields...)
}

func (l *zapLogger) Warn(msg string, fields ...Field) {
	l.logger.Warn(msg, fields...)
}

func (l *zapLogger) Error(msg string, fields ...Field) {
	l.logger.Error(msg, fields...)
}

func (l *zapLogger) Fatal(msg string, fields ...Field) {
	l.logger.Fatal(msg, fields...)
}

func (l *zapLogger) Panic(msg string, fields ...Field) {
	l.logger.Panic(msg, fields...)
}

func (l *zapLogger) Debugf(format string, args ...interface{}) {
	l.sugar.Debugf(format, args...)
}

func (l *zapLogger) Infof(format string, args ...interface{}) {
	l.sugar.Infof(format, args...)
}

func (l *zapLogger) Warnf(format string, args ...interface{}) {
	l.sugar.Warnf(format, args...)
}

func (l *zapLogger) Errorf(format string, args ...interface{}) {
	l.sugar.Errorf(format, args...)
}

func (l *zapLogger) Fatalf(format string, args ...interface{}) {
	l.sugar.Fatalf(format, args...)
}

func (l *zapLogger) Panicf(format string, args ...interface{}) {
	l.sugar.Panicf(format, args...)
}

func (l *zapLogger) With(fields ...Field) Logger {
	with := l.logger.With(fields...)
	return &zapLogger{
		logger: with,
		sugar:  with.Sugar(),
		level:  l.level,
	}
}

func (l *zapLogger) Named(name string) Logger {
	named := l.logger.Named(name)
	return &zapLogger{
		logger: named,
		sugar:  named.Sugar(),
		level:  l.level,
	}
}

func (l *zapLogger) WithContext(ctx context.Context) Logger {
	fields := []Field{}

	if traceID := ctx.Value("trace_id"); traceID != nil {
		fields = append(fields, zap.String("trace_id", traceID.(string)))
	}

	if userID := ctx.Value("user_id"); userID != nil {
		fields = append(fields, zap.String("user_id", userID.(string)))
	}

	if requestID := ctx.Value("request_id"); requestID != nil {
		fields = append(fields, zap.String("request_id", requestID.(string)))
	}

	return l.With(fields...)
}

func (l *zapLogger) Sync() error {
	return l.logger.Sync()
}

// ============ global methods ============

func Default() Logger {
	if globalLogger == nil {
		Init(DefaultConfig())
	}
	return globalLogger
}

func SetDefault(logger Logger) {
	globalLogger = logger
}

func Debug(msg string, fields ...Field) {
	Default().Debug(msg, fields...)
}

func Info(msg string, fields ...Field) {
	Default().Info(msg, fields...)
}

func Warn(msg string, fields ...Field) {
	Default().Warn(msg, fields...)
}

func Error(msg string, fields ...Field) {
	Default().Error(msg, fields...)
}

func Fatal(msg string, fields ...Field) {
	Default().Fatal(msg, fields...)
}

func Panic(msg string, fields ...Field) {
	Default().Panic(msg, fields...)
}

func Debugf(format string, args ...interface{}) {
	Default().Debugf(format, args...)
}

func Infof(format string, args ...interface{}) {
	Default().Infof(format, args...)
}

func Warnf(format string, args ...interface{}) {
	Default().Warnf(format, args...)
}

func Errorf(format string, args ...interface{}) {
	Default().Errorf(format, args...)
}

func Fatalf(format string, args ...interface{}) {
	Default().Fatalf(format, args...)
}

func Panicf(format string, args ...interface{}) {
	Default().Panicf(format, args...)
}

func With(fields ...Field) Logger {
	return Default().With(fields...)
}

func Named(name string) Logger {
	return Default().Named(name)
}

func WithContext(ctx context.Context) Logger {
	return Default().WithContext(ctx)
}

func Sync() error {
	return Default().Sync()
}

// ============ helper functions ============

func GetCaller(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}

func NewNoOpLogger() Logger {
	return &zapLogger{
		logger: zap.NewNop(),
		sugar:  zap.NewNop().Sugar(),
		level:  DebugLevel,
	}
}

func NewTestLogger(w io.Writer) Logger {
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		MessageKey:     "msg",
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	}

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(w),
		DebugLevel,
	)

	zapLog := zap.New(core)

	return &zapLogger{
		logger: zapLog,
		sugar:  zapLog.Sugar(),
		level:  DebugLevel,
	}
}

// ParseLevel 把配置里的级别名转换成 Level，无法识别时为 info
func ParseLevel(levelStr string) Level {
	switch levelStr {
	case "debug":
		return DebugLevel
	case "info":
		return InfoLevel
	case "warn":
		return WarnLevel
	case "error":
		return ErrorLevel
	}
	return InfoLevel
}

// ParseLevelStrict 与 ParseLevel 相同，但无法识别时返回错误
func ParseLevelStrict(levelStr string) (Level, error) {
	return zapcore.ParseLevel(levelStr)
}
