package admin

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/config"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie = "asu_admin_session"
	loginLimit    = 5
	loginWindow   = 5 * time.Minute
	maxLoginKeys  = 10000
)

type sessionClaims struct {
	Name    string `json:"name"`
	Expires int64  `json:"expires"`
}

type loginAttempt struct {
	count int
	until time.Time
}

// Sessions 不依赖后台 goroutine；失败窗口在下一次登录时惰性回收。
type Sessions struct {
	secret    []byte
	ttl       time.Duration
	users     map[string]string
	dummyHash []byte
	bearer    Authenticator
	now       func() time.Time
	mu        sync.Mutex
	attempts  map[string]loginAttempt
}

// RegisterAuth 返回受保护的管理路由组，调用方应在该组注册所有管理 API。
func RegisterAuth(router *gin.Engine, cfg config.Admin, logger *zap.Logger) (*gin.RouterGroup, error) {
	s, err := newSessions(cfg, logger)
	if err != nil {
		return nil, err
	}
	group := router.Group("/admin/api")
	group.POST("/login", s.login)
	group.Use(s.middleware())
	group.POST("/logout", s.logout)
	group.GET("/me", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"name": c.GetString("admin_name")})
	})
	return group, nil
}

func newSessions(cfg config.Admin, logger *zap.Logger) (*Sessions, error) {
	secret := []byte(cfg.SessionSecret)
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("生成 admin.session_secret: %w", err)
		}
		logger.Warn("admin.session_secret 为空，已生成临时密钥；重启后管理会话失效")
	}
	dummy, err := bcrypt.GenerateFromPassword([]byte("invalid-account-placeholder"), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("初始化密码校验: %w", err)
	}
	s := &Sessions{secret: secret, ttl: cfg.SessionTTL, users: make(map[string]string), dummyHash: dummy,
		bearer: BearerAuthenticator(cfg.APIToken), now: time.Now, attempts: make(map[string]loginAttempt)}
	for _, user := range cfg.Users {
		s.users[user.Name] = user.PasswordHash
	}
	return s, nil
}

func (s *Sessions) sign(payload string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Sessions) encode(name string) string {
	b, _ := json.Marshal(sessionClaims{Name: name, Expires: s.now().Add(s.ttl).Unix()})
	payload := base64.RawURLEncoding.EncodeToString(b)
	return payload + "." + s.sign(payload)
}

func (s *Sessions) authenticate(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) > 4096 {
		return "", false
	}
	payload, signature, ok := strings.Cut(cookie.Value, ".")
	actual, err := base64.RawURLEncoding.DecodeString(signature)
	if !ok || err != nil {
		return "", false
	}
	expected, _ := base64.RawURLEncoding.DecodeString(s.sign(payload))
	if !hmac.Equal(actual, expected) {
		return "", false
	}
	b, err := base64.RawURLEncoding.DecodeString(payload)
	var claims sessionClaims
	if err != nil || json.Unmarshal(b, &claims) != nil || claims.Expires <= s.now().Unix() {
		return "", false
	}
	_, ok = s.users[claims.Name]
	return claims.Name, ok
}

func (s *Sessions) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if s.bearer != nil && s.bearer(c.Request) {
			c.Set("admin_name", "api_token")
			c.Next()
			return
		}
		if name, ok := s.authenticate(c.Request); ok {
			// 自定义头无法由跨站表单设置；管理 API 不开放跨域 CORS。
			if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions && c.GetHeader("X-Requested-With") != "asu-admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "缺少 CSRF 防护头"})
				return
			}
			c.Set("admin_name", name)
			c.Next()
			return
		}
		c.Header("WWW-Authenticate", "Bearer")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "需要有效的管理凭据"})
	}
}

// reserve 在 bcrypt 校验前计数，避免并发请求绕过限制；成功后清除。
func (s *Sessions) reserve(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, attempt := range s.attempts {
		if !now.Before(attempt.until) {
			delete(s.attempts, k)
		}
	}
	a, exists := s.attempts[key]
	if (!exists && len(s.attempts) >= maxLoginKeys) || a.count >= loginLimit {
		return false
	}
	if !exists {
		a.until = now.Add(loginWindow)
	}
	a.count++
	s.attempts[key] = a
	return true
}

func (s *Sessions) login(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var input struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "登录请求无效"})
		return
	}
	// 使用直连地址，不信任客户端提供的 X-Forwarded-For。
	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		ip = c.Request.RemoteAddr
	}
	key := ip + "\x00" + input.Name
	if !s.reserve(key) {
		c.Header("Retry-After", "300")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "登录失败，请稍后重试"})
		return
	}
	hash, exists := s.users[input.Name]
	if !exists {
		hash = string(s.dummyHash)
	}
	checkErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.Password))
	if checkErr != nil || !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	s.mu.Lock()
	delete(s.attempts, key)
	s.mu.Unlock()
	s.setCookie(c, s.encode(input.Name), int(s.ttl.Seconds()), s.now().Add(s.ttl))
	c.JSON(http.StatusOK, gin.H{"name": input.Name})
}

func (s *Sessions) setCookie(c *gin.Context, value string, maxAge int, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: value, Path: "/admin", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: c.Request.TLS != nil, MaxAge: maxAge, Expires: expires})
}

func (s *Sessions) logout(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	s.setCookie(c, "", -1, time.Unix(1, 0))
	c.Status(http.StatusNoContent)
}
