package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chisaato/asu-go/internal/config"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserNotFound    = errors.New("用户不存在")
	ErrInvalidPassword = errors.New("密码错误")
	ErrUserDisabled    = errors.New("用户已被禁用")
	ErrSessionRevoked  = errors.New("会话已失效")
	ErrReplayDetected  = errors.New("检测到令牌重放攻击")
)

type User struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Role        string     `json:"role"`
	DisabledAt  *time.Time `json:"disabled_at,omitempty"`
}

type AuthStore struct {
	db *sql.DB
}

func NewAuthStore(db *sql.DB) *AuthStore {
	return &AuthStore{db: db}
}

// SyncUsers 将配置中的用户同步导入 admin_users 表，确保初次启动或通过配置定义的用户立即可用。
func (s *AuthStore) SyncUsers(ctx context.Context, users []config.AdminUser) error {
	if s.db == nil {
		return nil
	}
	now := time.Now().UTC()
	for _, u := range users {
		id := generateUUID()
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO admin_users (id, username, display_name, password_hash, role, created_at, password_changed_at)
			VALUES (?, ?, ?, ?, 'admin', ?, ?)
			ON CONFLICT(username) DO UPDATE SET
				password_hash = excluded.password_hash,
				password_changed_at = excluded.password_changed_at;
		`, id, u.Name, u.Name, u.PasswordHash, now, now)
		if err != nil {
			return fmt.Errorf("同步管理用户 %s: %w", u.Name, err)
		}
	}
	return nil
}

// AuthenticateUser 验证用户名和密码，检查是否被禁用。
func (s *AuthStore) AuthenticateUser(ctx context.Context, username, password string) (*User, error) {
	if s.db == nil {
		return nil, ErrUserNotFound
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, display_name, password_hash, role, disabled_at
		FROM admin_users
		WHERE username = ?;
	`, username)

	var u User
	var hash sql.NullString
	var disabledAt sql.NullTime
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &hash, &u.Role, &disabledAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("查询用户: %w", err)
	}

	if disabledAt.Valid {
		u.DisabledAt = &disabledAt.Time
		return nil, ErrUserDisabled
	}
	if !hash.Valid || hash.String == "" {
		return nil, ErrInvalidPassword
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash.String), []byte(password)); err != nil {
		return nil, ErrInvalidPassword
	}

	return &u, nil
}

// AuthenticateExternal 为 OIDC / 外部 OAuth 提供的认证接缝，利用 (issuer, subject) 唯一映射用户。
func (s *AuthStore) AuthenticateExternal(ctx context.Context, issuer, subject, displayName string) (*User, error) {
	if s.db == nil {
		return nil, ErrUserNotFound
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.display_name, u.role, u.disabled_at
		FROM admin_users u
		JOIN admin_user_identities i ON u.id = i.user_id
		WHERE i.issuer = ? AND i.subject = ?;
	`, issuer, subject)

	var u User
	var disabledAt sql.NullTime
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &disabledAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("查询外部身份用户: %w", err)
	}
	if disabledAt.Valid {
		u.DisabledAt = &disabledAt.Time
		return nil, ErrUserDisabled
	}
	return &u, nil
}

// LinkIdentity 将外部 OIDC 身份 (issuer, subject) 绑定至指定内部用户。
func (s *AuthStore) LinkIdentity(ctx context.Context, userID, issuer, subject string) error {
	if s.db == nil {
		return errors.New("数据库未就绪")
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO admin_user_identities (id, user_id, issuer, subject, created_at)
		VALUES (?, ?, ?, ?, ?);
	`, generateUUID(), userID, issuer, subject, now)
	return err
}

// ProvisionExternalUser 根据外部身份 (issuer, subject) 查找已绑定的内部用户；若首次登录则自动分配内部用户并建立绑定。
func (s *AuthStore) ProvisionExternalUser(ctx context.Context, issuer, subject, preferredUsername, displayName string) (*User, error) {
	if s.db == nil {
		return nil, errors.New("数据库未就绪")
	}
	user, err := s.AuthenticateExternal(ctx, issuer, subject, displayName)
	if err == nil || !errors.Is(err, ErrUserNotFound) {
		return user, err
	}

	// 首次外部登录：自动创建内部用户
	baseName := strings.TrimSpace(preferredUsername)
	if baseName == "" {
		subPrefix := subject
		if len(subPrefix) > 8 {
			subPrefix = subPrefix[:8]
		}
		baseName = "oidc_" + subPrefix
	}
	if displayName == "" {
		displayName = baseName
	}

	now := time.Now().UTC()
	userID := generateUUID()
	username := baseName

	// 尝试插入，若用户名冲突则追加随机后缀
	for i := 0; i < 5; i++ {
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO admin_users (id, username, display_name, password_hash, role, created_at, password_changed_at)
			VALUES (?, ?, ?, NULL, 'admin', ?, ?);
		`, userID, username, displayName, now, now)
		if err == nil {
			break
		}
		username = fmt.Sprintf("%s_%s", baseName, generateUUID()[:4])
	}
	if err != nil {
		return nil, fmt.Errorf("创建 OIDC 内部用户失败: %w", err)
	}

	if err := s.LinkIdentity(ctx, userID, issuer, subject); err != nil {
		return nil, fmt.Errorf("关联 OIDC 身份失败: %w", err)
	}

	return &User{
		ID:          userID,
		Username:    username,
		DisplayName: displayName,
		Role:        "admin",
	}, nil
}

// CreateSession 创建新会话，并生成初始刷新令牌。
func (s *AuthStore) CreateSession(ctx context.Context, userID, userAgent, ip string, refreshTTL, refreshAbsoluteTTL time.Duration, now time.Time) (sessionID, rawRefresh string, expiresAt time.Time, err error) {
	if s.db == nil {
		return "", "", time.Time{}, errors.New("数据库未就绪")
	}
	sessionID = generateUUID()
	familyID := generateUUID()
	rawRefresh = GenerateSecureToken()
	tokenHash := HashToken(rawRefresh)

	expiresAt = now.Add(refreshTTL)
	absoluteExpiresAt := now.Add(refreshAbsoluteTTL)
	if expiresAt.After(absoluteExpiresAt) {
		expiresAt = absoluteExpiresAt
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", time.Time{}, err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO admin_auth_sessions (id, user_id, family_id, user_agent, ip, created_at, last_used_at)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`, sessionID, userID, familyID, userAgent, ip, now, now)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("创建会话: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO admin_refresh_tokens (id, session_id, token_hash, issued_at, expires_at, absolute_expires_at)
		VALUES (?, ?, ?, ?, ?, ?);
	`, generateUUID(), sessionID, tokenHash, now, expiresAt, absoluteExpiresAt)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("记录刷新令牌: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", "", time.Time{}, err
	}
	return sessionID, rawRefresh, expiresAt, nil
}

// RotateRefreshToken 执行单次轮换并检测重放攻击。
func (s *AuthStore) RotateRefreshToken(ctx context.Context, rawRefreshToken, userAgent, ip string, refreshTTL time.Duration, now time.Time) (*User, string, string, time.Time, error) {
	if s.db == nil {
		return nil, "", "", time.Time{}, errors.New("数据库未就绪")
	}
	tokenHash := HashToken(rawRefreshToken)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", "", time.Time{}, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
		SELECT r.id, r.session_id, r.expires_at, r.absolute_expires_at, r.used_at,
		       s.user_id, s.family_id, s.revoked_at,
		       u.username, u.display_name, u.role, u.disabled_at
		FROM admin_refresh_tokens r
		JOIN admin_auth_sessions s ON r.session_id = s.id
		JOIN admin_users u ON s.user_id = u.id
		WHERE r.token_hash = ?;
	`, tokenHash)

	var (
		tokenID           string
		sessionID         string
		expiresAt         time.Time
		absoluteExpiresAt time.Time
		usedAt            sql.NullTime
		userID            string
		familyID          string
		sessionRevokedAt  sql.NullTime
		u                 User
		userDisabledAt    sql.NullTime
	)

	if err := row.Scan(&tokenID, &sessionID, &expiresAt, &absoluteExpiresAt, &usedAt, &userID, &familyID, &sessionRevokedAt, &u.Username, &u.DisplayName, &u.Role, &userDisabledAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", "", time.Time{}, ErrInvalidToken
		}
		return nil, "", "", time.Time{}, fmt.Errorf("读取刷新令牌: %w", err)
	}

	u.ID = userID

	// 重放攻击检测：如果此刷新令牌已经被使用过，则代表发生令牌重放，吊销整条令牌族！
	if usedAt.Valid {
		_, _ = tx.ExecContext(ctx, `
			UPDATE admin_auth_sessions
			SET revoked_at = ?, revoke_reason = 'refresh-replay'
			WHERE family_id = ?;
		`, now, familyID)
		_ = tx.Commit()
		return nil, "", "", time.Time{}, ErrReplayDetected
	}

	if sessionRevokedAt.Valid {
		return nil, "", "", time.Time{}, ErrSessionRevoked
	}
	if userDisabledAt.Valid {
		return nil, "", "", time.Time{}, ErrUserDisabled
	}
	if !now.Before(expiresAt) || !now.Before(absoluteExpiresAt) {
		return nil, "", "", time.Time{}, ErrTokenExpired
	}

	// 颁发新令牌并标记旧令牌
	newRawRefresh := GenerateSecureToken()
	newTokenHash := HashToken(newRawRefresh)
	newTokenID := generateUUID()

	newExpiresAt := now.Add(refreshTTL)
	if newExpiresAt.After(absoluteExpiresAt) {
		newExpiresAt = absoluteExpiresAt
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE admin_refresh_tokens
		SET used_at = ?, replaced_by = ?
		WHERE id = ?;
	`, now, newTokenID, tokenID)
	if err != nil {
		return nil, "", "", time.Time{}, fmt.Errorf("标记旧刷新令牌: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO admin_refresh_tokens (id, session_id, token_hash, issued_at, expires_at, absolute_expires_at)
		VALUES (?, ?, ?, ?, ?, ?);
	`, newTokenID, sessionID, newTokenHash, now, newExpiresAt, absoluteExpiresAt)
	if err != nil {
		return nil, "", "", time.Time{}, fmt.Errorf("插入新刷新令牌: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE admin_auth_sessions
		SET last_used_at = ?, user_agent = ?, ip = ?
		WHERE id = ?;
	`, now, userAgent, ip, sessionID)
	if err != nil {
		return nil, "", "", time.Time{}, fmt.Errorf("更新会话状态: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, "", "", time.Time{}, err
	}

	return &u, sessionID, newRawRefresh, newExpiresAt, nil
}

// RevokeSession 吊销指定会话。
func (s *AuthStore) RevokeSession(ctx context.Context, sessionID, reason string, now time.Time) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE admin_auth_sessions
		SET revoked_at = ?, revoke_reason = ?
		WHERE id = ? AND revoked_at IS NULL;
	`, now, reason, sessionID)
	return err
}

// IsSessionActive 检查指定会话是否依然有效。
func (s *AuthStore) IsSessionActive(ctx context.Context, sessionID string) bool {
	if s.db == nil || sessionID == "" {
		return true
	}
	var revokedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT revoked_at FROM admin_auth_sessions WHERE id = ?;
	`, sessionID).Scan(&revokedAt)
	if err != nil {
		return false
	}
	return !revokedAt.Valid
}
