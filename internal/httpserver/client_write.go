package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"hydra-admin/internal/audit"
	"hydra-admin/internal/auth"
	"hydra-admin/internal/hydra"
)

type createClientRequest struct {
	Fields map[string]json.RawMessage `json:"fields"`
}
type patchClientRequest struct {
	BaseRevision string                     `json:"base_revision"`
	Changes      map[string]json.RawMessage `json:"changes"`
}

func clientWriteAction(method, path string) (string, string) {
	const prefix = "/api/v1/clients"
	if method == http.MethodPost && path == prefix {
		return "client.create", ""
	}
	if !strings.HasPrefix(path, prefix+"/") {
		return "", ""
	}
	id := strings.TrimPrefix(path, prefix+"/")
	if id == "" || strings.Contains(id, "/") {
		return "", ""
	}
	switch method {
	case http.MethodPatch:
		return "client.update", id
	case http.MethodDelete:
		return "client.delete", id
	}
	return "", ""
}

// Early auth/CSRF/rate failures still produce a failure audit record, with no
// request body, header, or credential attached.
func (s *Server) auditRejectedClientWrite(c *gin.Context) bool {
	action, id := clientWriteAction(c.Request.Method, c.Request.URL.Path)
	if action == "" {
		return true
	}
	actor := ""
	if value, exists := c.Get("session"); exists {
		if session, ok := value.(auth.Session); ok {
			actor = session.Username
		}
	}
	if err := s.Audit.Append(audit.Event{Action: action, Outcome: "failure", Actor: actor, SourceIP: s.sourceIP(c), RequestID: c.GetString("request_id"), TargetType: "client", TargetID: id}); err != nil {
		s.Logger.Error("client rejection audit failed", "request_id", c.GetString("request_id"), "error", err)
		s.fail(c, 500, "audit_write_failed", "审计写入失败")
		return false
	}
	return true
}

func decodeWrite(c *gin.Context, target any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (s *Server) clientWriter(c *gin.Context, capability string) (hydra.ClientWriteAdapter, bool) {
	report := s.currentHydraReport(c.Request.Context())
	if report.Status == "unsupported_version" {
		if !s.auditRejectedClientWrite(c) {
			return nil, false
		}
		s.fail(c, 409, "unsupported_version", "Hydra 运行版本不受支持")
		return nil, false
	}
	writer, ok := s.HydraAdapter.(hydra.ClientWriteAdapter)
	if !ok || !report.BusinessAllowed(capability) {
		if !s.auditRejectedClientWrite(c) {
			return nil, false
		}
		s.fail(c, 503, "unavailable", "Client 写入能力不可用")
		return nil, false
	}
	return writer, true
}

func (s *Server) auditClient(c *gin.Context, action, outcome, id string, fields []string) bool {
	session := c.MustGet("session").(auth.Session)
	event := audit.Event{Action: action, Outcome: outcome, Actor: session.Username, SourceIP: s.sourceIP(c), RequestID: c.GetString("request_id"), TargetType: "client", TargetID: id, ChangedFields: fields}
	if err := s.Audit.Append(event); err != nil {
		// The upstream state may already have changed. Never retry or roll back.
		s.Logger.Error("CRITICAL client audit append failed after Hydra operation", "request_id", c.GetString("request_id"), "action", action, "client_id", id, "error", err)
		s.fail(c, 500, "audit_write_failed", "Hydra 操作可能已成功但审计写入失败，请按 Client ID 核验")
		return false
	}
	return true
}

func fieldNames(fields map[string]json.RawMessage) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *Server) createClient(c *gin.Context) {
	w, ok := s.clientWriter(c, "client_create")
	if !ok {
		return
	}
	var input createClientRequest
	if err := decodeWrite(c, &input); err != nil {
		if s.auditClient(c, "client.create", "failure", "", nil) {
			s.invalidBody(c, err)
		}
		return
	}
	var id string
	_ = json.Unmarshal(input.Fields["client_id"], &id)
	fields := fieldNames(input.Fields)
	if err := hydra.ValidateFields(input.Fields, true); err != nil {
		if s.auditClient(c, "client.create", "failure", id, fields) {
			s.fail(c, err.Status, err.Code, "Client 字段校验失败")
		}
		return
	}
	if err := validateResourceAssignment(s.Config.ResourceCatalog, input.Fields, nil); err != nil {
		if s.auditClient(c, "client.create", "failure", id, fields) {
			s.fail(c, http.StatusUnprocessableEntity, "resource_assignment_invalid", "Scope 或 Audience 不符合资源目录")
		}
		return
	}
	result, err := w.CreateClient(c.Request.Context(), input.Fields)
	if err != nil {
		if s.auditClient(c, "client.create", "failure", id, fields) {
			s.failHydraWrite(c, err)
		}
		return
	}
	if !s.auditClient(c, "client.create", "success", result.Client.ClientID, fields) {
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (s *Server) patchClient(c *gin.Context) {
	w, ok := s.clientWriter(c, "client_update")
	if !ok {
		return
	}
	id := c.Param("clientId")
	var input patchClientRequest
	if err := decodeWrite(c, &input); err != nil {
		if s.auditClient(c, "client.update", "failure", id, nil) {
			s.invalidBody(c, err)
		}
		return
	}
	fields := fieldNames(input.Changes)
	if err := hydra.ValidateFields(input.Changes, false); err != nil {
		if s.auditClient(c, "client.update", "failure", id, fields) {
			s.fail(c, err.Status, err.Code, "Client 字段校验失败")
		}
		return
	}
	if s.Config.ResourceCatalog.Enabled() && (input.Changes["scope"] != nil || input.Changes["audience"] != nil) {
		current, readErr := s.HydraAdapter.GetClient(c.Request.Context(), id)
		if readErr != nil {
			if s.auditClient(c, "client.update", "failure", id, fields) {
				s.failHydraRead(c, readErr)
			}
			return
		}
		if current.Revision() != input.BaseRevision {
			if s.auditClient(c, "client.update", "failure", id, fields) {
				s.fail(c, http.StatusConflict, "client_changed", "Client 已变化，请刷新后重试")
			}
			return
		}
		if err := validateResourceAssignment(s.Config.ResourceCatalog, input.Changes, current.Fields); err != nil {
			if s.auditClient(c, "client.update", "failure", id, fields) {
				s.fail(c, http.StatusUnprocessableEntity, "resource_assignment_invalid", "Scope 或 Audience 不符合资源目录")
			}
			return
		}
	}
	client, err := w.PatchClient(c.Request.Context(), id, input.BaseRevision, input.Changes)
	if err != nil {
		if s.auditClient(c, "client.update", "failure", id, fields) {
			s.failHydraWrite(c, err)
		}
		return
	}
	if !s.auditClient(c, "client.update", "success", id, fields) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"client": client, "revision": client.Revision()})
}

func (s *Server) deleteClient(c *gin.Context) {
	w, ok := s.clientWriter(c, "client_delete")
	if !ok {
		return
	}
	id := c.Param("clientId")
	if c.GetHeader("X-Confirm-Client-ID") != id || id == "" {
		if s.auditClient(c, "client.delete", "failure", id, nil) {
			s.fail(c, 400, "confirmation_required", "请准确输入 Client ID 确认删除")
		}
		return
	}
	if err := w.DeleteClient(c.Request.Context(), id); err != nil {
		if s.auditClient(c, "client.delete", "failure", id, nil) {
			s.failHydraWrite(c, err)
		}
		return
	}
	if !s.auditClient(c, "client.delete", "success", id, nil) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) failHydraWrite(c *gin.Context, err *hydra.ReadError) {
	message := "Hydra 写入失败；请按 Client ID 核验状态后再决定下一步"
	switch err.Code {
	case "unsupported_version":
		message = "Hydra 运行版本不受支持"
	case "client_changed":
		message = "Client 已变化，请刷新后重新检查修改内容"
	case "client_not_found":
		message = "Client 不存在"
	case "hydra_conflict":
		message = "Client 冲突，请核验当前状态"
	case "hydra_validation_failed":
		message = "Hydra 拒绝了 Client 字段，请检查输入"
	case "invalid_client_id", "bad_request", "validation_failed", "no_changes":
		message = "Client 请求无效"
	}
	s.fail(c, err.Status, err.Code, message)
}
