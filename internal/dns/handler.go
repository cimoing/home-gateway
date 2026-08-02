package dns

import (
	"errors"
	"net/http"
	"strings"

	"home-gateway/internal/cloudflare"

	"github.com/gin-gonic/gin"
)

// Handler exposes the protected DNS management API.
type Handler struct {
	service *Service
}

// NewHandler creates a DNS API handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Register adds DNS management routes to an authenticated API group.
func (h *Handler) Register(api *gin.RouterGroup) {
	group := api.Group("/dns")
	group.GET("/zones", h.listZones)
	group.POST("/zones/:zoneName/sync", h.syncZone)
	group.GET("/zones/:zoneName/records", h.listRecords)
	group.POST("/zones/:zoneName/records", h.createRecord)
	group.PUT("/zones/:zoneName/records/:recordID", h.updateRecord)
	group.DELETE("/zones/:zoneName/records/:recordID", h.deleteRecord)
}

func (h *Handler) listZones(c *gin.Context) {
	zones, err := h.service.ListZones(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"zones": zones})
}

func (h *Handler) syncZone(c *gin.Context) {
	zoneName, ok := zoneNameParam(c)
	if !ok {
		return
	}
	records, err := h.service.RefreshZone(c.Request.Context(), zoneName)
	if err != nil {
		writeError(c, err)
		return
	}
	zones, err := h.service.ListZones(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	var zone any
	for _, item := range zones {
		if strings.EqualFold(item.Name, zoneName) {
			zone = item
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{"zone": zone, "records": records})
}

func (h *Handler) listRecords(c *gin.Context) {
	zoneName, ok := zoneNameParam(c)
	if !ok {
		return
	}
	records, err := h.service.ListRecords(c.Request.Context(), zoneName)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"records": records})
}

func (h *Handler) createRecord(c *gin.Context) {
	zoneName, ok := zoneNameParam(c)
	if !ok {
		return
	}
	var input cloudflare.RecordInput
	if !bindJSON(c, &input) {
		return
	}
	record, err := h.service.CreateRecord(c.Request.Context(), zoneName, input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"record": record})
}

func (h *Handler) updateRecord(c *gin.Context) {
	zoneName, ok := zoneNameParam(c)
	if !ok {
		return
	}
	recordID := strings.TrimSpace(c.Param("recordID"))
	if recordID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid record ID"})
		return
	}
	var input cloudflare.RecordInput
	if !bindJSON(c, &input) {
		return
	}
	record, err := h.service.UpdateRecord(c.Request.Context(), zoneName, recordID, input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"record": record})
}

func (h *Handler) deleteRecord(c *gin.Context) {
	zoneName, ok := zoneNameParam(c)
	if !ok {
		return
	}
	recordID := strings.TrimSpace(c.Param("recordID"))
	if recordID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid record ID"})
		return
	}
	if err := h.service.DeleteRecord(c.Request.Context(), zoneName, recordID); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func zoneNameParam(c *gin.Context) (string, bool) {
	name := strings.TrimSpace(c.Param("zoneName"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid zone name"})
		return "", false
	}
	return name, true
}

func bindJSON(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	if err := c.ShouldBindJSON(target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return false
	}
	return true
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": ErrNotFound.Error()})
	case errors.Is(err, ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": ErrConflict.Error()})
	case errors.Is(err, ErrNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": ErrNotConfigured.Error()})
	case cloudflare.IsStatus(err, http.StatusTooManyRequests):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Cloudflare rate limit exceeded"})
	case errors.Is(err, ErrProvider):
		c.JSON(http.StatusBadGateway, gin.H{"error": ErrProvider.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "DNS operation failed"})
	}
}
