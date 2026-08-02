package storage

import (
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"

	"home-gateway/internal/credential"

	"github.com/gin-gonic/gin"
)

const maxUploadBytes = 100 << 20

// Handler exposes authenticated storage APIs.
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Register(api *gin.RouterGroup) {
	group := api.Group("/storage")
	group.GET("/backends", h.listBackends)
	group.POST("/backends", h.createBackend)
	group.POST("/backends/test", h.testDraft)
	group.GET("/backends/:backendID", h.getBackend)
	group.PUT("/backends/:backendID", h.updateBackend)
	group.DELETE("/backends/:backendID", h.deleteBackend)
	group.POST("/backends/:backendID/test", h.testBackend)
	group.GET("/backends/:backendID/entries", h.listEntries)
	group.POST("/backends/:backendID/mkdir", h.mkdir)
	group.POST("/backends/:backendID/rename", h.rename)
	group.DELETE("/backends/:backendID/entries", h.removeEntry)
	group.GET("/backends/:backendID/download", h.download)
	group.POST("/backends/:backendID/upload", h.upload)
}

func (h *Handler) listBackends(c *gin.Context) {
	items, err := h.service.ListBackends(c.Request.Context())
	if err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"backends": items})
}

func (h *Handler) getBackend(c *gin.Context) {
	id, ok := backendID(c)
	if !ok {
		return
	}
	item, err := h.service.GetBackend(c.Request.Context(), id)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"backend": item})
}

func (h *Handler) createBackend(c *gin.Context) {
	var request CreateBackendRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid storage backend"})
		return
	}
	item, err := h.service.CreateBackend(c.Request.Context(), request)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"backend": item})
}

func (h *Handler) updateBackend(c *gin.Context) {
	id, ok := backendID(c)
	if !ok {
		return
	}
	var request CreateBackendRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid storage backend"})
		return
	}
	item, err := h.service.UpdateBackend(c.Request.Context(), id, request)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"backend": item})
}

func (h *Handler) deleteBackend(c *gin.Context) {
	id, ok := backendID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteBackend(c.Request.Context(), id); err != nil {
		writeStorageError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) testBackend(c *gin.Context) {
	id, ok := backendID(c)
	if !ok {
		return
	}
	if err := h.service.TestBackend(c.Request.Context(), id); err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) testDraft(c *gin.Context) {
	var request CreateBackendRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid storage backend"})
		return
	}
	if err := h.service.TestDraft(c.Request.Context(), request); err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) listEntries(c *gin.Context) {
	id, ok := backendID(c)
	if !ok {
		return
	}
	entries, err := h.service.ListEntries(c.Request.Context(), id, c.Query("path"))
	if err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries, "path": c.Query("path")})
}

func (h *Handler) mkdir(c *gin.Context) {
	id, ok := backendID(c)
	if !ok {
		return
	}
	var request struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}
	if err := h.service.Mkdir(c.Request.Context(), id, request.Path); err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ok": true})
}

func (h *Handler) rename(c *gin.Context) {
	id, ok := backendID(c)
	if !ok {
		return
	}
	var request struct {
		From string `json:"from" binding:"required"`
		To   string `json:"to" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from and to are required"})
		return
	}
	if err := h.service.Rename(c.Request.Context(), id, request.From, request.To); err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) removeEntry(c *gin.Context) {
	id, ok := backendID(c)
	if !ok {
		return
	}
	recursive, _ := strconv.ParseBool(c.Query("recursive"))
	if err := h.service.Remove(c.Request.Context(), id, c.Query("path"), recursive); err != nil {
		writeStorageError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) download(c *gin.Context) {
	id, ok := backendID(c)
	if !ok {
		return
	}
	filePath := c.Query("path")
	backend, err := h.service.OpenByID(c.Request.Context(), id)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	defer backend.Close()
	reader, err := backend.Open(c.Request.Context(), filePath)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	defer reader.Close()
	c.Header("Content-Disposition", `attachment; filename="`+path.Base(filePath)+`"`)
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, reader)
}

func (h *Handler) upload(c *gin.Context) {
	id, ok := backendID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes+1024)
	if err := c.Request.ParseMultipartForm(maxUploadBytes + 1024); err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "upload is too large"})
		return
	}
	filePath := c.PostForm("path")
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()
	if filePath == "" {
		filePath = header.Filename
	}
	backend, err := h.service.OpenByID(c.Request.Context(), id)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	defer backend.Close()
	writer, err := backend.Create(c.Request.Context(), filePath)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	if _, err := io.Copy(writer, io.LimitReader(file, maxUploadBytes+1)); err != nil {
		_ = writer.Close()
		writeStorageError(c, err)
		return
	}
	if err := writer.Close(); err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"path": filePath})
}

func backendID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("backendID"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid storage backend ID"})
		return 0, false
	}
	return id, true
}

func writeStorageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": ErrNotFound.Error()})
	case errors.Is(err, ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": ErrConflict.Error()})
	case errors.Is(err, ErrNotEmpty):
		c.JSON(http.StatusConflict, gin.H{"error": ErrNotEmpty.Error()})
	case errors.Is(err, ErrUnavailable):
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
	case errors.Is(err, credential.ErrNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "credential encryption key is not configured"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage operation failed"})
	}
}
