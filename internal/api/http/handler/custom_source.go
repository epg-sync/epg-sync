package handler

import (
	"net/http"
	"strconv"

	"github.com/epg-sync/epgsync/internal/api/dto"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/service"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/gin-gonic/gin"
)

const defaultCustomSourcePriority = 100

type CustomSourceHandler struct {
	customSourceService *service.CustomSourceService
}

func NewCustomSourceHandler(customSourceService *service.CustomSourceService) *CustomSourceHandler {
	return &CustomSourceHandler{customSourceService: customSourceService}
}

func (h *CustomSourceHandler) ListCustomSources(c *gin.Context) {
	sources, err := h.customSourceService.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.InternalServerError("Failed to list custom sources", err))
		return
	}
	if sources == nil {
		sources = []*model.CustomSource{}
	}

	c.JSON(http.StatusOK, dto.Success(sources))
}

func (h *CustomSourceHandler) CreateCustomSource(c *gin.Context) {
	var req dto.CustomSourceRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.BadRequest("Invalid request parameters", err))
		return
	}

	source := &model.CustomSource{
		Name:     req.Name,
		URL:      req.URL,
		Enabled:  1,
		Priority: defaultCustomSourcePriority,
	}
	if req.Enabled != nil {
		source.Enabled = *req.Enabled
	}
	if req.Priority != nil {
		source.Priority = *req.Priority
	}

	created, err := h.customSourceService.Create(c.Request.Context(), source)
	if err != nil {
		respondCustomSourceError(c, "Failed to create custom source", err)
		return
	}

	c.JSON(http.StatusCreated, dto.Success(created))
}

func (h *CustomSourceHandler) UpdateCustomSource(c *gin.Context) {
	id, ok := parseCustomSourceID(c)
	if !ok {
		return
	}
	var req dto.CustomSourceRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.BadRequest("Invalid request parameters", err))
		return
	}

	ctx := c.Request.Context()
	existing, err := h.customSourceService.Get(ctx, id)
	if err != nil {
		respondCustomSourceError(c, "Failed to get custom source", err)
		return
	}

	source := &model.CustomSource{
		ID:       id,
		Name:     req.Name,
		URL:      req.URL,
		Enabled:  existing.Enabled,
		Priority: existing.Priority,
	}
	if req.Enabled != nil {
		source.Enabled = *req.Enabled
	}
	if req.Priority != nil {
		source.Priority = *req.Priority
	}

	updated, err := h.customSourceService.Update(ctx, source)
	if err != nil {
		respondCustomSourceError(c, "Failed to update custom source", err)
		return
	}

	c.JSON(http.StatusOK, dto.Success(updated))
}

func (h *CustomSourceHandler) DeleteCustomSource(c *gin.Context) {
	id, ok := parseCustomSourceID(c)
	if !ok {
		return
	}

	if err := h.customSourceService.Delete(c.Request.Context(), id); err != nil {
		respondCustomSourceError(c, "Failed to delete custom source", err)
		return
	}

	c.JSON(http.StatusOK, dto.Success(gin.H{"message": "Custom source deleted successfully"}))
}

// SyncCustomSource 在后台同步一个数据源，立即返回；结果看数据源的 last_status。已有同步在进行时返回 409
func (h *CustomSourceHandler) SyncCustomSource(c *gin.Context) {
	id, ok := parseCustomSourceID(c)
	if !ok {
		return
	}

	if err := h.customSourceService.StartSync(c.Request.Context(), id); err != nil {
		respondCustomSourceError(c, "Failed to start custom source sync", err)
		return
	}

	c.JSON(http.StatusAccepted, dto.Success("Custom source sync started"))
}

func parseCustomSourceID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, dto.BadRequest("Invalid custom source id", err))
		return 0, false
	}
	return id, true
}

func respondCustomSourceError(c *gin.Context, message string, err error) {
	switch {
	case errors.Is(err, errors.ErrCodeNotFound):
		c.JSON(http.StatusNotFound, dto.NotFound("Custom source not found"))
	case errors.Is(err, errors.ErrCodeInvalidParam):
		c.JSON(http.StatusBadRequest, dto.BadRequest(message, err))
	case errors.Is(err, errors.ErrCodeAlreadyExists):
		c.JSON(http.StatusConflict, dto.Error(http.StatusConflict, "A custom source sync is already in progress, try again later", err))
	default:
		c.JSON(http.StatusInternalServerError, dto.InternalServerError(message, err))
	}
}
