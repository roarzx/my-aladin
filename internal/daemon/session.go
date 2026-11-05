package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// SessionRecord 是 Server Session ↔ Provider Session 的映射（C4）。
type SessionRecord struct {
	SessionID    string    `json:"session_id"`
	ACPSessionID string    `json:"acp_session_id"`
	AgentID      string    `json:"agent_id,omitempty"`
	Kind         string    `json:"kind,omitempty"`
	Fingerprint  string    `json:"fingerprint,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// SessionFingerprint 暴露指纹，满足 session.ResumableSession（E3）。
func (r SessionRecord) SessionFingerprint() string { return r.Fingerprint }

type sessionFile struct {
	Version  int                      `json:"version"`
	Sessions map[string]SessionRecord `json:"sessions"`
}

// SessionStore 持久化会话映射；写入使用临时文件 + rename。
type SessionStore struct {
	mu      sync.Mutex
	path    string
	records map[string]SessionRecord
}

// OpenSessionStore 加载 session_history.json；文件不存在视为空，损坏则报错。
func OpenSessionStore(path string) (*SessionStore, error) {
	s := &SessionStore{path: path, records: make(map[string]SessionRecord)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取会话历史: %w", err)
	}
	var file sessionFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("解析会话历史 %s: %w", path, err)
	}
	if file.Sessions != nil {
		s.records = file.Sessions
	}
	return s, nil
}

// Get 读取映射。
func (s *SessionStore) Get(sessionID string) (SessionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[sessionID]
	return rec, ok
}

// Put 写入或更新映射，并原子落盘。
func (s *SessionStore) Put(rec SessionRecord) error {
	if rec.SessionID == "" {
		return errors.New("session: session_id 不能为空")
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[rec.SessionID] = rec
	return s.saveLocked()
}

// Delete 删除映射。
func (s *SessionStore) Delete(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[sessionID]; !ok {
		return nil
	}
	delete(s.records, sessionID)
	return s.saveLocked()
}

// Len 返回映射数量。
func (s *SessionStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

func (s *SessionStore) saveLocked() error {
	file := sessionFile{Version: 1, Sessions: s.records}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, append(data, '\n'), 0o600)
}
