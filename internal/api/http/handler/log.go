package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/epg-sync/epgsync/internal/api/dto"
	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/service"
	"github.com/epg-sync/epgsync/pkg/logger"
	"github.com/gin-gonic/gin"
)

type LogHandler struct {
	logService *service.LogService
}

func NewLogHandler(logService *service.LogService) *LogHandler {
	return &LogHandler{logService: logService}
}

// ListLogs 按条件查询程序日志，从新到旧
//
//	level      最低级别：debug / info / warn / error
//	logger     来源：app（业务）/ http / gorm
//	q          关键字，匹配消息、字段和代码位置
//	since/until 时间范围，RFC3339
//	before_id  翻页，返回比它更早的
//	after_id   实时刷新，返回比它更新的
//	limit      条数，默认 100，最大 500
func (h *LogHandler) ListLogs(c *gin.Context) {
	var filter model.LogFilter

	if v := c.Query("level"); v != "" {
		lvl, err := logger.ParseLevelStrict(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, dto.BadRequest("Invalid level", err))
			return
		}
		n := int8(lvl)
		filter.MinLevel = &n
	}
	if v, ok := c.GetQuery("logger"); ok && v != "" {
		if v == "app" {
			v = ""
		}
		filter.Logger = &v
	}
	filter.Keyword = c.Query("q")

	var err error
	if filter.Since, err = parseTimeParam(c, "since"); err != nil {
		c.JSON(http.StatusBadRequest, dto.BadRequest("Invalid since", err))
		return
	}
	if filter.Until, err = parseTimeParam(c, "until"); err != nil {
		c.JSON(http.StatusBadRequest, dto.BadRequest("Invalid until", err))
		return
	}
	for _, p := range []struct {
		name string
		dst  *int64
	}{{"before_id", &filter.BeforeID}, {"after_id", &filter.AfterID}} {
		if v := c.Query(p.name); v != "" {
			if *p.dst, err = strconv.ParseInt(v, 10, 64); err != nil {
				c.JSON(http.StatusBadRequest, dto.BadRequest("Invalid "+p.name, err))
				return
			}
		}
	}
	if v := c.Query("limit"); v != "" {
		if filter.Limit, err = strconv.Atoi(v); err != nil {
			c.JSON(http.StatusBadRequest, dto.BadRequest("Invalid limit", err))
			return
		}
	}

	logs, err := h.logService.List(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.InternalServerError("Failed to list logs", err))
		return
	}
	c.JSON(http.StatusOK, dto.Success(logs))
}

func parseTimeParam(c *gin.Context, name string) (time.Time, error) {
	v := c.Query(name)
	if v == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, v)
}
