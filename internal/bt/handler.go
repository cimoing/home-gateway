package bt

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"home-gateway/internal/model"

	"github.com/gin-gonic/gin"
)

// Handler exposes the authenticated BT management API.
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Register(api *gin.RouterGroup) {
	group := api.Group("/bt")
	group.GET("/settings", h.settings)
	group.PUT("/settings", h.updateSettings)
	group.POST("/block", h.addBlock)
	group.GET("/status", h.status)
	group.GET("/tasks", h.listTasks)
	group.POST("/tasks/magnet", h.addMagnet)
	group.POST("/tasks/torrent", h.addTorrent)
	group.GET("/tasks/:taskID", h.getTask)
	group.GET("/tasks/:taskID/files", h.files)
	group.GET("/tasks/:taskID/peers", h.peers)
	group.POST("/tasks/:taskID/pause", h.pause)
	group.POST("/tasks/:taskID/resume", h.resume)
	group.POST("/tasks/:taskID/sync", h.syncTask)
	group.PUT("/tasks/:taskID/files", h.updateFiles)
	group.DELETE("/tasks/:taskID", h.deleteTask)
}

func (h *Handler) settings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"settings": h.service.Settings()})
}

func (h *Handler) updateSettings(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	var request UpdateSettingsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid BT settings"})
		return
	}
	settings, err := h.service.UpdateSettings(request)
	if err != nil {
		writeBTError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": settings})
}

func (h *Handler) addBlock(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	var request AddBlockRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid BT block request"})
		return
	}
	block, err := h.service.AddBlock(request)
	if err != nil {
		writeBTError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"block": block})
}

func (h *Handler) status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": h.service.Status()})
}

func (h *Handler) listTasks(c *gin.Context) {
	tasks, err := h.service.ListTasks(
		c.Request.Context(),
		c.Query("status"),
		c.Query("search"),
	)
	if err != nil {
		writeBTError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

func (h *Handler) getTask(c *gin.Context) {
	id, ok := btPathID(c)
	if !ok {
		return
	}
	task, err := h.service.GetTask(c.Request.Context(), id)
	if err != nil {
		writeBTError(c, err)
		return
	}
	files, err := h.service.Files(c.Request.Context(), id)
	if err != nil {
		writeBTError(c, err)
		return
	}
	peers, err := h.service.Peers(c.Request.Context(), id)
	if err != nil {
		writeBTError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": task, "files": files, "peers": peers})
}

func (h *Handler) peers(c *gin.Context) {
	id, ok := btPathID(c)
	if !ok {
		return
	}
	peers, err := h.service.Peers(c.Request.Context(), id)
	if err != nil {
		writeBTError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"peers": peers})
}

func (h *Handler) addMagnet(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024)
	var request struct {
		URI            string `json:"uri" binding:"required"`
		Subdirectory   string `json:"subdirectory"`
		StorageBackend string `json:"storageBackend"`
		SyncStrategy   string `json:"syncStrategy"`
		Start          bool   `json:"start"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid magnet request"})
		return
	}
	task, err := h.service.AddMagnet(c.Request.Context(), request.URI, AddOptions{
		Subdirectory:   request.Subdirectory,
		StorageBackend: request.StorageBackend,
		SyncStrategy:   request.SyncStrategy,
		Start:          request.Start,
	})
	if err != nil {
		writeBTError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"task": task})
}

func (h *Handler) addTorrent(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 11<<20)
	if err := c.Request.ParseMultipartForm(11 << 20); err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "torrent upload is too large"})
		return
	}
	file, _, err := c.Request.FormFile("torrent")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "torrent file is required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read torrent file failed"})
		return
	}
	if len(data) > 10<<20 {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "torrent upload is too large"})
		return
	}
	start, _ := strconv.ParseBool(c.PostForm("start"))
	task, err := h.service.AddTorrent(c.Request.Context(), data, AddOptions{
		Subdirectory:   c.PostForm("subdirectory"),
		StorageBackend: c.PostForm("storageBackend"),
		SyncStrategy:   c.PostForm("syncStrategy"),
		Start:          start,
	})
	if err != nil {
		writeBTError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"task": task})
}

func (h *Handler) files(c *gin.Context) {
	id, ok := btPathID(c)
	if !ok {
		return
	}
	files, err := h.service.Files(c.Request.Context(), id)
	if err != nil {
		writeBTError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

func (h *Handler) pause(c *gin.Context) {
	h.control(c, h.service.Pause)
}

func (h *Handler) resume(c *gin.Context) {
	h.control(c, h.service.Resume)
}

func (h *Handler) syncTask(c *gin.Context) {
	h.control(c, h.service.RequestSync)
}

func (h *Handler) control(
	c *gin.Context,
	action func(context.Context, int64) (model.BTTask, error),
) {
	id, ok := btPathID(c)
	if !ok {
		return
	}
	task, err := action(c.Request.Context(), id)
	if err != nil {
		writeBTError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": task})
}

func (h *Handler) updateFiles(c *gin.Context) {
	id, ok := btPathID(c)
	if !ok {
		return
	}
	var request struct {
		Files []FileSelection `json:"files" binding:"required"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file selection"})
		return
	}
	files, err := h.service.UpdateFiles(c.Request.Context(), id, request.Files)
	if err != nil {
		writeBTError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

func (h *Handler) deleteTask(c *gin.Context) {
	id, ok := btPathID(c)
	if !ok {
		return
	}
	deleteData, _ := strconv.ParseBool(c.Query("deleteData"))
	if err := h.service.Delete(c.Request.Context(), id, deleteData); err != nil {
		writeBTError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func btPathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("taskID"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid BT task ID"})
		return 0, false
	}
	return id, true
}

func writeBTError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": ErrNotFound.Error()})
	case errors.Is(err, ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": ErrConflict.Error()})
	case errors.Is(err, ErrUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": ErrUnavailable.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "BT operation failed"})
	}
}
