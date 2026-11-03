package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"hydra-admin/internal/audit"
	"hydra-admin/internal/hydra"
)

type usageRow struct {
	Value       string   `json:"value"`
	ClientCount int      `json:"client_count"`
	ClientIDs   []string `json:"client_ids"`
}

func (s *Server) scopes(c *gin.Context)    { s.collection(c, "scopes") }
func (s *Server) audiences(c *gin.Context) { s.collection(c, "audiences") }

func (s *Server) collection(c *gin.Context, kind string) {
	query, parseErr := url.ParseQuery(c.Request.URL.RawQuery)
	if parseErr != nil || len(query) > 1 || len(query["page_token"]) > 1 || (len(query) == 1 && (len(query["page_token"]) != 1 || query.Get("page_token") == "")) {
		s.fail(c, http.StatusBadRequest, "invalid_page_token", "分页游标无效")
		return
	}
	if !s.clientReadReady(c, kind) {
		return
	}
	pageAdapter, ok := s.HydraAdapter.(hydra.ClientPageAdapter)
	if !ok || !s.currentHydraReport(c.Request.Context()).BusinessAllowed("pagination") {
		s.fail(c, http.StatusServiceUnavailable, "unavailable", "Client 分页能力不可用")
		return
	}
	const pageSize = 100
	page, err := pageAdapter.ListClientPage(c.Request.Context(), pageSize, query.Get("page_token"))
	if err != nil {
		s.failHydraRead(c, err)
		return
	}
	usage := make(map[string]map[string]struct{})
	for _, client := range page.Items {
		values := collectionValues(client, kind)
		for _, value := range values {
			if usage[value] == nil {
				usage[value] = make(map[string]struct{})
			}
			usage[value][client.ClientID] = struct{}{}
		}
	}
	rows := make([]usageRow, 0, len(usage))
	for value, ids := range usage {
		clientIDs := make([]string, 0, len(ids))
		for id := range ids {
			clientIDs = append(clientIDs, id)
		}
		sort.Strings(clientIDs)
		rows = append(rows, usageRow{Value: value, ClientCount: len(clientIDs), ClientIDs: clientIDs})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Value < rows[j].Value })
	c.JSON(http.StatusOK, gin.H{
		"items": rows, "source_client_count": len(page.Items), "source": "currently_readable_clients",
		"complete": page.NextPageToken == "", "notice": "来自当前可读 Client；读取期间的数据变动可能影响结果",
		"page_size": pageSize, "next_page_token": page.NextPageToken, "pagination_supported": true,
		"capabilities": s.currentHydraReport(c.Request.Context()).Capabilities,
	})
}

func collectionValues(client hydra.ClientRead, kind string) []string {
	if kind == "scopes" {
		var scope string
		if json.Unmarshal(client.Fields["scope"], &scope) == nil {
			return strings.Fields(scope)
		}
		return nil
	}
	var audience []string
	if json.Unmarshal(client.Fields["audience"], &audience) != nil {
		return nil
	}
	values := make([]string, 0, len(audience))
	for _, value := range audience {
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

var auditAction = regexp.MustCompile(`^[A-Za-z0-9._-]{1,80}$`)
var auditCursor = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func auditFilter(raw string) (audit.Query, bool) {
	q := audit.Query{Limit: 50}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return q, false
	}
	for key, value := range values {
		if len(value) != 1 {
			return q, false
		}
		switch key {
		case "from", "to":
			parsed, err := time.Parse(time.RFC3339, value[0])
			if err != nil {
				return q, false
			}
			if key == "from" {
				q.From = parsed
			} else {
				q.To = parsed
			}
		case "action":
			if !auditAction.MatchString(value[0]) {
				return q, false
			}
			q.Action = value[0]
		case "client_id":
			if value[0] == "" || len(value[0]) > 512 || strings.ContainsAny(value[0], "/\\") {
				return q, false
			}
			for _, r := range value[0] {
				if r < 0x20 || r == 0x7f {
					return q, false
				}
			}
			q.ClientID = value[0]
		case "limit", "offset":
			n, err := strconv.Atoi(value[0])
			if err != nil {
				return q, false
			}
			if key == "limit" {
				q.Limit = n
			} else {
				q.Offset = n
			}
		case "cursor":
			if len(value[0]) == 0 || len(value[0]) > 4096 || !auditCursor.MatchString(value[0]) {
				return q, false
			}
			q.Cursor = value[0]
		default:
			return q, false
		}
	}
	if q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || q.Offset > 1000 || (q.Cursor != "" && q.Offset != 0) || (!q.From.IsZero() && !q.To.IsZero() && q.From.After(q.To)) {
		return q, false
	}
	return q, true
}

func (s *Server) auditEvents(c *gin.Context) {
	query, ok := auditFilter(c.Request.URL.RawQuery)
	if !ok {
		s.fail(c, http.StatusBadRequest, "invalid_filter", "审计筛选参数无效")
		return
	}
	page, err := s.auditReader.Query(query)
	if err != nil {
		if errors.Is(err, audit.ErrInvalidCursor) {
			s.fail(c, http.StatusBadRequest, "invalid_cursor", "审计游标已失效，请重新查询")
			return
		}
		s.fail(c, http.StatusInternalServerError, "audit_read_failed", "审计记录读取失败")
		return
	}
	c.JSON(http.StatusOK, page)
}
