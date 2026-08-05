// Package applog 统一进程日志：固定输出日志等级，便于 docker/grep 过滤。
package applog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Options 日志初始化选项
type Options struct {
	// Level: debug | info | warn | error，默认 info
	Level string
	// Format: text | json，默认 text
	Format string
	// Service 可选服务名，写入每条日志（api/scheduler/pusher）
	Service string
}

// Init 设置全局 slog，使每条日志都带明确等级字段。
// text 示例：2026-08-05 18:52:26 [INFO] mq ready driver=redis_stream
// json 示例：{"time":"...","level":"INFO","msg":"..."}
func Init(opts Options) {
	level := parseLevel(opts.Level)
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "text"
	}

	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
			ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
				if a.Key == slog.LevelKey {
					a.Value = slog.StringValue(strings.ToUpper(a.Value.String()))
				}
				return a
			},
		})
	default:
		handler = newBracketHandler(os.Stdout, level)
	}

	if opts.Service != "" {
		handler = handler.WithAttrs([]slog.Attr{slog.String("service", opts.Service)})
	}
	slog.SetDefault(slog.New(handler))
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// bracketHandler：time [LEVEL] msg key=value ...
type bracketHandler struct {
	w     io.Writer
	level slog.Level
	attrs []slog.Attr
	group string
}

func newBracketHandler(w io.Writer, level slog.Level) *bracketHandler {
	return &bracketHandler{w: w, level: level}
}

func (h *bracketHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *bracketHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.Grow(256)
	b.WriteString(r.Time.Format("2006-01-02 15:04:05"))
	b.WriteString(" [")
	b.WriteString(strings.ToUpper(r.Level.String()))
	b.WriteString("] ")
	b.WriteString(r.Message)

	writeAttr := func(a slog.Attr) {
		if a.Equal(slog.Attr{}) {
			return
		}
		a.Value = a.Value.Resolve()
		if a.Value.Kind() == slog.KindGroup {
			for _, ga := range a.Value.Group() {
				b.WriteByte(' ')
				if a.Key != "" {
					b.WriteString(a.Key)
					b.WriteByte('.')
				}
				b.WriteString(ga.Key)
				b.WriteByte('=')
				b.WriteString(formatValue(ga.Value))
			}
			return
		}
		b.WriteByte(' ')
		if h.group != "" {
			b.WriteString(h.group)
			b.WriteByte('.')
		}
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(formatValue(a.Value))
	}

	for _, a := range h.attrs {
		writeAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(a)
		return true
	})
	b.WriteByte('\n')
	_, err := io.WriteString(h.w, b.String())
	return err
}

func formatValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		if strings.IndexFunc(s, func(r rune) bool { return r == ' ' || r == '=' || r == '"' }) >= 0 {
			return fmt.Sprintf("%q", s)
		}
		return s
	case slog.KindAny:
		return fmt.Sprintf("%v", v.Any())
	default:
		return v.String()
	}
}

func (h *bracketHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cp := *h
	cp.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &cp
}

func (h *bracketHandler) WithGroup(name string) slog.Handler {
	cp := *h
	if h.group != "" {
		cp.group = h.group + "." + name
	} else {
		cp.group = name
	}
	return &cp
}

// NewGormLogger 将 GORM 日志转到 slog，并带上同一套等级前缀。
func NewGormLogger(level string) logger.Interface {
	return &gormSlog{
		slow:           200 * time.Millisecond,
		level:          gormLevel(level),
		ignoreNotFound: true,
	}
}

func gormLevel(s string) logger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return logger.Info // GORM Info 含 SQL
	case "warn", "warning":
		return logger.Warn
	case "error":
		return logger.Error
	case "silent":
		return logger.Silent
	default:
		return logger.Warn
	}
}

type gormSlog struct {
	slow           time.Duration
	level          logger.LogLevel
	ignoreNotFound bool
}

func (l *gormSlog) LogMode(level logger.LogLevel) logger.Interface {
	cp := *l
	cp.level = level
	return &cp
}

func (l *gormSlog) Info(_ context.Context, msg string, data ...interface{}) {
	if l.level < logger.Info {
		return
	}
	slog.Info(fmt.Sprintf(msg, data...), "component", "gorm")
}

func (l *gormSlog) Warn(_ context.Context, msg string, data ...interface{}) {
	if l.level < logger.Warn {
		return
	}
	slog.Warn(fmt.Sprintf(msg, data...), "component", "gorm")
}

func (l *gormSlog) Error(_ context.Context, msg string, data ...interface{}) {
	if l.level < logger.Error {
		return
	}
	slog.Error(fmt.Sprintf(msg, data...), "component", "gorm")
}

func (l *gormSlog) Trace(_ context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.level <= logger.Silent {
		return
	}
	elapsed := time.Since(begin)
	sql, rows := fc()

	switch {
	case err != nil && (!l.ignoreNotFound || !errors.Is(err, gorm.ErrRecordNotFound)):
		if l.level >= logger.Error {
			slog.Error("sql",
				"component", "gorm",
				"elapsed", elapsed.String(),
				"rows", rows,
				"err", err.Error(),
				"sql", sql,
			)
		}
	case elapsed > l.slow && l.slow > 0:
		if l.level >= logger.Warn {
			slog.Warn("slow sql",
				"component", "gorm",
				"elapsed", elapsed.String(),
				"rows", rows,
				"sql", sql,
			)
		}
	case l.level >= logger.Info:
		slog.Info("sql",
			"component", "gorm",
			"elapsed", elapsed.String(),
			"rows", rows,
			"sql", sql,
		)
	}
}
