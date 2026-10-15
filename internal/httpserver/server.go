package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"hydra-admin/internal/audit"
	"hydra-admin/internal/auth"
	"hydra-admin/internal/config"
	"hydra-admin/internal/hydra"
)

type Server struct {
	Config       *config.Config
	Engine       *gin.Engine
	Sessions     *auth.SessionStore
	PreAuth      *auth.PreAuthStore
	Login        *auth.LoginService
	Audit        auditSink
	auditReader  *audit.Recorder
	Logger       *slog.Logger
	HydraReport  hydra.Report
	HydraAdapter hydra.HydraAdapter
	web          fs.FS
	index        []byte
	apiLimit     *auth.IPLimiter
	proxies      []*net.IPNet
}

type auditSink interface {
	Ready() error
	Append(audit.Event) error
}

var hashedAssetName = regexp.MustCompile(`-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`)

func New(cfg *config.Config, web fs.FS, recorder *audit.Recorder, logger *slog.Logger) (*Server, error) {
	if cfg == nil || web == nil || recorder == nil || logger == nil {
		return nil, errors.New("server dependencies are required")
	}
	index, err := fs.ReadFile(web, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read embedded web index: %w", err)
	}
	if !strings.Contains(string(index), "__CSP_NONCE__") {
		return nil, errors.New("web index lacks CSP nonce placeholder")
	}
	sessions, err := auth.NewSessionStore(cfg.Auth.MaxSessions, cfg.Auth.SessionTTL.Value())
	if err != nil {
		return nil, err
	}
	proxies, err := auth.ParseTrustedCIDRs(cfg.Server.TrustedProxyCIDRs)
	if err != nil {
		return nil, err
	}
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	if err := engine.SetTrustedProxies(nil); err != nil {
		return nil, err
	}
	s := &Server{Config: cfg, Engine: engine, Sessions: sessions, PreAuth: auth.NewPreAuthStore(), Audit: recorder, auditReader: recorder, Logger: logger, web: web, index: index, proxies: proxies,
		apiLimit: auth.NewIPLimiter(cfg.Security.APIRateLimit.ReadPerMinutePerIP, cfg.Security.APIRateLimit.WritePerMinutePerIP)}
	s.Login = &auth.LoginService{Username: cfg.Auth.Username, PasswordHash: cfg.Auth.PasswordHash, PublicBaseURL: cfg.Server.PublicBaseURL, Sessions: sessions, PreAuth: s.PreAuth,
		Limiter: auth.NewLoginLimiter(cfg.Auth.Login.Window.Value(), cfg.Auth.Login.MaxAttemptsPerIP, cfg.Auth.Login.MaxAttemptsPerUsername, cfg.Auth.Login.LockoutDuration.Value())}
	// Request-wide middleware runs before route-specific authentication, CSRF and audit guards.
	engine.Use(s.bodyLimit(), s.requestID(), s.recovery(), s.proxy(), s.securityHeaders(), s.accessLog(), s.rateLimit())
	engine.GET("/healthz", s.health)
	api := engine.Group("/api/v1/auth")
	api.GET("/csrf", s.csrf)
	api.POST("/login", s.login)
	protected := api.Group("")
	protected.Use(s.requireSession())
	protected.GET("/session", s.session)
	protected.POST("/logout", s.requireCSRF(), s.auditReady(), s.logout)
	system := engine.Group("/api/v1/system")
	system.Use(s.requireSession())
	system.GET("/status", s.systemStatus)
	clients := engine.Group("/api/v1/clients")
	clients.Use(s.requireSession())
	clients.GET("", s.listClients)
	clients.GET("/:clientId", s.getClient)
	clients.POST("", s.requireCSRF(), s.auditReady(), s.createClient)
	clients.PATCH("/:clientId", s.requireCSRF(), s.auditReady(), s.patchClient)
	clients.DELETE("/:clientId", s.requireCSRF(), s.auditReady(), s.deleteClient)
	clients.POST("/:clientId/secret/rotate", s.requireCSRF(), s.auditReady(), s.secretRotationUnavailable)
	tokens := engine.Group("/api/v1/tokens")
	tokens.Use(s.requireSession())
	tokens.GET("/introspect", s.businessUnavailable())
	tokens.GET("/revoke", s.businessUnavailable())
	tokens.POST("/introspect", s.requireCSRF(), s.auditReady(), s.introspectToken)
	tokens.POST("/revoke", s.requireCSRF(), s.auditReady(), s.revokeToken)
	collections := engine.Group("/api/v1")
	collections.Use(s.requireSession())
	collections.GET("/scopes", s.scopes)
	collections.GET("/audiences", s.audiences)
	collections.GET("/audit", s.auditEvents)
	engine.NoRoute(s.staticOrNotFound)
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.Engine }
func (s *Server) StartCleanup(ctx context.Context) <-chan struct{} {
	return auth.StartCleanup(ctx, s.Sessions, s.PreAuth)
}

func (s *Server) health(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) }

func (s *Server) currentHydraReport(ctx context.Context) hydra.Report {
	if reporter, ok := s.HydraAdapter.(interface {
		Report(context.Context) hydra.Report
	}); ok {
		return reporter.Report(ctx)
	}
	return s.HydraReport
}

func (s *Server) systemStatus(c *gin.Context) {
	c.JSON(http.StatusOK, s.currentHydraReport(c.Request.Context()))
}

func (s *Server) businessUnavailable() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.currentHydraReport(c.Request.Context()).Status == "unsupported_version" {
			s.fail(c, http.StatusConflict, "unsupported_version", "Hydra 运行版本不受支持")
			return
		}
		s.fail(c, http.StatusServiceUnavailable, "unavailable", "业务操作尚未实现")
	}
}

func (s *Server) secretRotationUnavailable(c *gin.Context) {
	if !s.auditClient(c, "client.rotate", "failure", c.Param("clientId"), nil) {
		return
	}
	if s.currentHydraReport(c.Request.Context()).Status == "unsupported_version" {
		s.fail(c, http.StatusConflict, "unsupported_version", "Hydra 运行版本不受支持")
		return
	}
	s.fail(c, http.StatusServiceUnavailable, "rotation_operation_absent", "固定版本规范没有独立 Secret 轮换操作")
}

func (s *Server) clientReadReady(c *gin.Context, capability string) bool {
	report := s.currentHydraReport(c.Request.Context())
	if report.Status == "unsupported_version" {
		s.fail(c, http.StatusConflict, "unsupported_version", "Hydra 运行版本不受支持")
		return false
	}
	if s.HydraAdapter == nil || !report.BusinessAllowed(capability) {
		s.fail(c, http.StatusServiceUnavailable, "unavailable", "Client 只读能力不可用")
		return false
	}
	return true
}

func (s *Server) listClients(c *gin.Context) {
	if !s.clientReadReady(c, "client_list") {
		return
	}
	if !s.currentHydraReport(c.Request.Context()).BusinessAllowed("pagination") {
		s.fail(c, http.StatusServiceUnavailable, "unavailable", "Client 分页能力不可用")
		return
	}
	query, parseErr := url.ParseQuery(c.Request.URL.RawQuery)
	if parseErr != nil || len(query) > 1 || len(query["page_token"]) > 1 || (len(query) == 1 && len(query["page_token"]) != 1) {
		s.fail(c, http.StatusBadRequest, "invalid_page_token", "分页游标无效")
		return
	}
	pageToken := query.Get("page_token")
	if len(query) == 1 && pageToken == "" {
		s.fail(c, http.StatusBadRequest, "invalid_page_token", "分页游标无效")
		return
	}
	pageAdapter, ok := s.HydraAdapter.(hydra.ClientPageAdapter)
	if !ok {
		s.fail(c, http.StatusServiceUnavailable, "unavailable", "Client 分页能力不可用")
		return
	}
	const pageSize = 10
	page, err := pageAdapter.ListClientPage(c.Request.Context(), pageSize, pageToken)
	if err != nil {
		s.failHydraRead(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": page.Items, "page_size": pageSize, "next_page_token": page.NextPageToken, "pagination_supported": true,
		"capabilities": s.currentHydraReport(c.Request.Context()).Capabilities, "writable_schema": hydra.WritableSchema,
		"resource_catalog": s.Config.ResourceCatalog})
}

func (s *Server) getClient(c *gin.Context) {
	if !s.clientReadReady(c, "client_detail") {
		return
	}
	client, err := s.HydraAdapter.GetClient(c.Request.Context(), c.Param("clientId"))
	if err != nil {
		s.failHydraRead(c, err)
		return
	}
	editable := []string{}
	report := s.currentHydraReport(c.Request.Context())
	if report.BusinessAllowed("client_update") {
		editable = hydra.WritableNames(false)
	}
	c.JSON(http.StatusOK, gin.H{"client": client, "revision": client.Revision(), "editable_fields": editable, "writable_schema": hydra.WritableSchema,
		"capabilities": report.Capabilities, "resource_catalog": s.Config.ResourceCatalog})
}

func (s *Server) failHydraRead(c *gin.Context, err *hydra.ReadError) {
	message := "Hydra 只读请求失败"
	switch err.Code {
	case "unsupported_version":
		message = "Hydra 运行版本不受支持"
	case "hydra_timeout":
		message = "Hydra 请求超时"
	case "hydra_conflict":
		message = "Hydra 返回冲突"
	case "hydra_upstream_denied":
		message = "Hydra 上游拒绝访问"
	case "client_not_found":
		message = "Client 不存在"
	case "invalid_client_id":
		message = "Client ID 无效"
	case "invalid_page_token":
		message = "分页游标无效"
	}
	s.fail(c, err.Status, err.Code, message)
}

func (s *Server) csrf(c *gin.Context) {
	now := time.Now()
	if session, ok := s.currentSession(c, now); ok {
		c.JSON(http.StatusOK, gin.H{"csrf_token": session.CSRFToken, "authenticated": true})
		return
	}
	cookie, nonce, err := s.PreAuth.Issue(now)
	if err != nil {
		s.fail(c, http.StatusInternalServerError, "internal_error", "服务暂不可用")
		return
	}
	http.SetCookie(c.Writer, auth.PreAuthCookie(cookie, s.Config.Security.CookieSecure, now))
	c.JSON(http.StatusOK, gin.H{"csrf_token": nonce, "authenticated": false})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) login(c *gin.Context) {
	if err := s.Audit.Ready(); err != nil {
		s.Logger.Error("audit unavailable", "error", err)
		s.fail(c, 500, "audit_write_failed", "审计写入失败")
		return
	}
	var body loginRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		if !s.auditLoginFailure(c) {
			return
		}
		s.invalidBody(c, err)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if !s.auditLoginFailure(c) {
			return
		}
		s.fail(c, http.StatusBadRequest, "bad_request", "请求参数无效")
		return
	}
	if body.Username == "" || body.Password == "" || len(body.Username) > 256 || len(body.Password) > 4096 {
		if !s.auditLoginFailure(c) {
			return
		}
		s.fail(c, http.StatusUnprocessableEntity, "validation_failed", "用户名或密码格式无效")
		return
	}
	preCookie, _ := c.Request.Cookie(auth.PreAuthCookieName)
	oldCookie, _ := c.Request.Cookie(s.Config.Auth.SessionCookieName)
	input := auth.LoginInput{Username: body.Username, Password: body.Password, SourceIP: s.sourceIP(c), Origin: c.GetHeader("Origin"), Referer: c.GetHeader("Referer"), PreAuthNonce: c.GetHeader(s.Config.Security.CSRFHeaderName)}
	if preCookie != nil {
		input.PreAuthCookie = preCookie.Value
	}
	if oldCookie != nil {
		input.OldSessionID = oldCookie.Value
	}
	result := s.Login.Attempt(input, time.Now())
	body.Password = ""
	if result.RetryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(int((result.RetryAfter+time.Second-1)/time.Second)))
	}
	if result.Status != http.StatusOK {
		if !s.auditLoginFailure(c) {
			return
		}
		s.fail(c, result.Status, loginCode(result.Status), result.Message)
		return
	}
	if err := s.Audit.Append(audit.Event{Action: "login", Outcome: "success", Actor: s.Config.Auth.Username, SourceIP: input.SourceIP}); err != nil {
		s.Sessions.Delete(result.Session.ID)
		s.Logger.Error("audit login success failed", "error", err)
		s.fail(c, 500, "audit_write_failed", "审计写入失败")
		return
	}
	http.SetCookie(c.Writer, auth.ExpiredCookie(auth.PreAuthCookieName, s.Config.Security.CookieSecure))
	http.SetCookie(c.Writer, auth.SessionCookie(s.Config.Auth.SessionCookieName, result.Session, s.Config.Security.CookieSecure, s.Config.Auth.SessionTTL.Value()))
	c.JSON(http.StatusOK, gin.H{"username": result.Session.Username, "expires_at": result.Session.ExpiresAt})
}

func (s *Server) auditLoginFailure(c *gin.Context) bool {
	if err := s.Audit.Append(audit.Event{Action: "login", Outcome: "failure", SourceIP: s.sourceIP(c)}); err != nil {
		s.Logger.Error("audit login failure failed", "error", err)
		s.fail(c, 500, "audit_write_failed", "审计写入失败")
		return false
	}
	return true
}

func loginCode(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "unauthenticated"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "internal_error"
	}
}

func (s *Server) session(c *gin.Context) {
	v := c.MustGet("session").(auth.Session)
	c.JSON(http.StatusOK, gin.H{"username": v.Username, "expires_at": v.ExpiresAt})
}

func (s *Server) logout(c *gin.Context) {
	v := c.MustGet("session").(auth.Session)
	if err := s.Audit.Append(audit.Event{Action: "logout", Outcome: "success", Actor: v.Username, SourceIP: s.sourceIP(c)}); err != nil {
		s.Logger.Error("audit logout failed", "error", err)
		s.fail(c, 500, "audit_write_failed", "审计写入失败")
		return
	}
	s.Sessions.Delete(v.ID)
	http.SetCookie(c.Writer, auth.ExpiredCookie(s.Config.Auth.SessionCookieName, s.Config.Security.CookieSecure))
	c.Status(http.StatusNoContent)
}

func (s *Server) invalidBody(c *gin.Context, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		s.fail(c, http.StatusRequestEntityTooLarge, "body_too_large", "请求体过大")
		return
	}
	s.fail(c, http.StatusBadRequest, "bad_request", "请求参数无效")
}

func (s *Server) fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"code": code, "message": message, "request_id": c.GetString("request_id")})
}

func (s *Server) currentSession(c *gin.Context, now time.Time) (auth.Session, bool) {
	cookie, err := c.Request.Cookie(s.Config.Auth.SessionCookieName)
	if err != nil {
		return auth.Session{}, false
	}
	return s.Sessions.Get(cookie.Value, now)
}

func (s *Server) sourceIP(c *gin.Context) string { return c.GetString("source_ip") }

func (s *Server) staticOrNotFound(c *gin.Context) {
	urlPath := c.Request.URL.Path
	if strings.HasPrefix(urlPath, "/api/") || urlPath == "/api" {
		s.fail(c, http.StatusNotFound, "not_found", "接口不存在")
		return
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		s.fail(c, http.StatusNotFound, "not_found", "资源不存在")
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if name != "" && name != "index.html" {
		if info, err := fs.Stat(s.web, name); err == nil && info.Mode().IsRegular() {
			data, err := fs.ReadFile(s.web, name)
			if err != nil {
				s.fail(c, 500, "internal_error", "服务暂不可用")
				return
			}
			if strings.HasPrefix(name, "assets/") && hashedAssetName.MatchString(path.Base(name)) {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
			if kind := mime.TypeByExtension(path.Ext(name)); kind != "" {
				c.Header("Content-Type", kind)
			}
			c.Data(http.StatusOK, c.Writer.Header().Get("Content-Type"), data)
			return
		}
	}
	if strings.Contains(path.Base(urlPath), ".") && name != "index.html" {
		s.fail(c, 404, "not_found", "资源不存在")
		return
	}
	nonce := c.GetString("csp_nonce")
	html := bytesReplaceNonce(s.index, nonce)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
}

func bytesReplaceNonce(index []byte, nonce string) []byte {
	return []byte(strings.ReplaceAll(string(index), "__CSP_NONCE__", nonce))
}

func randomURLToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
