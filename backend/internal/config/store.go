// Package config 管理 fnos-vm-remote 的持久化配置、鉴权信息和日志。
//
// 所有持久化文件位于应用数据目录（TRIM_PKGVAR），升级时由 FNOS 保留：
//   - config.json   服务配置（当前仅包含端口）
//   - auth.json     Token 的 SHA-256 哈希及元数据（不存明文）
//   - logs/         操作日志（JSON Lines，自动轮转，上限约 512KB + 1 个备份）
package config

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultPort    = 8321
	AuthFileName   = "auth.json"
	ConfigFileName = "config.json"

	maxLogBytes = 512 * 1024
)

// Config 是应用配置。
type Config struct {
	Port int `json:"port"`
}

// AuthState 是 Token 鉴权的持久化状态。
type AuthState struct {
	TokenSHA256 string    `json:"token_sha256"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store 封装数据目录下的所有持久化操作。
type Store struct {
	Dir string
}

func NewStore(dir string) *Store { return &Store{Dir: dir} }

func (s *Store) configPath() string { return filepath.Join(s.Dir, ConfigFileName) }
func (s *Store) authPath() string   { return filepath.Join(s.Dir, AuthFileName) }
func (s *Store) logDir() string     { return filepath.Join(s.Dir, "logs") }
func (s *Store) logPath() string    { return filepath.Join(s.logDir(), "fnos-vm-remote.log") }

// EnsureDirs 创建数据目录及日志目录，权限 0700/0750。
func (s *Store) EnsureDirs() error {
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return err
	}
	return os.MkdirAll(s.logDir(), 0o750)
}

// LoadConfig 读取配置；文件缺失时返回默认配置；文件损坏时返回错误。
func (s *Store) LoadConfig() (Config, error) {
	var cfg Config
	data, err := os.ReadFile(s.configPath())
	if errors.Is(err, os.ErrNotExist) {
		return Config{Port: DefaultPort}, nil
	}
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("config.json 解析失败: %w", err)
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return Config{}, fmt.Errorf("config.json 中的端口非法: %d", cfg.Port)
	}
	return cfg, nil
}

// SaveConfig 原子写入配置。
func (s *Store) SaveConfig(cfg Config) error {
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("端口非法: %d", cfg.Port)
	}
	return atomicWriteJSON(s.configPath(), cfg, 0o600)
}

// ValidateToken 校验 Bearer Token 是否与存储的哈希匹配。
func (s *Store) ValidateToken(token string) bool {
	auth, err := s.loadAuth()
	if err != nil || auth == nil || token == "" {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	got := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(auth.TokenSHA256)) == 1
}

// HasToken 报告是否已配置 Token。
func (s *Store) HasToken() bool {
	auth, err := s.loadAuth()
	return err == nil && auth != nil && auth.TokenSHA256 != ""
}

// TokenMeta 返回 Token 元数据（不包含哈希值）。
func (s *Store) TokenMeta() (map[string]any, error) {
	auth, err := s.loadAuth()
	if err != nil {
		return nil, err
	}
	if auth == nil {
		return nil, errors.New("尚未生成 Token")
	}
	return map[string]any{
		"created_at": auth.CreatedAt.Format(time.RFC3339),
		"updated_at": auth.UpdatedAt.Format(time.RFC3339),
	}, nil
}

// SetToken 以新 Token 的哈希覆盖存储（用于重新生成）。
func (s *Store) SetToken(token string) error {
	if len(token) < 16 {
		return errors.New("Token 长度至少为 16 个字符")
	}
	auth, _ := s.loadAuth()
	now := time.Now().UTC()
	created := now
	if auth != nil && !auth.CreatedAt.IsZero() {
		created = auth.CreatedAt
	}
	sum := sha256.Sum256([]byte(token))
	return atomicWriteJSON(s.authPath(), AuthState{
		TokenSHA256: hex.EncodeToString(sum[:]),
		CreatedAt:   created,
		UpdatedAt:   now,
	}, 0o600)
}

func (s *Store) loadAuth() (*AuthState, error) {
	data, err := os.ReadFile(s.authPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var auth AuthState
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("auth.json 解析失败: %w", err)
	}
	return &auth, nil
}

// GenerateToken 生成 48 字符的高强度随机 Token（base64url，36 字节熵）。
func GenerateToken() (string, error) {
	buf := make([]byte, 36)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf) // 48 chars
	return token, nil
}

// InitialTokenPath 返回初始 Token 明文文件路径（安装时一次性写入）。
func (s *Store) InitialTokenPath() string { return filepath.Join(s.Dir, "initial-token.txt") }

// WriteInitialToken 将安装时生成的 Token 明文一次性写入（0600）。
func (s *Store) WriteInitialToken(token string) error {
	return os.WriteFile(s.InitialTokenPath(), []byte(token+"\n"), 0o600)
}

// RemoveInitialToken 删除初始 Token 文件（Token 重新生成后调用）。
func (s *Store) RemoveInitialToken() {
	_ = os.Remove(s.InitialTokenPath())
}

// AppendLog 追加一条操作日志并执行轮转。永不记录 Token。
func (s *Store) AppendLog(entry map[string]any) error {
	if err := s.EnsureDirs(); err != nil {
		return err
	}
	entry["ts"] = time.Now().UTC().Format(time.RFC3339)
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	path := s.logPath()
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogBytes {
		_ = os.Rename(path, path+".1") // 覆盖旧备份
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// ReadLogs 读取最近的日志（新→旧），最多 limit 条。
func (s *Store) ReadLogs(limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	path := s.logPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	out := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			out = append(out, entry)
		}
	}
	// 新的在前
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ClearLogs 清空日志文件。
func (s *Store) ClearLogs() error {
	if err := s.EnsureDirs(); err != nil {
		return err
	}
	return os.WriteFile(s.logPath(), []byte{}, 0o600)
}

func atomicWriteJSON(path string, v any, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
