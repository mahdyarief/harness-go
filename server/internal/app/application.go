package app

import (
	"database/sql"
	"log/slog"

	authula "github.com/Authula/authula"

	"hsg/internal/config"
	"hsg/internal/repositories"
)

type Application struct {
	Config       config.Config
	Foundation   config.Foundation
	TenantDB     *sql.DB
	Logger       *slog.Logger
	DB           *sql.DB
	Repositories repositories.Repositories
	Auth         *authula.Auth
}
