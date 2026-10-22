package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
)

// Terminal 是外部执行体的身份。
type Terminal struct {
	ID       string
	Name     string
	BannedAt sql.NullString
}

// CreateTerminal 生成一个终端与一把一次性展示的 token（库里只存哈希）。
func (s *Store) CreateTerminal(name string) (string, string, error) {
	token, err := randomToken()
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(token))
	_, err = s.db.Exec(`INSERT INTO terminals (id, name, token_hash, created_at) VALUES (?,?,?,?)`,
		name, name, sum[:], now())
	if err != nil {
		return "", "", err
	}
	return name, token, nil
}

// TerminalByToken 用 token 反查终端；已吊销的返回 sql.ErrNoRows。
func (s *Store) TerminalByToken(token string) (*Terminal, error) {
	sum := sha256.Sum256([]byte(token))
	var t Terminal
	err := s.db.QueryRow(`SELECT id, name, banned_at FROM terminals WHERE token_hash = ? AND banned_at IS NULL`,
		sum[:]).Scan(&t.ID, &t.Name, &t.BannedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// BanTerminal 吊销一个终端。
func (s *Store) BanTerminal(id string) error {
	_, err := s.db.Exec(`UPDATE terminals SET banned_at = ? WHERE id = ?`, now(), id)
	return err
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ov_" + hex.EncodeToString(b), nil
}

// randomID 生成带前缀的短 id，用于 lease 等。
func randomID(prefix string) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

var _ = fmt.Sprintf
