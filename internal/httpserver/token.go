package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"hydra-admin/internal/audit"
	"hydra-admin/internal/auth"
	"hydra-admin/internal/hydra"
)

type introspectRequest struct {
	Token string `json:"token"`
}
type revokeRequest struct {
	Token           string `json:"token"`
	ClientID        string `json:"client_id"`
	ClientSecret    string `json:"client_secret"`
	ConfirmClientID string `json:"confirm_client_id"`
}

func (s *Server) tokenAdapter(c *gin.Context, capability string) (hydra.TokenAdapter, bool) {
	report := s.currentHydraReport(c.Request.Context())
	if report.Status == "unsupported_version" {
		if !s.auditRejectedTokenRequest(c) {
			return nil, false
		}
		s.fail(c, 409, "unsupported_version", "Hydra 运行版本不受支持")
		return nil, false
	}
	adapter, ok := s.HydraAdapter.(hydra.TokenAdapter)
	if !ok || !report.BusinessAllowed(capability) {
		if !s.auditRejectedTokenRequest(c) {
			return nil, false
		}
		s.fail(c, 503, "unavailable", "此 Token 能力未开放，请查看系统状态中的原因")
		return nil, false
	}
	return adapter, true
}

func (s *Server) auditRejectedTokenRequest(c *gin.Context) bool {
	if c.Request.Method != http.MethodPost {
		return true
	}
	action := ""
	switch c.Request.URL.Path {
	case "/api/v1/tokens/introspect":
		action = "token.introspect"
	case "/api/v1/tokens/revoke":
		action = "token.revoke"
	default:
		return true
	}
	actor := ""
	if value, exists := c.Get("session"); exists {
		if session, ok := value.(auth.Session); ok {
			actor = session.Username
		}
	}
	if err := s.Audit.Append(audit.Event{Action: action, Outcome: "failure", Actor: actor, SourceIP: s.sourceIP(c), RequestID: c.GetString("request_id"), TargetType: "token"}); err != nil {
		s.Logger.Error("token rejection audit failed", "request_id", c.GetString("request_id"), "error", err)
		s.fail(c, 500, "audit_write_failed", "审计写入失败")
		return false
	}
	return true
}

func (s *Server) auditToken(c *gin.Context, action, outcome, clientID string) bool {
	session := c.MustGet("session").(auth.Session)
	event := audit.Event{Action: action, Outcome: outcome, Actor: session.Username, SourceIP: s.sourceIP(c), RequestID: c.GetString("request_id"), TargetType: "token", TargetID: clientID}
	if err := s.Audit.Append(event); err != nil {
		s.Logger.Error("CRITICAL token audit append failed", "request_id", c.GetString("request_id"), "action", action, "error", err)
		s.fail(c, 500, "audit_write_failed", "Hydra 操作可能已成功但审计写入失败，请按 Client ID 核验")
		return false
	}
	return true
}

func (s *Server) introspectToken(c *gin.Context) {
	adapter, ok := s.tokenAdapter(c, "token_introspect")
	if !ok {
		return
	}
	var input introspectRequest
	if err := decodeWrite(c, &input); err != nil {
		if s.auditToken(c, "token.introspect", "failure", "") {
			s.invalidBody(c, err)
		}
		return
	}
	result, readErr := adapter.IntrospectToken(c.Request.Context(), input.Token)
	input.Token = ""
	if readErr != nil {
		if s.auditToken(c, "token.introspect", "failure", "") {
			s.failToken(c, readErr)
		}
		return
	}
	if !s.auditToken(c, "token.introspect", "success", result.ClientID) {
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) revokeToken(c *gin.Context) {
	adapter, ok := s.tokenAdapter(c, "token_revoke")
	if !ok {
		return
	}
	var input revokeRequest
	if err := decodeWrite(c, &input); err != nil {
		if s.auditToken(c, "token.revoke", "failure", "") {
			s.invalidBody(c, err)
		}
		return
	}
	if input.ClientID == "" || input.ClientID != input.ConfirmClientID || input.Token == "" || input.ClientSecret == "" {
		if s.auditToken(c, "token.revoke", "failure", input.ClientID) {
			s.fail(c, 400, "confirmation_required", "请提供本次 Client 凭据并准确确认 Client ID")
		}
		return
	}
	err := adapter.RevokeToken(c.Request.Context(), input.Token, input.ClientID, input.ClientSecret)
	input.Token = ""
	input.ClientSecret = ""
	if err != nil {
		if s.auditToken(c, "token.revoke", "failure", input.ClientID) {
			s.failToken(c, err)
		}
		return
	}
	if !s.auditToken(c, "token.revoke", "success", input.ClientID) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"accepted": true})
}

func (s *Server) failToken(c *gin.Context, err *hydra.ReadError) {
	message := "Hydra 请求失败；如执行了撤销，请先核验状态，勿直接重复提交"
	switch err.Code {
	case "unsupported_version":
		message = "Hydra 运行版本不受支持"
	case "validation_failed", "hydra_validation_failed":
		message = "Token 请求字段无效"
	case "token_owner_unconfirmed":
		message = "Token 未激活或无法确认属于所填 Client；未发送撤销请求"
	case "client_auth_method_unsupported":
		message = "该 Client 不使用 client_secret_basic，本适配器不支持其撤销认证方式"
	case "revoke_not_configured":
		message = "Public revoke 未配置"
	case "hydra_upstream_denied":
		message = "Hydra 拒绝访问或 Client 认证失败"
	}
	s.fail(c, err.Status, err.Code, message)
}
