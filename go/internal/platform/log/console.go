package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
	ansiGray   = "\x1b[90m"
)

var (
	consoleBufPool = buffer.NewPool()
	consoleOmit    = map[string]struct{}{
		"process":     {},
		"method":      {},
		"path":        {},
		"status":      {},
		"duration_ms": {},
	}
)

type prettyEncoder struct {
	fields []zapcore.Field
	ns     string
	color  bool
}

func newPrettyEncoder(color bool) zapcore.Encoder {
	return &prettyEncoder{color: color}
}

func (e *prettyEncoder) Clone() zapcore.Encoder {
	clone := *e
	if len(e.fields) > 0 {
		clone.fields = append([]zapcore.Field{}, e.fields...)
	}
	return &clone
}

func (e *prettyEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	buf := consoleBufPool.Get()
	all := make([]zapcore.Field, 0, len(e.fields)+len(fields))
	all = append(all, e.fields...)
	all = append(all, fields...)

	process := "-"
	for _, f := range all {
		if f.Key == "process" && f.Type == zapcore.StringType && f.String != "" {
			process = f.String
			break
		}
	}

	if e.color {
		buf.AppendString(ansiGray)
	}
	buf.AppendString(ent.Time.Format("15:04:05"))
	if e.color {
		buf.AppendString(ansiReset)
	}
	buf.AppendString("  ")
	appendColoredLevel(buf, ent.Level, e.color)
	buf.AppendString("  ")
	if e.color {
		buf.AppendString(ansiCyan)
	}
	buf.AppendString(padRight(process, 11))
	if e.color {
		buf.AppendString(ansiReset)
	}
	buf.AppendString("  ")
	if ent.Message != "" {
		if e.color && ent.Level >= zapcore.ErrorLevel {
			buf.AppendString(ansiBold)
		}
		buf.AppendString(ent.Message)
		if e.color && ent.Level >= zapcore.ErrorLevel {
			buf.AppendString(ansiReset)
		}
	}

	sink := &kvSink{buf: buf, color: e.color}
	for _, f := range all {
		if f.Key == "process" {
			continue
		}
		if _, skip := consoleOmit[f.Key]; skip {
			continue
		}
		f.AddTo(sink)
	}
	buf.AppendByte('\n')
	return buf, nil
}

func (e *prettyEncoder) fullKey(key string) string {
	if e.ns == "" {
		return key
	}
	return e.ns + "." + key
}

func (e *prettyEncoder) AddArray(key string, marshaler zapcore.ArrayMarshaler) error {
	e.fields = append(e.fields, zap.Array(e.fullKey(key), marshaler))
	return nil
}

func (e *prettyEncoder) AddObject(key string, marshaler zapcore.ObjectMarshaler) error {
	e.fields = append(e.fields, zap.Object(e.fullKey(key), marshaler))
	return nil
}

func (e *prettyEncoder) AddBinary(key string, value []byte) {
	e.fields = append(e.fields, zap.Binary(e.fullKey(key), value))
}

func (e *prettyEncoder) AddByteString(key string, value []byte) {
	e.fields = append(e.fields, zap.ByteString(e.fullKey(key), value))
}

func (e *prettyEncoder) AddBool(key string, value bool) {
	e.fields = append(e.fields, zap.Bool(e.fullKey(key), value))
}

func (e *prettyEncoder) AddComplex128(key string, value complex128) {
	e.fields = append(e.fields, zap.Complex128(e.fullKey(key), value))
}

func (e *prettyEncoder) AddComplex64(key string, value complex64) {
	e.fields = append(e.fields, zap.Complex64(e.fullKey(key), value))
}

func (e *prettyEncoder) AddDuration(key string, value time.Duration) {
	e.fields = append(e.fields, zap.Duration(e.fullKey(key), value))
}

func (e *prettyEncoder) AddFloat64(key string, value float64) {
	e.fields = append(e.fields, zap.Float64(e.fullKey(key), value))
}

func (e *prettyEncoder) AddFloat32(key string, value float32) {
	e.fields = append(e.fields, zap.Float32(e.fullKey(key), value))
}

func (e *prettyEncoder) AddInt(key string, value int) {
	e.fields = append(e.fields, zap.Int(e.fullKey(key), value))
}

func (e *prettyEncoder) AddInt64(key string, value int64) {
	e.fields = append(e.fields, zap.Int64(e.fullKey(key), value))
}

func (e *prettyEncoder) AddInt32(key string, value int32) {
	e.fields = append(e.fields, zap.Int32(e.fullKey(key), value))
}

func (e *prettyEncoder) AddInt16(key string, value int16) {
	e.fields = append(e.fields, zap.Int16(e.fullKey(key), value))
}

func (e *prettyEncoder) AddInt8(key string, value int8) {
	e.fields = append(e.fields, zap.Int8(e.fullKey(key), value))
}

func (e *prettyEncoder) AddString(key, value string) {
	e.fields = append(e.fields, zap.String(e.fullKey(key), value))
}

func (e *prettyEncoder) AddTime(key string, value time.Time) {
	e.fields = append(e.fields, zap.Time(e.fullKey(key), value))
}

func (e *prettyEncoder) AddUint(key string, value uint) {
	e.fields = append(e.fields, zap.Uint(e.fullKey(key), value))
}

func (e *prettyEncoder) AddUint64(key string, value uint64) {
	e.fields = append(e.fields, zap.Uint64(e.fullKey(key), value))
}

func (e *prettyEncoder) AddUint32(key string, value uint32) {
	e.fields = append(e.fields, zap.Uint32(e.fullKey(key), value))
}

func (e *prettyEncoder) AddUint16(key string, value uint16) {
	e.fields = append(e.fields, zap.Uint16(e.fullKey(key), value))
}

func (e *prettyEncoder) AddUint8(key string, value uint8) {
	e.fields = append(e.fields, zap.Uint8(e.fullKey(key), value))
}

func (e *prettyEncoder) AddUintptr(key string, value uintptr) {
	e.fields = append(e.fields, zap.Uintptr(e.fullKey(key), value))
}

func (e *prettyEncoder) AddReflected(key string, value interface{}) error {
	e.fields = append(e.fields, zap.Reflect(e.fullKey(key), value))
	return nil
}

func (e *prettyEncoder) OpenNamespace(key string) {
	if e.ns == "" {
		e.ns = key
		return
	}
	e.ns = e.ns + "." + key
}

type kvSink struct {
	buf   *buffer.Buffer
	color bool
	ns    string
}

func (s *kvSink) key(key string) string {
	if s.ns == "" {
		return key
	}
	return s.ns + "." + key
}

func (s *kvSink) pair(key, value string, errVal bool) {
	s.buf.AppendByte(' ')
	if s.color {
		s.buf.AppendString(ansiDim)
	}
	s.buf.AppendString(s.key(key))
	s.buf.AppendByte('=')
	if s.color {
		s.buf.AppendString(ansiReset)
		if errVal {
			s.buf.AppendString(ansiRed)
		}
	}
	if needsQuote(value) {
		s.buf.AppendString(strconv.Quote(value))
	} else {
		s.buf.AppendString(value)
	}
	if s.color {
		s.buf.AppendString(ansiReset)
	}
}

func (s *kvSink) AddArray(key string, marshaler zapcore.ArrayMarshaler) error {
	arr := &arraySink{}
	if err := marshaler.MarshalLogArray(arr); err != nil {
		s.pair(key, fmt.Sprint(marshaler), false)
		return nil
	}
	s.pair(key, "["+strings.Join(arr.parts, ",")+"]", false)
	return nil
}

func (s *kvSink) AddObject(key string, marshaler zapcore.ObjectMarshaler) error {
	child := &kvSink{buf: s.buf, color: s.color, ns: s.key(key)}
	return marshaler.MarshalLogObject(child)
}

func (s *kvSink) AddBinary(key string, value []byte) {
	s.pair(key, fmt.Sprintf("<%d bytes>", len(value)), false)
}

func (s *kvSink) AddByteString(key string, value []byte) {
	s.pair(key, string(value), false)
}

func (s *kvSink) AddBool(key string, value bool) {
	s.pair(key, strconv.FormatBool(value), false)
}

func (s *kvSink) AddComplex128(key string, value complex128) {
	s.pair(key, fmt.Sprint(value), false)
}

func (s *kvSink) AddComplex64(key string, value complex64) {
	s.pair(key, fmt.Sprint(value), false)
}

func (s *kvSink) AddDuration(key string, value time.Duration) {
	s.pair(key, value.String(), false)
}

func (s *kvSink) AddFloat64(key string, value float64) {
	s.pair(key, strconv.FormatFloat(value, 'f', -1, 64), false)
}

func (s *kvSink) AddFloat32(key string, value float32) {
	s.pair(key, strconv.FormatFloat(float64(value), 'f', -1, 32), false)
}

func (s *kvSink) AddInt(key string, value int) {
	s.pair(key, strconv.Itoa(value), false)
}

func (s *kvSink) AddInt64(key string, value int64) {
	s.pair(key, strconv.FormatInt(value, 10), false)
}

func (s *kvSink) AddInt32(key string, value int32) {
	s.pair(key, strconv.FormatInt(int64(value), 10), false)
}

func (s *kvSink) AddInt16(key string, value int16) {
	s.pair(key, strconv.FormatInt(int64(value), 10), false)
}

func (s *kvSink) AddInt8(key string, value int8) {
	s.pair(key, strconv.FormatInt(int64(value), 10), false)
}

func (s *kvSink) AddString(key, value string) {
	s.pair(key, value, key == "error" || strings.HasSuffix(key, ".error"))
}

func (s *kvSink) AddTime(key string, value time.Time) {
	s.pair(key, value.Format(time.RFC3339), false)
}

func (s *kvSink) AddUint(key string, value uint) {
	s.pair(key, strconv.FormatUint(uint64(value), 10), false)
}

func (s *kvSink) AddUint64(key string, value uint64) {
	s.pair(key, strconv.FormatUint(value, 10), false)
}

func (s *kvSink) AddUint32(key string, value uint32) {
	s.pair(key, strconv.FormatUint(uint64(value), 10), false)
}

func (s *kvSink) AddUint16(key string, value uint16) {
	s.pair(key, strconv.FormatUint(uint64(value), 10), false)
}

func (s *kvSink) AddUint8(key string, value uint8) {
	s.pair(key, strconv.FormatUint(uint64(value), 10), false)
}

func (s *kvSink) AddUintptr(key string, value uintptr) {
	s.pair(key, strconv.FormatUint(uint64(value), 10), false)
}

func (s *kvSink) AddReflected(key string, value interface{}) error {
	if err, ok := value.(error); ok && err != nil {
		s.pair(key, err.Error(), true)
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		s.pair(key, fmt.Sprint(value), false)
		return nil
	}
	s.pair(key, string(raw), false)
	return nil
}

func (s *kvSink) OpenNamespace(key string) {
	s.ns = s.key(key)
}

type arraySink struct {
	parts []string
}

func (a *arraySink) append(v string) { a.parts = append(a.parts, v) }

func (a *arraySink) AppendBool(v bool)                   { a.append(strconv.FormatBool(v)) }
func (a *arraySink) AppendByteString(v []byte)           { a.append(string(v)) }
func (a *arraySink) AppendComplex128(v complex128)       { a.append(fmt.Sprint(v)) }
func (a *arraySink) AppendComplex64(v complex64)         { a.append(fmt.Sprint(v)) }
func (a *arraySink) AppendFloat64(v float64)             { a.append(strconv.FormatFloat(v, 'f', -1, 64)) }
func (a *arraySink) AppendFloat32(v float32)             { a.append(strconv.FormatFloat(float64(v), 'f', -1, 32)) }
func (a *arraySink) AppendInt(v int)                     { a.append(strconv.Itoa(v)) }
func (a *arraySink) AppendInt64(v int64)                 { a.append(strconv.FormatInt(v, 10)) }
func (a *arraySink) AppendInt32(v int32)                 { a.append(strconv.FormatInt(int64(v), 10)) }
func (a *arraySink) AppendInt16(v int16)                 { a.append(strconv.FormatInt(int64(v), 10)) }
func (a *arraySink) AppendInt8(v int8)                   { a.append(strconv.FormatInt(int64(v), 10)) }
func (a *arraySink) AppendString(v string)               { a.append(v) }
func (a *arraySink) AppendUint(v uint)                   { a.append(strconv.FormatUint(uint64(v), 10)) }
func (a *arraySink) AppendUint64(v uint64)               { a.append(strconv.FormatUint(v, 10)) }
func (a *arraySink) AppendUint32(v uint32)               { a.append(strconv.FormatUint(uint64(v), 10)) }
func (a *arraySink) AppendUint16(v uint16)               { a.append(strconv.FormatUint(uint64(v), 10)) }
func (a *arraySink) AppendUint8(v uint8)                 { a.append(strconv.FormatUint(uint64(v), 10)) }
func (a *arraySink) AppendUintptr(v uintptr)             { a.append(strconv.FormatUint(uint64(v), 10)) }
func (a *arraySink) AppendDuration(v time.Duration)      { a.append(v.String()) }
func (a *arraySink) AppendTime(v time.Time)              { a.append(v.Format(time.RFC3339)) }
func (a *arraySink) AppendReflected(v interface{}) error { a.append(fmt.Sprint(v)); return nil }
func (a *arraySink) AppendArray(zapcore.ArrayMarshaler) error {
	a.append("[...]")
	return nil
}
func (a *arraySink) AppendObject(zapcore.ObjectMarshaler) error {
	a.append("{...}")
	return nil
}

func appendColoredLevel(buf *buffer.Buffer, level zapcore.Level, color bool) {
	if color {
		switch {
		case level >= zapcore.ErrorLevel:
			buf.AppendString(ansiRed)
			buf.AppendString(ansiBold)
		case level == zapcore.WarnLevel:
			buf.AppendString(ansiYellow)
		case level == zapcore.DebugLevel:
			buf.AppendString(ansiGray)
		default:
			buf.AppendString(ansiGreen)
		}
	}
	buf.AppendString(padRight(level.CapitalString(), 5))
	if color {
		buf.AppendString(ansiReset)
	}
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func needsQuote(value string) bool {
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || r == '=' || r == '"' || r == '{' || r == '[' {
			return true
		}
	}
	return false
}

func consoleColorEnabled(w io.Writer, disable bool) bool {
	if disable || strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	if strings.TrimSpace(os.Getenv("FORCE_COLOR")) != "" || strings.TrimSpace(os.Getenv("CLICOLOR_FORCE")) != "" {
		return true
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	// TTY 和管道（just / Compose）上色；重定向到普通文件则不上色。
	return !info.Mode().IsRegular()
}
