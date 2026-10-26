package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/config"
	"github.com/gzzchh/asu-go/internal/db"
	"github.com/gzzchh/asu-go/internal/sources"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/crypto/bcrypt"
)

func sessionConfig(t *testing.T) config.Admin {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return config.Admin{APIToken: "token", SessionSecret: "test-secret", SessionTTL: time.Hour,
		Users: []config.AdminUser{{Name: "alice", PasswordHash: string(hash)}}}
}

func sessionRequest(router http.Handler, method, path, body string, cookie *http.Cookie, header string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if header != "" {
		r.Header.Set("X-Requested-With", header)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func TestSessionFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group, err := RegisterAuth(router, sessionConfig(t), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	RegisterSources(group, sources.NewStore(database))
	// 公开 API 尚未落地：用占位 handler 验证认证仅限管理组，不改变公开路由。
	for _, path := range []string{"/api/v1/test", "/json/v1/test", "/store/test"} {
		router.GET(path, func(c *gin.Context) { c.Status(204) })
		if w := sessionRequest(router, "GET", path, "", nil, ""); w.Code != 204 {
			t.Fatalf("公开路径 %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/admin/api/me", "/admin/api/sources"} {
		if w := sessionRequest(router, "GET", path, "", nil, ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	wrong := sessionRequest(router, "POST", "/admin/api/login", `{"name":"alice","password":"wrong"}`, nil, "")
	unknown := sessionRequest(router, "POST", "/admin/api/login", `{"name":"unknown","password":"wrong"}`, nil, "")
	if wrong.Code != 401 || unknown.Code != 401 || wrong.Body.String() != unknown.Body.String() {
		t.Fatal("登录失败信息必须统一")
	}
	w := sessionRequest(router, "POST", "/admin/api/login", `{"name":"alice","password":"correct"}`, nil, "")
	if w.Code != 200 {
		t.Fatalf("登录: %d %s", w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("缺少 cookie")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/admin" || cookie.MaxAge != 3600 {
		t.Fatalf("cookie 属性: %+v", cookie)
	}
	if w := sessionRequest(router, "GET", "/admin/api/me", "", cookie, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "alice") {
		t.Fatal(w.Code, w.Body)
	}
	if w := sessionRequest(router, "GET", "/admin/api/sources", "", cookie, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, header := range []string{"", "wrong"} {
		if w := sessionRequest(router, "POST", "/admin/api/logout", "", cookie, header); w.Code != 403 {
			t.Fatal("缺少 CSRF 防护", w.Code)
		}
	}
	w = sessionRequest(router, "POST", "/admin/api/logout", "", cookie, "asu-admin")
	if w.Code != 204 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("登出未清除 cookie")
	}
	if w := sessionRequest(router, "GET", "/admin/api/me", "", w.Result().Cookies()[0], ""); w.Code != 401 {
		t.Fatal("登出后仍可访问")
	}
	cookie.Value += "x"
	if w := sessionRequest(router, "GET", "/admin/api/me", "", cookie, ""); w.Code != 401 {
		t.Fatal("篡改 cookie 被接受")
	}
	// Bearer 不依赖 cookie，也不要求 CSRF 头，即使同时存在无效 cookie。
	r := httptest.NewRequest("POST", "/admin/api/logout", nil)
	r.AddCookie(cookie)
	r.Header.Set("Authorization", "Bearer token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("Bearer 写请求失败", w.Code)
	}
}

func TestSessionExpiryAndThrottle(t *testing.T) {
	cfg := sessionConfig(t)
	s, err := newSessions(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	s.now = func() time.Time { return now }
	r := httptest.NewRequest("GET", "/admin/api/me", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: s.encode("alice")})
	if _, ok := s.authenticate(r); !ok {
		t.Fatal("有效 cookie 被拒绝")
	}
	now = now.Add(cfg.SessionTTL)
	if _, ok := s.authenticate(r); ok {
		t.Fatal("过期 cookie 被接受")
	}
	router := gin.New()
	router.POST("/login", s.login)
	for i := range loginLimit + 1 {
		w := sessionRequest(router, "POST", "/login", `{"name":"alice","password":"wrong"}`, nil, "")
		want := 401
		if i == loginLimit {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("第 %d 次: %d", i, w.Code)
		}
	}
	// 用户/IP 独立，客户端无法通过伪造 X-Forwarded-For 绕过。
	r = httptest.NewRequest("POST", "/login", strings.NewReader(`{"name":"alice","password":"correct"}`))
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 429 {
		t.Fatal("伪造地址绕过限制")
	}
	if w := sessionRequest(router, "POST", "/login", `{"name":"bob","password":"wrong"}`, nil, ""); w.Code != 401 {
		t.Fatal("不同用户名应独立")
	}
	r = httptest.NewRequest("POST", "/login", strings.NewReader(`{"name":"alice","password":"wrong"}`))
	r.RemoteAddr = "192.0.2.2:1234"
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("不同 IP 应独立")
	}
	now = now.Add(loginWindow)
	if w := sessionRequest(router, "POST", "/login", `{"name":"alice","password":"correct"}`, nil, ""); w.Code != 200 {
		t.Fatal("窗口到期未恢复", w.Code)
	}
	if len(s.attempts) != 0 {
		t.Fatal("成功登录未清除计数或过期项")
	}
}

func TestThrottleConcurrentAndBounded(t *testing.T) {
	s, err := newSessions(sessionConfig(t), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0
	for range 20 {
		wg.Go(func() {
			if s.reserve("same") {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if accepted != loginLimit {
		t.Fatalf("并发限流: %d", accepted)
	}
	for i := range maxLoginKeys {
		s.attempts[string(rune(i))+"key"] = loginAttempt{until: time.Now().Add(time.Hour)}
	}
	if s.reserve("new") {
		t.Fatal("计数表容量未限制")
	}
}

func TestNoCredentialsAndEphemeralSecret(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	cfg := config.Default().Admin
	s, err := newSessions(cfg, zap.New(core))
	if err != nil {
		t.Fatal(err)
	}
	other, err := newSessions(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if string(s.secret) == string(other.secret) || len(s.secret) != 32 || logs.Len() != 1 {
		t.Fatal("临时密钥生成/警告失败")
	}
	router := gin.New()
	group, err := RegisterAuth(router, cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	group.GET("/test", func(c *gin.Context) { c.Status(204) })
	if w := sessionRequest(router, "GET", "/admin/api/test", "", nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
}
