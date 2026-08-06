package storage

import (
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

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
	group.POST("/backends/test", h.testDraft)
	group.GET("/backends/:name", h.getBackend)
	group.POST("/backends/:name/test", h.testBackend)
	group.GET("/backends/:name/entries", h.listEntries)
	group.POST("/backends/:name/mkdir", h.mkdir)
	group.POST("/backends/:name/rename", h.rename)
	group.DELETE("/backends/:name/entries", h.removeEntry)
	group.GET("/backends/:name/download", h.download)
	group.POST("/backends/:name/upload", h.upload)
	group.POST("/sync/jobs", h.startSyncJob)
	group.GET("/sync/jobs/:jobID", h.getSyncJob)
	group.POST("/sync/jobs/:jobID/cancel", h.cancelSyncJob)
	group.GET("/sync/schedules", h.listSyncSchedules)
	group.POST("/sync/schedules/:id/run", h.runSyncSchedule)
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
	name, ok := backendName(c)
	if !ok {
		return
	}
	item, err := h.service.GetBackend(c.Request.Context(), name)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"backend": item})
}

func (h *Handler) testBackend(c *gin.Context) {
	name, ok := backendName(c)
	if !ok {
		return
	}
	if err := h.service.TestBackend(c.Request.Context(), name); err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) testDraft(c *gin.Context) {
	var request DraftBackendRequest
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
	name, ok := backendName(c)
	if !ok {
		return
	}
	entries, err := h.service.ListEntries(c.Request.Context(), name, c.Query("path"))
	if err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries, "path": c.Query("path")})
}

func (h *Handler) mkdir(c *gin.Context) {
	name, ok := backendName(c)
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
	if err := h.service.Mkdir(c.Request.Context(), name, request.Path); err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ok": true})
}

func (h *Handler) rename(c *gin.Context) {
	name, ok := backendName(c)
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
	if err := h.service.Rename(c.Request.Context(), name, request.From, request.To); err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) removeEntry(c *gin.Context) {
	name, ok := backendName(c)
	if !ok {
		return
	}
	recursive, _ := strconv.ParseBool(c.Query("recursive"))
	if err := h.service.Remove(c.Request.Context(), name, c.Query("path"), recursive); err != nil {
		writeStorageError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) download(c *gin.Context) {
	name, ok := backendName(c)
	if !ok {
		return
	}
	filePath := c.Query("path")
	backend, err := h.service.OpenByName(c.Request.Context(), name)
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
	name, ok := backendName(c)
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
	backend, err := h.service.OpenByName(c.Request.Context(), name)
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

func (h *Handler) startSyncJob(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)
	var request SyncJobRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sync job request"})
		return
	}
	job, err := h.service.StartSyncJob(c.Request.Context(), request)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job": job})
}

func (h *Handler) getSyncJob(c *gin.Context) {
	job, err := h.service.GetSyncJob(c.Param("jobID"))
	if err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": job})
}

func (h *Handler) cancelSyncJob(c *gin.Context) {
	job, err := h.service.CancelSyncJob(c.Param("jobID"))
	if err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": job})
}

func (h *Handler) listSyncSchedules(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"schedules": h.service.ListSyncSchedules()})
}

func (h *Handler) runSyncSchedule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid schedule id"})
		return
	}
	schedule, err := h.service.TriggerSyncSchedule(id)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"schedule": schedule})
}

func backendName(c *gin.Context) (string, bool) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" || name == "test" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid storage backend name"})
		return "", false
	}
	return name, true
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
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage operation failed"})
	}
}
