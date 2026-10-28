package runtime

import (
	"io"
	"sync"
	"time"

	logging "github.com/chisaato/asu-go/internal/logcontext"
	"go.uber.org/zap"
)

func (e *engine) operationResult(name string, start time.Time, err error, fields ...zap.Field) {
	log := logging.OrNop(e.opts.Logger).With(zap.String("component", "runtime"), zap.String("operation", name))
	fields = append(fields, zap.Duration("duration", time.Since(start)))
	log.Debug("运行时调用完成", append(fields, zap.Error(err))...)
	if err != nil {
		log.Error("运行时操作失败", append(fields, zap.Error(err))...)
	} else {
		log.Info("运行时操作完成", fields...)
	}
}

type countReader struct {
	io.Reader
	n int64
}

func (r *countReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.n += int64(n)
	return n, err
}

type archiveReader struct {
	countReader
	source io.Closer
	done   func(int64, error)
	err    error
	once   sync.Once
}

func (r *archiveReader) Read(p []byte) (int, error) {
	n, err := r.countReader.Read(p)
	if err != nil && err != io.EOF {
		r.err = err
	}
	return n, err
}

func (r *archiveReader) Close() (err error) {
	r.once.Do(func() {
		err = r.source.Close()
		if r.err != nil {
			err = r.err
		}
		r.done(r.n, err)
	})
	return err
}
