// Package stream 提供按任务隔离的持久日志与非阻塞广播。
package stream

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sync"
)

const DefaultCapacity = 5000

var validID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var ErrInvalidID = errors.New("无效任务 ID")

// RingBuffer 自身不加锁；Hub 在同一把锁下更新快照与订阅。
type RingBuffer struct {
	lines       []string
	next, count int
}

func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &RingBuffer{lines: make([]string, capacity)}
}
func (r *RingBuffer) Add(line string) {
	r.lines[r.next] = line
	r.next = (r.next + 1) % len(r.lines)
	if r.count < len(r.lines) {
		r.count++
	}
}
func (r *RingBuffer) Snapshot() []string {
	out := make([]string, r.count)
	for i := range out {
		out[i] = r.lines[(r.next-r.count+i+len(r.lines))%len(r.lines)]
	}
	return out
}

type entry struct {
	ring *RingBuffer
	file *os.File
	done bool
	subs map[chan string]struct{}
}
type Hub struct {
	mu       sync.Mutex
	root     *os.Root
	capacity int
	jobs     map[string]*entry
}

func NewHub(dataDir string, capacity int) (*Hub, error) {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	data, err := os.OpenRoot(dataDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = data.Close() }()
	if err = data.MkdirAll("logs", 0755); err != nil {
		return nil, err
	}
	root, err := data.OpenRoot("logs")
	if err != nil {
		return nil, err
	}
	return &Hub{root: root, capacity: capacity, jobs: make(map[string]*entry)}, nil
}
func ValidateID(id string) error {
	if !validID.MatchString(id) {
		return ErrInvalidID
	}
	return nil
}

// Begin 为一次新执行覆写日志；接管也可以复用它重新播放权威日志。
func (h *Hub) Begin(id string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.jobs[id]; old != nil && !old.done {
		return fmt.Errorf("任务已经运行: %s", id)
	}
	name := id + ".log"
	if fi, err := h.root.Lstat(name); err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("日志不是普通文件")
	}
	f, err := h.root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	h.jobs[id] = &entry{ring: NewRingBuffer(h.capacity), file: f, subs: make(map[chan string]struct{})}
	return nil
}

// Append 接收完整日志行（包含换行）。慢订阅者丢弃最旧待发送行，不阻塞写者。
func (h *Hub) Append(id, line string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.jobs[id]
	if e == nil || e.done {
		return fmt.Errorf("任务日志未打开")
	}
	if _, err := io.WriteString(e.file, line); err != nil {
		return err
	}
	e.ring.Add(line)
	for ch := range e.subs {
		select {
		case ch <- line:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- line:
			default:
			}
		}
	}
	return nil
}
func (h *Hub) Finish(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.jobs[id]
	if e == nil || e.done {
		return nil
	}
	e.done = true
	for ch := range e.subs {
		close(ch)
		delete(e.subs, ch)
	}
	return e.file.Close()
}
func (h *Hub) Tail(id string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e := h.jobs[id]; e != nil {
		return e.ring.Snapshot()
	}
	return nil
}

// Subscribe 在锁内完成快照和注册，确保二者之间没有丢行或重复。
func (h *Hub) Subscribe(id string) (snapshot []string, lines <-chan string, cancel func(), ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.jobs[id]
	if e == nil {
		return nil, nil, func() {}, false
	}
	ch := make(chan string, h.capacity)
	if e.done {
		close(ch)
	} else {
		e.subs[ch] = struct{}{}
	}
	return e.ring.Snapshot(), ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, exists := e.subs[ch]; exists {
			delete(e.subs, ch)
			close(ch)
		}
	}, true
}
func (h *Hub) History(id string) (io.ReadCloser, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.root.Open(id + ".log")
}

// Forget 释放已结束任务的内存，磁盘日志继续可用。
func (h *Hub) Forget(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e := h.jobs[id]; e != nil && e.done {
		delete(h.jobs, id)
	}
}
func (h *Hub) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var err error
	for _, e := range h.jobs {
		if !e.done {
			e.done = true
			err = errors.Join(err, e.file.Close())
			for ch := range e.subs {
				close(ch)
				delete(e.subs, ch)
			}
		}
	}
	return errors.Join(err, h.root.Close())
}
