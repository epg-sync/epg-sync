package logger

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"go.uber.org/zap/zapcore"
)

// Record 是一条结构化日志，交给 Hub 的消费者（例如写入数据库供管理后台查看）
type Record struct {
	Time    time.Time
	Level   Level
	Logger  string
	Caller  string
	Message string
	Fields  map[string]any
	Stack   string
}

// Hub 把日志条目转交给异步消费者。写入方从不阻塞：
// 缓冲区满了（消费者还没启动或跟不上）就丢弃并计数，日志照常输出到 stdout / 文件。
type Hub struct {
	ch      chan Record
	dropped atomic.Int64
}

func NewHub(size int) *Hub {
	return &Hub{ch: make(chan Record, size)}
}

// Records 返回日志条目的通道，只应有一个消费者
func (h *Hub) Records() <-chan Record {
	return h.ch
}

// Dropped 返回因缓冲区满而丢弃的条目数
func (h *Hub) Dropped() int64 {
	return h.dropped.Load()
}

// Core 返回一个把日志条目送进 Hub 的 zapcore.Core，与输出到 stdout 的 core 并列使用
func (h *Hub) Core(level zapcore.LevelEnabler) zapcore.Core {
	return &hubCore{LevelEnabler: level, hub: h}
}

func (h *Hub) publish(r Record) {
	select {
	case h.ch <- r:
	default:
		h.dropped.Add(1)
	}
}

type hubCore struct {
	zapcore.LevelEnabler
	hub    *Hub
	fields []zapcore.Field
}

func (c *hubCore) With(fields []zapcore.Field) zapcore.Core {
	clone := *c
	clone.fields = make([]zapcore.Field, 0, len(c.fields)+len(fields))
	clone.fields = append(clone.fields, c.fields...)
	clone.fields = append(clone.fields, fields...)
	return &clone
}

func (c *hubCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *hubCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	r := Record{
		Time:    ent.Time,
		Level:   ent.Level,
		Logger:  ent.LoggerName,
		Message: ent.Message,
		Stack:   ent.Stack,
	}
	if ent.Caller.Defined {
		r.Caller = ent.Caller.TrimmedPath()
	}

	if n := len(c.fields) + len(fields); n > 0 {
		enc := zapcore.NewMapObjectEncoder()
		for _, f := range c.fields {
			f.AddTo(enc)
		}
		for _, f := range fields {
			f.AddTo(enc)
		}
		r.Fields = jsonSafe(enc.Fields)
	}

	c.hub.publish(r)
	return nil
}

func (c *hubCore) Sync() error { return nil }

// jsonSafe 保证字段能被 json 编码（zap.Any / zap.Reflect 可能带进任意类型），
// 无法编码的值退化成 fmt 的字符串形式
func jsonSafe(fields map[string]any) map[string]any {
	for k, v := range fields {
		switch val := v.(type) {
		case time.Duration:
			fields[k] = val.String()
			continue
		case error:
			fields[k] = val.Error()
			continue
		}
		if _, err := json.Marshal(v); err != nil {
			fields[k] = fmt.Sprintf("%+v", v)
		}
	}
	return fields
}
