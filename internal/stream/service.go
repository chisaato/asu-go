package stream

import (
	"context"
	"io"
	"os"
	"sync"
)

// JobSource 由队列/数据库适配。不存在任务时仍允许读取已归档的磁盘日志。
type JobSource interface {
	State(context.Context, string) (exists, finished bool, err error)
}
type MemoryJobSource struct {
	mu   sync.RWMutex
	jobs map[string]bool
}

func NewMemoryJobSource() *MemoryJobSource { return &MemoryJobSource{jobs: make(map[string]bool)} }
func (s *MemoryJobSource) Set(id string, finished bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[id] = finished
}
func (s *MemoryJobSource) State(ctx context.Context, id string) (bool, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	finished, exists := s.jobs[id]
	return exists, finished, ctx.Err()
}

type Service struct {
	Hub  *Hub
	Jobs JobSource
}
type Log struct {
	snapshot []string
	lines    <-chan string
	history  io.ReadCloser
	cancel   func()
}

func (s Service) Open(ctx context.Context, id string, follow bool) (*Log, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	exists, finished, err := s.Jobs.State(ctx, id)
	if err != nil {
		return nil, err
	}
	if follow && exists && !finished {
		snapshot, lines, cancel, ok := s.Hub.Subscribe(id)
		if ok {
			return &Log{snapshot: snapshot, lines: lines, cancel: cancel}, nil
		}
	}
	history, err := s.Hub.History(id)
	if err != nil {
		if os.IsNotExist(err) && exists && finished {
			return &Log{}, nil
		}
		return nil, err
	}
	return &Log{history: history}, nil
}
func (l *Log) Close() error {
	if l.cancel != nil {
		l.cancel()
	}
	if l.history != nil {
		return l.history.Close()
	}
	return nil
}
func (l *Log) Copy(ctx context.Context, w io.Writer) error {
	if l.history != nil {
		buf := make([]byte, 32<<10)
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			n, err := l.history.Read(buf)
			if n > 0 {
				if _, e := w.Write(buf[:n]); e != nil {
					return e
				}
			}
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
	for _, line := range l.snapshot {
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
	}
	if l.lines == nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-l.lines:
			if !ok {
				return nil
			}
			if _, err := io.WriteString(w, line); err != nil {
				return err
			}
		}
	}
}
