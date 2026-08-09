package admin

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type proxyGroupRequest struct {
	Name string `json:"name" binding:"required"`
}

func (h *ProxyHandler) ListProxyGroups(c *gin.Context) {
	groups, err := h.proxyGroupService.ListProxyGroups(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.ProxyGroup, 0, len(groups))
	for i := range groups {
		out = append(out, *dto.ProxyGroupFromService(&groups[i]))
	}
	response.Success(c, out)
}

func (h *ProxyHandler) CreateProxyGroup(c *gin.Context) {
	var req proxyGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	group, err := h.proxyGroupService.CreateProxyGroup(c.Request.Context(), req.Name)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ProxyGroupFromService(group))
}

func (h *ProxyHandler) UpdateProxyGroup(c *gin.Context) {
	id, err := parsePositiveProxyGroupID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	var req proxyGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	group, err := h.proxyGroupService.UpdateProxyGroup(c.Request.Context(), id, req.Name)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ProxyGroupFromService(group))
}

func (h *ProxyHandler) DeleteProxyGroup(c *gin.Context) {
	id, err := parsePositiveProxyGroupID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := h.proxyGroupService.DeleteProxyGroup(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Proxy group deleted successfully"})
}

func parsePositiveProxyGroupID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid proxy group ID")
	}
	return id, nil
}

func parseProxyListFilters(c *gin.Context) (service.ProxyListFilters, error) {
	filters := service.ProxyListFilters{
		Protocol: strings.TrimSpace(c.Query("protocol")),
		Status:   strings.TrimSpace(c.Query("status")),
		Search:   strings.TrimSpace(c.Query("search")),
	}
	searchRunes := []rune(filters.Search)
	if len(searchRunes) > 100 {
		filters.Search = string(searchRunes[:100])
	}

	groupValue := strings.TrimSpace(c.Query("group_id"))
	if groupValue != "" {
		id, err := parsePositiveProxyGroupID(groupValue)
		if err != nil {
			return filters, err
		}
		filters.ProxyGroupID = &id
	}
	if raw, ok := c.GetQuery("ungrouped"); ok {
		ungrouped, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return filters, fmt.Errorf("invalid ungrouped value")
		}
		filters.Ungrouped = ungrouped
	}
	if filters.ProxyGroupID != nil && filters.Ungrouped {
		return filters, fmt.Errorf("group_id and ungrouped cannot be used together")
	}
	return filters, nil
}
