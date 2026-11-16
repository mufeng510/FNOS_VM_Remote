package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenLifecycle(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if s.HasToken() {
		t.Fatal("fresh store must not have a token")
	}

	token, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 48 {
		t.Errorf("expected 48-char token, got %d", len(token))
	}
	if err := s.SetToken(token); err != nil {
		t.Fatal(err)
	}
	if !s.HasToken() {
		t.Fatal("token should be stored")
	}
	if !s.ValidateToken(token) {
		t.Fatal("correct token must validate")
	}
	if s.ValidateToken(token + "x") {
		t.Fatal("wrong token must not validate")
	}
	if s.ValidateToken("") {
		t.Fatal("empty token must not validate")
	}

	// 明文 Token 不得出现在 auth.json 中
	data, err := os.ReadFile(filepath.Join(dir, AuthFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("auth.json must not contain the plaintext token")
	}

	// 文件权限必须是 0600
	fi, err := os.Stat(filepath.Join(dir, AuthFileName))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("auth.json perm = %o, want 600", fi.Mode().Perm())
	}

	// 轮换后旧 token 失效
	token2, _ := GenerateToken()
	if err := s.SetToken(token2); err != nil {
		t.Fatal(err)
	}
	if s.ValidateToken(token) {
		t.Fatal("old token must be invalid after rotation")
	}
	if !s.ValidateToken(token2) {
		t.Fatal("new token must validate")
	}
}

func TestConfigLoadSave(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	cfg, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("default port = %d, want %d", cfg.Port, DefaultPort)
	}
	p := 9130
	if err := s.SaveConfig(Config{Port: p}); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != p {
		t.Errorf("port = %d, want %d", cfg.Port, p)
	}
	if err := s.SaveConfig(Config{Port: 0}); err == nil {
		t.Error("port 0 must be rejected")
	}
	if err := s.SaveConfig(Config{Port: 70000}); err == nil {
		t.Error("port 70000 must be rejected")
	}
}

func TestConfigCorrupted(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadConfig(); err == nil {
		t.Fatal("corrupted config must return error")
	}
}

func TestLogs(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	for i := 0; i < 5; i++ {
		if err := s.AppendLog(map[string]any{"action": "vm.start", "result": "ok", "n": i}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := s.ReadLogs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
	// 新的在前
	if int(entries[0]["n"].(float64)) != 4 {
		t.Errorf("newest entry expected first, got %v", entries[0]["n"])
	}
	// token 不应自动出现在日志中
	if err := s.ClearLogs(); err != nil {
		t.Fatal(err)
	}
	entries, err = s.ReadLogs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", len(entries))
	}
}
