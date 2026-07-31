package dns

import (
	"errors"
	"net/http"
	"strconv"

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
	group.GET("/credentials", h.listCredentials)
	group.POST("/credentials", h.createCredential)
	group.PUT("/credentials/:credentialID", h.updateCredential)
	group.DELETE("/credentials/:credentialID", h.deleteCredential)

	group.GET("/zones", h.listZones)
	group.POST("/zones", h.createZone)
	group.DELETE("/zones/:zoneID", h.deleteZone)
	group.POST("/zones/:zoneID/sync", h.syncZone)

	group.GET("/zones/:zoneID/records", h.listRecords)
	group.POST("/zones/:zoneID/records", h.createRecord)
	group.PUT("/zones/:zoneID/records/:recordID", h.updateRecord)
	group.DELETE("/zones/:zoneID/records/:recordID", h.deleteRecord)
}

func (h *Handler) listCredentials(c *gin.Context) {
	items, err := h.service.ListCredentials(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"credentials": items})
}

func (h *Handler) createCredential(c *gin.Context) {
	var request struct {
		Name  string `json:"name" binding:"required"`
		Token string `json:"token" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	item, err := h.service.CreateCredential(c.Request.Context(), request.Name, request.Token)
	request.Token = ""
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"credential": item})
}

func (h *Handler) updateCredential(c *gin.Context) {
	id, ok := pathID(c, "credentialID")
	if !ok {
		return
	}
	var request struct {
		Token string `json:"token" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	item, err := h.service.UpdateCredential(c.Request.Context(), id, request.Token)
	request.Token = ""
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"credential": item})
}

func (h *Handler) deleteCredential(c *gin.Context) {
	id, ok := pathID(c, "credentialID")
	if !ok {
		return
	}
	if err := h.service.DeleteCredential(c.Request.Context(), id); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) listZones(c *gin.Context) {
	zones, err := h.service.ListZones(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"zones": zones})
}

func (h *Handler) createZone(c *gin.Context) {
	var request struct {
		CredentialID int64  `json:"credentialId" binding:"required"`
		Name         string `json:"name" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	zone, err := h.service.CreateZone(
		c.Request.Context(),
		request.CredentialID,
		request.Name,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"zone": zone})
}

func (h *Handler) deleteZone(c *gin.Context) {
	zoneID, ok := pathID(c, "zoneID")
	if !ok {
		return
	}
	if err := h.service.DeleteZone(c.Request.Context(), zoneID); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) syncZone(c *gin.Context) {
	zoneID, ok := pathID(c, "zoneID")
	if !ok {
		return
	}
	records, err := h.service.SyncZone(c.Request.Context(), zoneID)
	if err != nil {
		writeError(c, err)
		return
	}
	zone, err := h.service.zoneByID(c.Request.Context(), zoneID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"zone": zone, "records": records})
}

func (h *Handler) listRecords(c *gin.Context) {
	zoneID, ok := pathID(c, "zoneID")
	if !ok {
		return
	}
	records, err := h.service.ListRecords(c.Request.Context(), zoneID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"records": records})
}

func (h *Handler) createRecord(c *gin.Context) {
	zoneID, ok := pathID(c, "zoneID")
	if !ok {
		return
	}
	var input cloudflare.RecordInput
	if !bindJSON(c, &input) {
		return
	}
	record, err := h.service.CreateRecord(c.Request.Context(), zoneID, input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"record": record})
}

func (h *Handler) updateRecord(c *gin.Context) {
	zoneID, ok := pathID(c, "zoneID")
	if !ok {
		return
	}
	recordID, ok := pathID(c, "recordID")
	if !ok {
		return
	}
	var input cloudflare.RecordInput
	if !bindJSON(c, &input) {
		return
	}
	record, err := h.service.UpdateRecord(c.Request.Context(), zoneID, recordID, input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"record": record})
}

func (h *Handler) deleteRecord(c *gin.Context) {
	zoneID, ok := pathID(c, "zoneID")
	if !ok {
		return
	}
	recordID, ok := pathID(c, "recordID")
	if !ok {
		return
	}
	if err := h.service.DeleteRecord(c.Request.Context(), zoneID, recordID); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func bindJSON(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	if err := c.ShouldBindJSON(target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return false
	}
	return true
}

func pathID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource ID"})
		return 0, false
	}
	return id, true
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
