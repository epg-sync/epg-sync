package app

import (
	"github.com/epg-sync/epgsync/internal/service"
	"github.com/epg-sync/epgsync/pkg/logger"
)

func (app *App) initializeServices() error {
	logger.Debug("Initializing services...")

	app.services = &Services{
		EPG:            service.NewEPGService(app.repos.Program, app.repos.Channel, app.repos.ChannelMappings, app.cache, app.providerChain),
		Channel:        service.NewChannelService(app.repos.Channel, app.repos.ChannelMappings, app.cache, app.providerChain),
		ChannelMapping: service.NewChannelMappingService(app.repos.ChannelMappings, app.repos.Channel),
		User:           service.NewUserService(app.repos.User, app.cfg.Server.JWTSecret, app.cfg.Server.JWTExpireHours),
		Log:            service.NewLogService(app.repos.Log, app.logHub, app.cfg.Logger.RetentionDays),
	}

	app.services.Scheduler = service.NewSchedulerService(app.services.EPG, app.services.Channel, app.services.ChannelMapping, app.providerChain, app.cache)

	return nil
}
