package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"hydra-admin/internal/audit"
	"hydra-admin/internal/auth"
)

type passwordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

func (s *Server) appendPasswordAudit(c *gin.Context, outcome string) error {
	actor := ""
	if value, exists := c.Get("session"); exists {
		if session, ok := value.(auth.Session); ok {
			actor = session.Username
		}
	}
	return s.Audit.Append(audit.Event{Action: "password.change", Outcome: outcome, Actor: actor, SourceIP: s.sourceIP(c), RequestID: c.GetString("request_id"), TargetType: "admin"})
}

func (s *Server) auditRejectedPasswordChange(c *gin.Context) bool {
	if c.Request.Method != http.MethodPost || c.Request.URL.Path != "/api/v1/auth/password" {
		return true
	}
	if err := s.appendPasswordAudit(c, "failure"); err != nil {
		s.Logger.Error("audit password change failure failed", "request_id", c.GetString("request_id"), "error", err)
		s.fail(c, 500, "audit_write_failed", "审计写入失败")
		return false
	}
	return true
}

func (s *Server) changePassword(c *gin.Context) {
	var body passwordChangeRequest
	if err := decodeWrite(c, &body); err != nil {
		if s.auditRejectedPasswordChange(c) {
			s.invalidBody(c, err)
		}
		return
	}
	if body.CurrentPassword == "" || len(body.CurrentPassword) > 4096 || len(body.NewPassword) > 4096 || utf8.RuneCountInString(body.NewPassword) < 12 {
		if s.auditRejectedPasswordChange(c) {
			s.fail(c, http.StatusUnprocessableEntity, "validation_failed", "新密码至少 12 个字符，且密码不得超过 4096 字节")
		}
		return
	}
	if body.NewPassword != body.ConfirmPassword {
		if s.auditRejectedPasswordChange(c) {
			s.fail(c, http.StatusUnprocessableEntity, "password_mismatch", "两次输入的新密码不一致")
		}
		return
	}
	actor := c.MustGet("session").(auth.Session).Username
	sourceIP := s.sourceIP(c)
	decision := s.Login.Limiter.Begin(sourceIP, actor, time.Now())
	if !decision.Allowed {
		if !s.auditRejectedPasswordChange(c) {
			return
		}
		if decision.RetryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(int((decision.RetryAfter+time.Second-1)/time.Second)))
		}
		s.fail(c, http.StatusTooManyRequests, "rate_limited", "请求过于频繁，请稍后重试")
		return
	}
	err := s.Login.ChangePassword(body.CurrentPassword, body.NewPassword)
	body = passwordChangeRequest{}
	if err != nil {
		if !s.auditRejectedPasswordChange(c) {
			return
		}
		if errors.Is(err, auth.ErrCurrentPasswordInvalid) {
			s.fail(c, http.StatusForbidden, "current_password_incorrect", "当前密码错误")
			return
		}
		if errors.Is(err, auth.ErrPasswordUnchanged) {
			s.fail(c, http.StatusUnprocessableEntity, "password_unchanged", "新密码不能与当前密码相同")
			return
		}
		s.Logger.Error("password state update failed", "request_id", c.GetString("request_id"), "error", err)
		s.fail(c, 500, "password_update_failed", "密码修改失败")
		return
	}
	s.Login.Limiter.RecordSuccess(sourceIP, actor)
	http.SetCookie(c.Writer, auth.ExpiredCookie(s.Config.Auth.SessionCookieName, s.Config.Security.CookieSecure))
	if err := s.appendPasswordAudit(c, "success"); err != nil {
		s.Logger.Error("audit password change success failed", "request_id", c.GetString("request_id"), "error", err)
		s.fail(c, 500, "password_changed_audit_failed", "密码已修改，但审计写入失败")
		return
	}
	c.Status(http.StatusNoContent)
}
