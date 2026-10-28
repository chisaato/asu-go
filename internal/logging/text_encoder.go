package logging

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

var textBufferPool = buffer.NewPool()

// primitiveArray 用于接收 zapcore.TimeEncoder、LevelEncoder、CallerEncoder 等写入的元信息。
type primitiveArray []string

func (p *primitiveArray) AppendString(v string)         { *p = append(*p, v) }
func (p *primitiveArray) AppendByteString(v []byte)     { *p = append(*p, string(v)) }
func (p *primitiveArray) AppendBool(v bool)             { *p = append(*p, strconv.FormatBool(v)) }
func (p *primitiveArray) AppendInt(v int)               { *p = append(*p, strconv.Itoa(v)) }
func (p *primitiveArray) AppendInt64(v int64)           { *p = append(*p, strconv.FormatInt(v, 10)) }
func (p *primitiveArray) AppendInt32(v int32)           { *p = append(*p, strconv.FormatInt(int64(v), 10)) }
func (p *primitiveArray) AppendInt16(v int16)           { *p = append(*p, strconv.FormatInt(int64(v), 10)) }
func (p *primitiveArray) AppendInt8(v int8)             { *p = append(*p, strconv.FormatInt(int64(v), 10)) }
func (p *primitiveArray) AppendUint(v uint)             { *p = append(*p, strconv.FormatUint(uint64(v), 10)) }
func (p *primitiveArray) AppendUint64(v uint64)         { *p = append(*p, strconv.FormatUint(v, 10)) }
func (p *primitiveArray) AppendUint32(v uint32)         { *p = append(*p, strconv.FormatUint(uint64(v), 10)) }
func (p *primitiveArray) AppendUint16(v uint16)         { *p = append(*p, strconv.FormatUint(uint64(v), 10)) }
func (p *primitiveArray) AppendUint8(v uint8)           { *p = append(*p, strconv.FormatUint(uint64(v), 10)) }
func (p *primitiveArray) AppendUintptr(v uintptr)       { *p = append(*p, strconv.FormatUint(uint64(v), 10)) }
func (p *primitiveArray) AppendFloat64(v float64)       { *p = append(*p, strconv.FormatFloat(v, 'f', -1, 64)) }
func (p *primitiveArray) AppendFloat32(v float32)       { *p = append(*p, strconv.FormatFloat(float64(v), 'f', -1, 32)) }
func (p *primitiveArray) AppendComplex128(v complex128) { *p = append(*p, fmt.Sprintf("%v", v)) }
func (p *primitiveArray) AppendComplex64(v complex64)   { *p = append(*p, fmt.Sprintf("%v", v)) }

type keyVal struct {
	key string
	val string
}

type textEncoder struct {
	zapcore.EncoderConfig
	namespaces []string
	kvs        []keyVal
}

func newTextEncoder(cfg zapcore.EncoderConfig) *textEncoder {
	if cfg.ConsoleSeparator == "" {
		cfg.ConsoleSeparator = "\t"
	}
	return &textEncoder{
		EncoderConfig: cfg,
	}
}

func (e *textEncoder) Clone() zapcore.Encoder {
	clone := &textEncoder{
		EncoderConfig: e.EncoderConfig,
		namespaces:    append([]string(nil), e.namespaces...),
		kvs:           make([]keyVal, len(e.kvs)),
	}
	copy(clone.kvs, e.kvs)
	return clone
}

func (e *textEncoder) fullKey(k string) string {
	if len(e.namespaces) == 0 {
		return k
	}
	return strings.Join(e.namespaces, ".") + "." + k
}

func (e *textEncoder) addKV(k, v string) {
	e.kvs = append(e.kvs, keyVal{key: e.fullKey(k), val: v})
}

// formatValue 将值格式化为清晰易读的 label=value 形式，包含空格/引号/等号时自动转义加引号。
func formatValue(val string) string {
	if val == "" {
		return `""`
	}
	// 如果是紧凑的 JSON 数组或对象，避免被外层双引号二次转义，提升人类可读性
	if (strings.HasPrefix(val, "{") && strings.HasSuffix(val, "}")) ||
		(strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]")) {
		if !strings.ContainsAny(val, " \t\r\n") {
			return val
		}
	}
	needsQuote := false
	for i := 0; i < len(val); i++ {
		c := val[i]
		if c <= ' ' || c == '"' || c == '=' || c == '\\' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return val
	}
	return strconv.Quote(val)
}

func (e *textEncoder) AddString(k, v string) {
	e.addKV(k, formatValue(v))
}

func (e *textEncoder) AddByteString(k string, v []byte) {
	e.addKV(k, formatValue(string(v)))
}

func (e *textEncoder) AddBool(k string, v bool) {
	e.addKV(k, strconv.FormatBool(v))
}

func (e *textEncoder) AddInt(k string, v int) {
	e.addKV(k, strconv.Itoa(v))
}

func (e *textEncoder) AddInt64(k string, v int64) {
	e.addKV(k, strconv.FormatInt(v, 10))
}

func (e *textEncoder) AddInt32(k string, v int32) {
	e.addKV(k, strconv.FormatInt(int64(v), 10))
}

func (e *textEncoder) AddInt16(k string, v int16) {
	e.addKV(k, strconv.FormatInt(int64(v), 10))
}

func (e *textEncoder) AddInt8(k string, v int8) {
	e.addKV(k, strconv.FormatInt(int64(v), 10))
}

func (e *textEncoder) AddUint(k string, v uint) {
	e.addKV(k, strconv.FormatUint(uint64(v), 10))
}

func (e *textEncoder) AddUint64(k string, v uint64) {
	e.addKV(k, strconv.FormatUint(v, 10))
}

func (e *textEncoder) AddUint32(k string, v uint32) {
	e.addKV(k, strconv.FormatUint(uint64(v), 10))
}

func (e *textEncoder) AddUint16(k string, v uint16) {
	e.addKV(k, strconv.FormatUint(uint64(v), 10))
}

func (e *textEncoder) AddUint8(k string, v uint8) {
	e.addKV(k, strconv.FormatUint(uint64(v), 10))
}

func (e *textEncoder) AddUintptr(k string, v uintptr) {
	e.addKV(k, strconv.FormatUint(uint64(v), 10))
}

func (e *textEncoder) AddFloat64(k string, v float64) {
	if math.IsNaN(v) {
		e.addKV(k, "NaN")
	} else if math.IsInf(v, 1) {
		e.addKV(k, "+Inf")
	} else if math.IsInf(v, -1) {
		e.addKV(k, "-Inf")
	} else {
		e.addKV(k, strconv.FormatFloat(v, 'f', -1, 64))
	}
}

func (e *textEncoder) AddFloat32(k string, v float32) {
	f := float64(v)
	if math.IsNaN(f) {
		e.addKV(k, "NaN")
	} else if math.IsInf(f, 1) {
		e.addKV(k, "+Inf")
	} else if math.IsInf(f, -1) {
		e.addKV(k, "-Inf")
	} else {
		e.addKV(k, strconv.FormatFloat(f, 'f', -1, 32))
	}
}

func (e *textEncoder) AddComplex128(k string, v complex128) {
	e.addKV(k, formatValue(fmt.Sprintf("%v", v)))
}

func (e *textEncoder) AddComplex64(k string, v complex64) {
	e.addKV(k, formatValue(fmt.Sprintf("%v", v)))
}

func (e *textEncoder) AddDuration(k string, v time.Duration) {
	if e.EncodeDuration != nil {
		var arr primitiveArray
		e.EncodeDuration(v, &arr)
		if len(arr) > 0 {
			e.addKV(k, formatValue(arr[0]))
			return
		}
	}
	e.addKV(k, formatValue(v.String()))
}

func (e *textEncoder) AddTime(k string, v time.Time) {
	if e.EncodeTime != nil {
		var arr primitiveArray
		e.EncodeTime(v, &arr)
		if len(arr) > 0 {
			e.addKV(k, formatValue(arr[0]))
			return
		}
	}
	e.addKV(k, formatValue(v.Format(time.RFC3339)))
}

func (e *textEncoder) AddBinary(k string, v []byte) {
	e.addKV(k, formatValue(base64.StdEncoding.EncodeToString(v)))
}

func (e *textEncoder) AddReflected(k string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		e.addKV(k, formatValue(fmt.Sprintf("%v", v)))
		return err
	}
	e.addKV(k, formatValue(string(data)))
	return nil
}

func (e *textEncoder) AddObject(k string, marshaler zapcore.ObjectMarshaler) error {
	m := zapcore.NewMapObjectEncoder()
	if err := marshaler.MarshalLogObject(m); err != nil {
		return err
	}
	data, err := json.Marshal(m.Fields)
	if err != nil {
		return err
	}
	e.addKV(k, formatValue(string(data)))
	return nil
}

func (e *textEncoder) AddArray(k string, marshaler zapcore.ArrayMarshaler) error {
	m := zapcore.NewMapObjectEncoder()
	if err := m.AddArray(k, marshaler); err != nil {
		return err
	}
	data, err := json.Marshal(m.Fields[k])
	if err != nil {
		return err
	}
	e.addKV(k, formatValue(string(data)))
	return nil
}

func (e *textEncoder) OpenNamespace(k string) {
	e.namespaces = append(e.namespaces, k)
}

func (e *textEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	line := textBufferPool.Get()

	// 1. 元信息：Time、Level、LoggerName、Caller
	var meta primitiveArray
	if e.TimeKey != "" && e.EncodeTime != nil && !ent.Time.IsZero() {
		e.EncodeTime(ent.Time, &meta)
	}
	if e.LevelKey != "" && e.EncodeLevel != nil {
		e.EncodeLevel(ent.Level, &meta)
	}
	if ent.LoggerName != "" && e.NameKey != "" {
		nameEncoder := e.EncodeName
		if nameEncoder == nil {
			nameEncoder = zapcore.FullNameEncoder
		}
		nameEncoder(ent.LoggerName, &meta)
	}
	if ent.Caller.Defined {
		if e.CallerKey != "" && e.EncodeCaller != nil {
			e.EncodeCaller(ent.Caller, &meta)
		}
		if e.FunctionKey != "" {
			meta.AppendString(ent.Caller.Function)
		}
	}

	for i := range meta {
		if i > 0 {
			line.AppendString(e.ConsoleSeparator)
		}
		line.AppendString(meta[i])
	}

	// 2. 日志消息
	if e.MessageKey != "" && ent.Message != "" {
		if line.Len() > 0 {
			line.AppendString(e.ConsoleSeparator)
		}
		line.AppendString(ent.Message)
	}

	// 3. 收集所有字段（上下文字段 + 本次调用的 fields）
	var finalKVs []keyVal
	if len(e.kvs) > 0 {
		finalKVs = make([]keyVal, len(e.kvs), len(e.kvs)+len(fields))
		copy(finalKVs, e.kvs)
	}

	if len(fields) > 0 {
		temp := &textEncoder{
			EncoderConfig: e.EncoderConfig,
			namespaces:    append([]string(nil), e.namespaces...),
		}
		for i := range fields {
			fields[i].AddTo(temp)
		}
		finalKVs = append(finalKVs, temp.kvs...)
	}

	// 4. 追加 label=value 字段
	if len(finalKVs) > 0 {
		if line.Len() > 0 {
			line.AppendString(e.ConsoleSeparator)
		}
		for i, kv := range finalKVs {
			if i > 0 {
				line.AppendByte(' ')
			}
			line.AppendString(kv.key)
			line.AppendByte('=')
			line.AppendString(kv.val)
		}
	}

	// 5. 堆栈信息
	if ent.Stack != "" && e.StacktraceKey != "" {
		line.AppendByte('\n')
		line.AppendString(ent.Stack)
	}

	if e.LineEnding != "" {
		line.AppendString(e.LineEnding)
	} else {
		line.AppendString(zapcore.DefaultLineEnding)
	}

	return line, nil
}
