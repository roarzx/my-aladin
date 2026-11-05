package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// idemRecord 是落盘的一条命令结果。
type idemRecord struct {
	ID      string          `json:"id"`
	At      time.Time       `json:"at"`
	Payload json.RawMessage `json:"payload"`
}

// IdempotentStore 按 command id 记录执行结果（C3）。
//
// 内存索引只做加速；正确性来自 commands.jsonl：重启后仍能识别重复命令。
type IdempotentStore struct {
	mu      sync.Mutex
	path    string
	ttl     time.Duration
	now     func() time.Time
	records map[string]idemRecord
	file    *os.File
}

// OpenIdempotentStore 加载历史记录并打开追加写入。
func OpenIdempotentStore(path string, ttl time.Duration) (*IdempotentStore, error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("创建命令记录目录: %w", err)
	}
	s := &IdempotentStore{
		path:    path,
		ttl:     ttl,
		now:     time.Now,
		records: make(map[string]idemRecord),
	}
	pruned, err := s.load()
	if err != nil {
		return nil, err
	}
	if pruned {
		if err := s.compact(); err != nil {
			return nil, err
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开命令记录: %w", err)
	}
	s.file = file
	return s, nil
}

// load 读取 JSONL，保留每个 id 的最新记录并丢弃过期记录；返回是否需要压缩。
func (s *IdempotentStore) load() (bool, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取命令记录: %w", err)
	}
	defer func() { _ = f.Close() }()

	pruned := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec idemRecord
		if err := json.Unmarshal(line, &rec); err != nil || rec.ID == "" {
			pruned = true // 跳过损坏行，压缩时清理
			continue
		}
		if s.now().Sub(rec.At) > s.ttl {
			pruned = true
			continue
		}
		if _, dup := s.records[rec.ID]; dup {
			pruned = true
		}
		s.records[rec.ID] = rec
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("扫描命令记录: %w", err)
	}
	return pruned, nil
}

// Get 返回缓存的响应；过期记录视为不存在。
func (s *IdempotentStore) Get(id string) (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	if !ok {
		return nil, false
	}
	if s.now().Sub(rec.At) > s.ttl {
		delete(s.records, id)
		return nil, false
	}
	return append(json.RawMessage(nil), rec.Payload...), true
}

// Put 追加一条记录；同 id 以最后一次为准。
func (s *IdempotentStore) Put(id string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := idemRecord{
		ID:      id,
		At:      s.now().UTC(),
		Payload: append(json.RawMessage(nil), payload...),
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if s.file != nil {
		if _, err := s.file.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("写入命令记录: %w", err)
		}
		if err := s.file.Sync(); err != nil {
			return fmt.Errorf("同步命令记录: %w", err)
		}
	}
	s.records[id] = rec
	return nil
}

// Len 返回内存中的记录数（测试与观测用）。
func (s *IdempotentStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// Close 关闭文件句柄。
func (s *IdempotentStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

// compact 用临时文件 + rename 重写记录文件。
func (s *IdempotentStore) compact() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compactLocked()
}

func (s *IdempotentStore) compactLocked() error {
	var buf []byte
	for _, rec := range s.records {
		line, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return writeFileAtomic(s.path, buf, 0o600)
}
