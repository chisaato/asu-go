package admin

import (
	"crypto/hmac"
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

	"go.uber.org/zap"
)

var (
	ErrInvalidToken = errors.New("无效令牌")
	ErrTokenExpired = errors.New("令牌已过期")
)

type TokenService struct {
	key    []byte
	issuer string
	aud    string
	logger *zap.Logger
}

type AccessClaims struct {
	Sub  string `json:"sub"`
	Sid  string `json:"sid"`
	Name string `json:"name"`
	Role string `json:"role"`
	Iss  string `json:"iss"`
	Aud  string `json:"aud"`
	Exp  int64  `json:"exp"`
}

func NewTokenService(cfgSecret, dataDir string, logger *zap.Logger) (*TokenService, error) {
	key, err := resolveSigningKey(cfgSecret, dataDir, logger)
	if err != nil {
		return nil, err
	}
	return &TokenService{
		key:    key,
		issuer: "asu-go",
		aud:    "asu-admin",
		logger: logger,
	}, nil
}

func resolveSigningKey(cfgSecret, dataDir string, logger *zap.Logger) ([]byte, error) {
	if strings.TrimSpace(cfgSecret) != "" {
		return []byte(cfgSecret), nil
	}
	if dataDir != "" {
		keyDir := filepath.Join(dataDir, "keys")
		if err := os.MkdirAll(keyDir, 0o700); err == nil {
			keyPath := filepath.Join(keyDir, "jwt-signing.key")
			if data, err := os.ReadFile(keyPath); err == nil && len(data) >= 32 {
				if decoded, bErr := base64.RawURLEncoding.DecodeString(string(data)); bErr == nil && len(decoded) == 32 {
					return decoded, nil
				}
				return data, nil
			}
			newKey := make([]byte, 32)
			if _, err := rand.Read(newKey); err != nil {
				return nil, fmt.Errorf("生成签名密钥: %w", err)
			}
			encoded := []byte(base64.RawURLEncoding.EncodeToString(newKey))
			if err := os.WriteFile(keyPath, encoded, 0o600); err != nil {
				logger.Warn("持久化签名密钥失败，降级为内存临时密钥", zap.Error(err))
			} else {
				logger.Info("已生成并持久化管理签名密钥", zap.String("path", keyPath))
				return newKey, nil
			}
			return newKey, nil
		}
	}
	memKey := make([]byte, 32)
	if _, err := rand.Read(memKey); err != nil {
		return nil, fmt.Errorf("生成临时密钥: %w", err)
	}
	logger.Warn("admin.session_secret 为空，已生成临时密钥；重启后管理会话失效")
	return memKey, nil
}

func (s *TokenService) IssueAccessToken(userID, sessionID, name, role string, ttl time.Duration, now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(ttl)
	headerJSON := `{"alg":"HS256","typ":"JWT"}`
	header := base64.RawURLEncoding.EncodeToString([]byte(headerJSON))

	claims := AccessClaims{
		Sub:  userID,
		Sid:  sessionID,
		Name: name,
		Role: role,
		Iss:  s.issuer,
		Aud:  s.aud,
		Exp:  expiresAt.Unix(),
	}
	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("序列化 claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(claimsBytes)

	data := header + "." + payload
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(data))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return data + "." + sig, expiresAt, nil
}

func (s *TokenService) ValidateAccessToken(tokenStr string, now time.Time) (*AccessClaims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	data := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(data))
	expectedSig := mac.Sum(nil)

	actualSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || subtle.ConstantTimeCompare(actualSig, expectedSig) != 1 {
		return nil, ErrInvalidToken
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}

	var claims AccessClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, ErrInvalidToken
	}

	if claims.Iss != s.issuer || claims.Aud != s.aud {
		return nil, ErrInvalidToken
	}
	if claims.Exp <= now.Unix() {
		return nil, ErrTokenExpired
	}

	return &claims, nil
}

func GenerateSecureToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func generateUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
