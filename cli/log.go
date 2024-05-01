package cli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"slices"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

var (
	logTimeStyle = lipgloss.NewStyle().Foreground(grayColor)
	logAttrStyle = lipgloss.NewStyle().Foreground(grayColor)

	logDebugStyle = lipgloss.NewStyle().Foreground(whiteColor)
	logInfoStyle  = lipgloss.NewStyle().Foreground(whiteColor)
	logWarnStyle  = lipgloss.NewStyle().Foreground(yellowColor)
	logErrorStyle = lipgloss.NewStyle().Foreground(redColor)
)

type slogHandler struct {
	mu     sync.Mutex
	stream io.Writer
	level  slog.Level

	parent *slogHandler
	attrs  []slog.Attr
}

func (h *slogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *slogHandler) Handle(ctx context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var b bytes.Buffer
	b.WriteString(logTimeStyle.Render(record.Time.Format("2006-01-02 15:04:05.000")))
	b.WriteByte(' ')
	b.WriteString(levelString(record.Level))
	b.WriteByte(' ')
	b.WriteString(record.Message)
	record.Attrs(func(attr slog.Attr) bool {
		b.WriteByte(' ')
		b.WriteString(logAttrStyle.Render(attr.String()))
		return true
	})
	for _, attr := range h.attrs {
		b.WriteByte(' ')
		b.WriteString(logAttrStyle.Render(attr.String()))
	}
	b.WriteByte('\n')

	_, err := h.stream.Write(b.Bytes())
	return err
}

func levelString(level slog.Level) string {
	switch level {
	case slog.LevelDebug:
		return logDebugStyle.Render(level.String())
	case slog.LevelInfo:
		return logInfoStyle.Render(level.String())
	case slog.LevelWarn:
		return logWarnStyle.Render(level.String())
	case slog.LevelError:
		return logErrorStyle.Render(level.String())
	default:
		return level.String()
	}
}

func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	parent := h
	if parent.parent != nil {
		parent = parent.parent
	}
	return &slogHandler{
		stream: h.stream,
		level:  h.level,
		parent: parent,
		attrs:  append(slices.Clip(parent.attrs), attrs...),
	}
}

func (h *slogHandler) WithGroup(group string) slog.Handler {
	panic("not implemented")
}

type prefixLogWriter struct {
	stream io.Writer
	prefix []byte
}

func (p *prefixLogWriter) Write(b []byte) (int, error) {
	var buffer bytes.Buffer
	if _, err := buffer.Write(p.prefix); err != nil {
		return 0, err
	}
	if _, err := buffer.Write(b); err != nil {
		return 0, err
	}
	return p.stream.Write(buffer.Bytes())
}
