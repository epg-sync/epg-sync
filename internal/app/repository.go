package app

import (
	"github.com/epg-sync/epgsync/internal/repository/postgres"
	"github.com/epg-sync/epgsync/pkg/logger"
)

func (app *App) initializeRepositories() error {
	logger.Debug("Initializing repositories...")

	app.repos = &Repositories{
		Channel:         postgres.NewChannelRepository(app.db),
		Program:         postgres.NewProgramRepository(app.db),
		ChannelMappings: postgres.NewChannelMappingsRepository(app.db),
		Timezone:        postgres.NewTimezoneRepository(app.db),
		User:            postgres.NewUserRepository(app.db),
	}

	return nil

}
