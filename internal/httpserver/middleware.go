package httpserver

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"hydra-admin/internal/auth"
	"hydra-admin/internal/observability"
)

func (s *Server) bodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := s.Config.Security.RequestBodyMaxBytes
		if c.Request.ContentLength > limit {
			c.Set("body_too_large", true)
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

func (s *Server) requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := randomURLToken(16)
		if err != nil {
			s.fail(c, 500, "internal_error", "服务暂不可用")
			return
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func (s *Server) recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.Logger.Error("request panic recovered", "request_id", c.GetString("request_id"))
				if !c.Writer.Written() {
					s.fail(c, 500, "internal_error", "服务暂不可用")
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

func (s *Server) proxy() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.Config.HTTPProxyOnly() && !auth.TrustedPeer(c.Request.RemoteAddr, s.proxies) {
			s.fail(c, http.StatusForbidden, "untrusted_proxy", "仅允许可信代理访问")
			return
		}
		ip, scheme, err := auth.ResolveClient(c.Request.RemoteAddr, c.Request.TLS != nil, c.GetHeader("X-Forwarded-For"), c.GetHeader("X-Forwarded-Proto"), s.proxies)
		if err != nil {
			s.fail(c, http.StatusBadRequest, "bad_request", "代理头无效")
			return
		}
		c.Set("source_ip", ip.String())
		c.Set("source_scheme", scheme)
		c.Next()
	}
}

func (s *Server) securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		nonce, err := randomURLToken(24)
		if err != nil {
			s.fail(c, 500, "internal_error", "服务暂不可用")
			return
		}
		c.Set("csp_nonce", nonce)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'nonce-"+nonce+"'; style-src-elem 'self' 'nonce-"+nonce+"' 'report-sample'; style-src-attr 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'")
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Header("Cache-Control", "no-store")
		}
		c.Next()
	}
}

func (s *Server) accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		s.Logger.Info("http request", "request_id", c.GetString("request_id"), "method", c.Request.Method,
			"path", observability.SanitizeString(c.Request.URL.RequestURI()), "status", c.Writer.Status(), "source_ip", s.sourceIP(c), "duration_ms", time.Since(start).Milliseconds())
	}
}

func (s *Server) rateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetBool("body_too_large") {
			if !s.auditRejectedClientWrite(c) {
				return
			}
			if !s.auditRejectedTokenRequest(c) {
				return
			}
			s.fail(c, http.StatusRequestEntityTooLarge, "body_too_large", "请求体过大")
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			write := !auth.SafeMethod(c.Request.Method)
			decision := s.apiLimit.Allow(s.sourceIP(c), write, time.Now())
			if !decision.Allowed {
				if !s.auditRejectedClientWrite(c) {
					return
				}
				if !s.auditRejectedTokenRequest(c) {
					return
				}
				if c.Request.URL.Path == "/api/v1/auth/login" && !s.auditLoginFailure(c) {
					return
				}
				c.Header("Retry-After", strconv.Itoa(int((decision.RetryAfter+time.Second-1)/time.Second)))
				s.fail(c, http.StatusTooManyRequests, "rate_limited", "请求过于频繁")
				return
			}
		}
		c.Next()
	}
}

func (s *Server) requireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		session, ok := s.currentSession(c, time.Now())
		if !ok {
			if !s.auditRejectedClientWrite(c) {
				return
			}
			if !s.auditRejectedTokenRequest(c) {
				return
			}
			s.fail(c, http.StatusUnauthorized, "unauthenticated", "请先登录")
			return
		}
		c.Set("session", session)
		c.Next()
	}
}

func (s *Server) requireCSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !auth.SafeMethod(c.Request.Method) {
			if !auth.ValidateOrigin(c.GetHeader("Origin"), c.GetHeader("Referer"), s.Config.Server.PublicBaseURL) {
				if !s.auditRejectedClientWrite(c) {
					return
				}
				if !s.auditRejectedTokenRequest(c) {
					return
				}
				s.fail(c, http.StatusForbidden, "forbidden", "请求来源校验失败")
				return
			}
			v := c.MustGet("session").(auth.Session)
			if !auth.ValidSessionCSRF(v, c.GetHeader(s.Config.Security.CSRFHeaderName)) {
				if !s.auditRejectedClientWrite(c) {
					return
				}
				if !s.auditRejectedTokenRequest(c) {
					return
				}
				s.fail(c, http.StatusForbidden, "forbidden", "CSRF 校验失败")
				return
			}
		}
		c.Next()
	}
}

func (s *Server) auditReady() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := s.Audit.Ready(); err != nil {
			s.Logger.Error("audit unavailable", "error", err)
			s.fail(c, 500, "audit_write_failed", "审计写入失败")
			return
		}
		c.Next()
	}
}
