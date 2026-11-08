package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chisaato/asu-go/internal/config"
	logging "github.com/chisaato/asu-go/internal/logcontext"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie      = "asu_admin_session"
	accessTokenCookie  = "asu_access_token"
	refreshTokenCookie = "asu_refresh_token"
	oidcStateCookie    = "asu_oidc_state"
	loginLimit         = 5
	loginWindow        = 5 * time.Minute
	maxLoginKeys       = 10000
)

type sessionClaims struct {
	Name    string `json:"name"`
	Expires int64  `json:"expires"`
}

type loginAttempt struct {
	count int
	until time.Time
}

type authOptions struct {
	dataDir string
	db      *sql.DB
}

type AuthOption func(*authOptions)

func WithDataDir(dataDir string) AuthOption {
	return func(o *authOptions) { o.dataDir = dataDir }
}

func WithDB(db *sql.DB) AuthOption {
	return func(o *authOptions) { o.db = db }
}

// Sessions 管理认证状态、Token 颁发、限流与会话生命周期。
type Sessions struct {
	logger             *zap.Logger
	secret             []byte
	ttl                time.Duration
	accessTTL          time.Duration
	refreshTTL         time.Duration
	refreshAbsoluteTTL time.Duration
	users              map[string]string
	dummyHash          []byte
	bearer             Authenticator
	tokens             *TokenService
	store              *AuthStore
	oidc               *OIDCClient
	oidcCfg            config.OIDC
	now                func() time.Time
	mu                 sync.Mutex
	attempts           map[string]loginAttempt
}

// RegisterAuth 返回受保护的管理路由组，调用方应在该组注册所有管理 API。
func RegisterAuth(router *gin.Engine, cfg config.Admin, logger *zap.Logger, opts ...AuthOption) (*gin.RouterGroup, error) {
	s, err := newSessions(cfg, logger, opts...)
	if err != nil {
		return nil, err
	}

	// 启动时同步配置中定义的用户
	if s.store != nil && len(cfg.Users) > 0 {
		if err := s.store.SyncUsers(context.Background(), cfg.Users); err != nil {
			logger.Warn("同步配置文件中的管理用户失败", zap.Error(err))
		}
	}

	group := router.Group("/admin/api")
	authGroup := group.Group("/auth")

	// 认证配置端点（前端用于探测是否开启 OIDC 等，免认证）
	group.GET("/config", s.authConfig)
	authGroup.GET("/config", s.authConfig)

	// OIDC 登录与回调（免认证）
	group.GET("/oidc/login", s.oidcLogin)
	authGroup.GET("/oidc/login", s.oidcLogin)
	group.GET("/oidc/callback", s.oidcCallback)
	authGroup.GET("/oidc/callback", s.oidcCallback)

	// 登录接口（免认证）
	group.POST("/login", s.login)
	authGroup.POST("/login", s.login)

	// 静默刷新接口（免通用认证，由内部验证 Refresh Token）
	group.POST("/refresh", s.refresh)
	authGroup.POST("/refresh", s.refresh)

	// 受保护的会话管理接口
	protected := group.Group("")
	protected.Use(s.middleware())
	{
		protected.POST("/logout", s.logout)
		authGroupProtected := authGroup.Group("")
		authGroupProtected.Use(s.middleware())
		authGroupProtected.POST("/logout", s.logout)

		protected.GET("/me", s.me)
		authGroupProtected.GET("/me", s.me)
	}

	return protected, nil
}

func newSessions(cfg config.Admin, logger *zap.Logger, opts ...AuthOption) (*Sessions, error) {
	options := authOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	logger = logging.OrNop(logger).With(zap.String("component", "admin"))

	tokens, err := NewTokenService(cfg.SessionSecret, options.dataDir, logger)
	if err != nil {
		return nil, fmt.Errorf("初始化 TokenService: %w", err)
	}

	dummy, err := bcrypt.GenerateFromPassword([]byte("invalid-account-placeholder"), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("初始化密码校验: %w", err)
	}

	accessTTL := cfg.AccessTokenTTL
	if accessTTL <= 0 {
		accessTTL = 15 * time.Minute
	}
	refreshTTL := cfg.RefreshTokenTTL
	if refreshTTL <= 0 {
		refreshTTL = 30 * 24 * time.Hour
	}
	refreshAbsoluteTTL := cfg.RefreshAbsoluteTTL
	if refreshAbsoluteTTL <= 0 {
		refreshAbsoluteTTL = 90 * 24 * time.Hour
	}

	var authStore *AuthStore
	if options.db != nil {
		authStore = NewAuthStore(options.db)
	}

	oidc := NewOIDCClient(cfg.OIDC, logger)

	s := &Sessions{
		logger:             logger,
		secret:             tokens.key,
		ttl:                cfg.SessionTTL,
		accessTTL:          accessTTL,
		refreshTTL:         refreshTTL,
		refreshAbsoluteTTL: refreshAbsoluteTTL,
		users:              make(map[string]string),
		dummyHash:          dummy,
		bearer:             BearerAuthenticator(cfg.APIToken),
		tokens:             tokens,
		store:              authStore,
		oidc:               oidc,
		oidcCfg:            cfg.OIDC,
		now:                time.Now,
		attempts:           make(map[string]loginAttempt),
	}

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
	name, _, _, _, ok := s.authenticateDetailed(r)
	return name, ok
}

func (s *Sessions) authenticateDetailed(r *http.Request) (name, userID, sessionID, role string, ok bool) {
	// 1. 尝试从 Authorization: Bearer 头获取 Token
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		rawToken := strings.TrimSpace(authHeader[7:])
		if claims, err := s.tokens.ValidateAccessToken(rawToken, s.now()); err == nil {
			if s.store == nil || s.store.IsSessionActive(r.Context(), claims.Sid) {
				return claims.Name, claims.Sub, claims.Sid, claims.Role, true
			}
		}
	}

	// 2. 尝试从 asu_access_token Cookie 解析
	if cookie, err := r.Cookie(accessTokenCookie); err == nil && len(cookie.Value) > 0 {
		if claims, err := s.tokens.ValidateAccessToken(cookie.Value, s.now()); err == nil {
			if s.store == nil || s.store.IsSessionActive(r.Context(), claims.Sid) {
				return claims.Name, claims.Sub, claims.Sid, claims.Role, true
			}
		}
	}

	// 3. 兼容旧版 asu_admin_session Cookie
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) > 4096 {
		return "", "", "", "", false
	}
	payload, signature, cutOk := strings.Cut(cookie.Value, ".")
	actual, err := base64.RawURLEncoding.DecodeString(signature)
	if !cutOk || err != nil {
		return "", "", "", "", false
	}
	expected, _ := base64.RawURLEncoding.DecodeString(s.sign(payload))
	if !hmac.Equal(actual, expected) {
		return "", "", "", "", false
	}
	b, err := base64.RawURLEncoding.DecodeString(payload)
	var claims sessionClaims
	if err != nil || json.Unmarshal(b, &claims) != nil || claims.Expires <= s.now().Unix() {
		return "", "", "", "", false
	}

	if s.store != nil {
		ctx := r.Context()
		if _, hashErr := s.store.AuthenticateUser(ctx, claims.Name, ""); !errors.Is(hashErr, ErrUserNotFound) {
			return claims.Name, "", "", "admin", true
		}
	}
	_, inUsers := s.users[claims.Name]
	return claims.Name, "", "", "admin", inUsers
}

func (s *Sessions) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if name := c.GetString("admin_name"); name != "" && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions {
				logging.OrNop(s.logger).Info("管理接口写操作", zap.String("operator", name), zap.String("method", c.Request.Method), zap.String("route", c.FullPath()), zap.String("path", c.Request.URL.Path), zap.Int("status", c.Writer.Status()), zap.String("request_id", logging.RequestID(c.Request.Context())))
			}
		}()
		c.Header("Cache-Control", "no-store")

		// 检查静态 API Token
		if s.bearer != nil && s.bearer(c.Request) {
			c.Set("admin_name", "api_token")
			c.Set("admin_role", "admin")
			c.Next()
			return
		}

		if name, userID, sessionID, role, ok := s.authenticateDetailed(c.Request); ok {
			// 对非安全 HTTP 方法进行 CSRF 防护校验
			if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions && c.GetHeader("X-Requested-With") != "asu-admin" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "缺少 CSRF 防护头"})
				return
			}
			c.Set("admin_name", name)
			c.Set("admin_user_id", userID)
			c.Set("admin_session_id", sessionID)
			c.Set("admin_role", role)
			c.Next()
			return
		}

		c.Header("WWW-Authenticate", "Bearer")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "需要有效的管理凭据"})
	}
}

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
	defer func() {
		log := logging.OrNop(s.logger)
		fields := []zap.Field{zap.String("request_id", logging.RequestID(c.Request.Context())), zap.String("client_ip", c.Request.RemoteAddr), zap.Int("status", c.Writer.Status())}
		if c.Writer.Status() >= 400 {
			log.Warn("管理登录失败", append(fields, zap.Error(fmt.Errorf("login rejected: HTTP %d", c.Writer.Status())))...)
		} else {
			log.Info("管理登录成功", append(fields, zap.String("operator", c.GetString("admin_name")))...)
		}
	}()
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

	var user *User
	if s.store != nil {
		user, err = s.store.AuthenticateUser(c.Request.Context(), input.Name, input.Password)
	} else {
		err = ErrUserNotFound
	}

	if err != nil {
		// 回退内存字典校验
		hash, exists := s.users[input.Name]
		if !exists {
			hash = string(s.dummyHash)
		}
		checkErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.Password))
		if checkErr != nil || !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
			return
		}
		user = &User{ID: generateUUID(), Username: input.Name, DisplayName: input.Name, Role: "admin"}
	}

	s.mu.Lock()
	delete(s.attempts, key)
	s.mu.Unlock()

	now := s.now()
	var sessionID string
	var rawRefresh string
	var refreshExpires time.Time

	if s.store != nil {
		sessionID, rawRefresh, refreshExpires, err = s.store.CreateSession(c.Request.Context(), user.ID, c.Request.UserAgent(), ip, s.refreshTTL, s.refreshAbsoluteTTL, now)
		if err != nil {
			s.logger.Error("创建认证会话失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
			return
		}

		accessToken, accessExpires, err := s.tokens.IssueAccessToken(user.ID, sessionID, user.Username, user.Role, s.accessTTL, now)
		if err != nil {
			s.logger.Error("签发访问令牌失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "签发令牌失败"})
			return
		}

		// 种下访问令牌 Cookie
		s.setCookie(c, accessTokenCookie, accessToken, "/", int(s.accessTTL.Seconds()), accessExpires)
		// 种下严格作用于 /admin/api/auth 的 Refresh Cookie
		s.setCookie(c, refreshTokenCookie, rawRefresh, "/admin/api/auth", int(s.refreshTTL.Seconds()), refreshExpires)
		// 种下兼容的旧 session Cookie
		s.setCookie(c, sessionCookie, s.encode(user.Username), "/admin", int(s.ttl.Seconds()), now.Add(s.ttl))

		c.Set("admin_name", user.Username)
		c.JSON(http.StatusOK, gin.H{
			"name":         user.Username,
			"display_name": user.DisplayName,
			"role":         user.Role,
			"expires_at":   accessExpires.Unix(),
		})
		return
	}

	// 纯内存模式（无 DB），仅下发传统 Session Cookie
	s.setCookie(c, sessionCookie, s.encode(user.Username), "/admin", int(s.ttl.Seconds()), now.Add(s.ttl))
	c.Set("admin_name", user.Username)
	c.JSON(http.StatusOK, gin.H{
		"name":         user.Username,
		"display_name": user.DisplayName,
		"role":         user.Role,
		"expires_at":   now.Add(s.ttl).Unix(),
	})
}

func (s *Sessions) refresh(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.GetHeader("X-Requested-With") != "asu-admin" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 CSRF 防护头"})
		return
	}

	cookie, err := c.Request.Cookie(refreshTokenCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未提供刷新凭据"})
		return
	}

	if s.store == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "数据库会话不可用"})
		return
	}

	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		ip = c.Request.RemoteAddr
	}

	now := s.now()
	user, sessionID, nextRawRefresh, refreshExpires, err := s.store.RotateRefreshToken(c.Request.Context(), cookie.Value, c.Request.UserAgent(), ip, s.refreshTTL, now)
	if err != nil {
		if errors.Is(err, ErrReplayDetected) {
			s.logger.Warn("检测到 Refresh Token 重放攻击，已吊销整族会话", zap.String("ip", ip))
		}
		// 轮换失败，清空所有 Cookie
		s.clearAllCookies(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "刷新凭据无效或已过期"})
		return
	}

	accessToken, accessExpires, err := s.tokens.IssueAccessToken(user.ID, sessionID, user.Username, user.Role, s.accessTTL, now)
	if err != nil {
		s.logger.Error("刷新时签发访问令牌失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发令牌失败"})
		return
	}

	// 重新种下新令牌
	s.setCookie(c, accessTokenCookie, accessToken, "/", int(s.accessTTL.Seconds()), accessExpires)
	s.setCookie(c, refreshTokenCookie, nextRawRefresh, "/admin/api/auth", int(s.refreshTTL.Seconds()), refreshExpires)
	s.setCookie(c, sessionCookie, s.encode(user.Username), "/admin", int(s.ttl.Seconds()), now.Add(s.ttl))

	c.JSON(http.StatusOK, gin.H{
		"name":         user.Username,
		"display_name": user.DisplayName,
		"role":         user.Role,
		"expires_at":   accessExpires.Unix(),
	})
}

func (s *Sessions) logout(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if sessionID := c.GetString("admin_session_id"); sessionID != "" && s.store != nil {
		_ = s.store.RevokeSession(c.Request.Context(), sessionID, "logout", s.now())
	}
	s.clearAllCookies(c)
	c.Status(http.StatusNoContent)
}

func (s *Sessions) me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name":         c.GetString("admin_name"),
		"display_name": c.GetString("admin_display_name"),
		"role":         c.GetString("admin_role"),
	})
}

func (s *Sessions) setCookie(c *gin.Context, name, value, path string, maxAge int, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   c.Request.TLS != nil,
		MaxAge:   maxAge,
		Expires:  expires,
	})
}

func (s *Sessions) clearAllCookies(c *gin.Context) {
	expired := time.Unix(1, 0)
	s.setCookie(c, sessionCookie, "", "/admin", -1, expired)
	if s.store != nil {
		s.setCookie(c, accessTokenCookie, "", "/", -1, expired)
		s.setCookie(c, refreshTokenCookie, "", "/admin/api/auth", -1, expired)
	}
}

func (s *Sessions) authConfig(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	enabled := s.oidc != nil
	buttonText := "使用 SSO 登录"
	if enabled && s.oidcCfg.ButtonText != "" {
		buttonText = s.oidcCfg.ButtonText
	}
	c.JSON(http.StatusOK, gin.H{
		"oidc_enabled":     enabled,
		"oidc_button_text": buttonText,
		"oidc_login_url":   "/admin/api/auth/oidc/login",
	})
}

func (s *Sessions) oidcLogin(c *gin.Context) {
	if s.oidc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "OIDC 未启用"})
		return
	}
	state := GenerateSecureToken()
	nonce := GenerateSecureToken()
	s.setCookie(c, oidcStateCookie, state, "/admin/api/auth/oidc", 300, s.now().Add(5*time.Minute))

	redirectURI := s.oidcCfg.RedirectURL
	if redirectURI == "" {
		scheme := "http"
		if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		redirectURI = fmt.Sprintf("%s://%s/admin/api/auth/oidc/callback", scheme, c.Request.Host)
	}

	authURL, err := s.oidc.BuildAuthURL(c.Request.Context(), state, nonce, redirectURI)
	if err != nil {
		s.logger.Error("构建 OIDC 授权 URL 失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "构建登录链接失败"})
		return
	}
	c.Redirect(http.StatusFound, authURL)
}

func (s *Sessions) oidcCallback(c *gin.Context) {
	if s.oidc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "OIDC 未启用"})
		return
	}

	stateCookie, err := c.Request.Cookie(oidcStateCookie)
	if err != nil || stateCookie.Value == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 state 校验 Cookie"})
		return
	}
	s.setCookie(c, oidcStateCookie, "", "/admin/api/auth/oidc", -1, time.Unix(1, 0))

	queryState := c.Query("state")
	if queryState == "" || subtle.ConstantTimeCompare([]byte(queryState), []byte(stateCookie.Value)) != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state 校验失败"})
		return
	}

	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未提供授权码 code"})
		return
	}

	redirectURI := s.oidcCfg.RedirectURL
	if redirectURI == "" {
		scheme := "http"
		if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		redirectURI = fmt.Sprintf("%s://%s/admin/api/auth/oidc/callback", scheme, c.Request.Host)
	}

	tokenResp, err := s.oidc.ExchangeCode(c.Request.Context(), code, redirectURI)
	if err != nil {
		s.logger.Error("OIDC 换取令牌失败", zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "换取外部身份令牌失败"})
		return
	}

	claims, err := s.oidc.VerifyIDToken(c.Request.Context(), tokenResp.IDToken, s.now())
	if err != nil {
		s.logger.Error("校验 ID Token 失败", zap.Error(err))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "校验外部凭据失败"})
		return
	}

	issuer := claims.Iss
	if issuer == "" {
		issuer = s.oidcCfg.Issuer
	}
	subject := claims.Sub
	if subject == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "外部凭据缺少 sub 标识"})
		return
	}

	preferredName := claims.PreferredUsername
	if preferredName == "" {
		preferredName = claims.Name
	}
	if preferredName == "" {
		preferredName = claims.Email
	}

	displayName := claims.Name
	if displayName == "" {
		displayName = preferredName
	}

	now := s.now()
	var user *User
	if s.store != nil {
		user, err = s.store.ProvisionExternalUser(c.Request.Context(), issuer, subject, preferredName, displayName)
		if err != nil {
			s.logger.Error("绑定 OIDC 用户失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "外部用户初始化失败"})
			return
		}
	} else {
		user = &User{
			ID:          generateUUID(),
			Username:    preferredName,
			DisplayName: displayName,
			Role:        "admin",
		}
	}

	if user.DisabledAt != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "该用户已被禁用"})
		return
	}

	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		ip = c.Request.RemoteAddr
	}

	var sessionID string
	var rawRefresh string
	var refreshExpires time.Time

	if s.store != nil {
		sessionID, rawRefresh, refreshExpires, err = s.store.CreateSession(c.Request.Context(), user.ID, c.Request.UserAgent(), ip, s.refreshTTL, s.refreshAbsoluteTTL, now)
		if err != nil {
			s.logger.Error("创建 OIDC 认证会话失败", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
			return
		}
	} else {
		sessionID = generateUUID()
	}

	accessToken, accessExpires, err := s.tokens.IssueAccessToken(user.ID, sessionID, user.Username, user.Role, s.accessTTL, now)
	if err != nil {
		s.logger.Error("签发访问令牌失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发令牌失败"})
		return
	}

	s.setCookie(c, accessTokenCookie, accessToken, "/", int(s.accessTTL.Seconds()), accessExpires)
	s.setCookie(c, sessionCookie, s.encode(user.Username), "/admin", int(s.ttl.Seconds()), now.Add(s.ttl))
	if rawRefresh != "" {
		s.setCookie(c, refreshTokenCookie, rawRefresh, "/admin/api/auth", int(s.refreshTTL.Seconds()), refreshExpires)
	}

	// 成功登录，跳转回管理后台页面
	c.Redirect(http.StatusFound, "/admin")
}
