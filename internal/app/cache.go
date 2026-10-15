package app

import (
	"github.com/epg-sync/epgsync/internal/cache"
	"github.com/epg-sync/epgsync/pkg/logger"
)

func (app *App) initializeCache() error {
	logger.Debug("Initializing memory cache...")
	app.cache = cache.NewMemoryCache()
	return nil
}
