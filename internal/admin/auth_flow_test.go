package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chisaato/asu-go/internal/config"
	"github.com/chisaato/asu-go/internal/db"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

func setupTestDB(t *testing.T) (*db.DB, string) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return database, dir
}

func TestDualTokenFlowAndRestartPersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, dataDir := setupTestDB(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.MinCost)
	cfg := config.Admin{
		Users:              []config.AdminUser{{Name: "admin_user", PasswordHash: string(hash)}},
		AccessTokenTTL:     15 * time.Minute,
		RefreshTokenTTL:    30 * 24 * time.Hour,
		RefreshAbsoluteTTL: 90 * 24 * time.Hour,
	}

	router1 := gin.New()
	_, err := RegisterAuth(router1, cfg, zap.NewNop(), WithDataDir(dataDir), WithDB(database.DB))
	if err != nil {
		t.Fatal(err)
	}

	// 1. 登录
	loginReq := httptest.NewRequest("POST", "/admin/api/auth/login", strings.NewReader(`{"name":"admin_user","password":"secret123"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router1.ServeHTTP(w1, loginReq)
	if w1.Code != http.StatusOK {
		t.Fatalf("登录失败: %d %s", w1.Code, w1.Body.String())
	}

	var accessCookie, refreshCookie *http.Cookie
	for _, c := range w1.Result().Cookies() {
		switch c.Name {
		case accessTokenCookie:
			accessCookie = c
		case refreshTokenCookie:
			refreshCookie = c
		}
	}
	if accessCookie == nil || refreshCookie == nil {
		t.Fatalf("未同时签发 Access Token 与 Refresh Token: %+v", w1.Result().Cookies())
	}
	if refreshCookie.Path != "/admin/api/auth" || !refreshCookie.HttpOnly {
		t.Fatalf("Refresh Token Cookie 作用域或安全性不符合规范: %+v", refreshCookie)
	}

	// 2. 模拟进程重启：创建全新的 router，指向相同的 dataDir 和 database
	// 验证持久化密钥是否使得重启后的进程依然认可原有的 Access Token
	keyFile := filepath.Join(dataDir, "keys", "jwt-signing.key")
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("签名密钥未成功写盘: %v", err)
	}

	router2 := gin.New()
	_, err = RegisterAuth(router2, cfg, zap.NewNop(), WithDataDir(dataDir), WithDB(database.DB))
	if err != nil {
		t.Fatal(err)
	}

	// 使用重启前签发的 Access Token 访问受保护接口
	meReq := httptest.NewRequest("GET", "/admin/api/auth/me", nil)
	meReq.AddCookie(accessCookie)
	w2 := httptest.NewRecorder()
	router2.ServeHTTP(w2, meReq)
	if w2.Code != http.StatusOK || !strings.Contains(w2.Body.String(), "admin_user") {
		t.Fatalf("进程重启后丢失登录态: %d %s", w2.Code, w2.Body.String())
	}

	// 3. 静默刷新（Token Rotation）：使用 Refresh Token 获取新 Token
	refreshReq := httptest.NewRequest("POST", "/admin/api/auth/refresh", nil)
	refreshReq.Header.Set("X-Requested-With", "asu-admin")
	refreshReq.AddCookie(refreshCookie)
	w3 := httptest.NewRecorder()
	router2.ServeHTTP(w3, refreshReq)
	if w3.Code != http.StatusOK {
		t.Fatalf("静默刷新失败: %d %s", w3.Code, w3.Body.String())
	}

	var newAccessCookie, newRefreshCookie *http.Cookie
	for _, c := range w3.Result().Cookies() {
		switch c.Name {
		case accessTokenCookie:
			newAccessCookie = c
		case refreshTokenCookie:
			newRefreshCookie = c
		}
	}
	if newAccessCookie == nil || newRefreshCookie == nil {
		t.Fatal("刷新后未下发新的双 Token")
	}
	if newRefreshCookie.Value == refreshCookie.Value {
		t.Fatal("Refresh Token 未发生轮换！")
	}

	// 4. 重放攻击测试：再次使用已被轮换作废的旧 refreshCookie
	replayReq := httptest.NewRequest("POST", "/admin/api/auth/refresh", nil)
	replayReq.Header.Set("X-Requested-With", "asu-admin")
	replayReq.AddCookie(refreshCookie)
	w4 := httptest.NewRecorder()
	router2.ServeHTTP(w4, replayReq)
	if w4.Code != http.StatusUnauthorized {
		t.Fatalf("重放攻击未被拦截: %d", w4.Code)
	}

	// 整族吊销验证：重放攻击发生后，即便是刚刚轮换出的 newAccessCookie 也应随整族失效
	meReqRevoked := httptest.NewRequest("GET", "/admin/api/auth/me", nil)
	meReqRevoked.AddCookie(newAccessCookie)
	w5 := httptest.NewRecorder()
	router2.ServeHTTP(w5, meReqRevoked)
	if w5.Code != http.StatusUnauthorized {
		t.Fatalf("整族吊销未生效，受害会话依然可用: %d", w5.Code)
	}
}

func TestExternalIdentityAndOIDCPreparation(t *testing.T) {
	database, _ := setupTestDB(t)
	store := NewAuthStore(database.DB)
	ctx := context.Background()

	// 1. 同步内部用户
	err := store.SyncUsers(ctx, []config.AdminUser{{Name: "github_user"}})
	if err != nil {
		t.Fatal(err)
	}

	// 2. 查询内部用户
	_, err = store.AuthenticateUser(ctx, "github_user", "")
	// 因为未设密码，所以期望非 ErrUserNotFound
	if err != ErrInvalidPassword {
		t.Fatalf("期望 ErrInvalidPassword，实际: %v", err)
	}

	// 手动从 DB 提取 user.ID
	var userID string
	err = database.DB.QueryRow("SELECT id FROM admin_users WHERE username = 'github_user'").Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}

	// 3. 模拟 OIDC 流程：将外部身份 (issuer, subject) 绑定至用户
	issuer := "https://oidc.example.com"
	subject := "sub_123456789"
	err = store.LinkIdentity(ctx, userID, issuer, subject)
	if err != nil {
		t.Fatalf("绑定外部身份失败: %v", err)
	}

	// 联合唯一键约束校验
	if err := store.LinkIdentity(ctx, userID, issuer, subject); err == nil {
		t.Fatal("重复绑定相同 (issuer, subject) 应产生唯一键冲突")
	}

	// 4. 通过 OIDC 外部身份鉴权
	extUser, err := store.AuthenticateExternal(ctx, issuer, subject, "GitHub User")
	if err != nil {
		t.Fatalf("外部身份鉴权失败: %v", err)
	}
	if extUser.Username != "github_user" || extUser.ID != userID {
		t.Fatalf("外部身份未正确映射内部用户: %+v", extUser)
	}
}
