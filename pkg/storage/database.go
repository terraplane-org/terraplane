package storage

import (
	"context"

	"github.com/xyzjace/terraplane/config"
	"gorm.io/gorm"
)

type DB struct {
	gorm        *gorm.DB
	databaseURL string
}

func New(cfg *config.Config) (*DB, error) {
	gormDB, err := openDB(cfg)
	if err != nil {
		return nil, err
	}
	return &DB{gorm: gormDB, databaseURL: cfg.DatabaseURL}, nil
}

// Migrate applies pending Atlas migrations bundled with the application.
func Migrate(ctx context.Context, cfg *config.Config) error {
	return applyMigrations(ctx, cfg.DatabaseURL)
}

// RequireCurrentSchema returns an error if migrations have not been applied.
func (d *DB) RequireCurrentSchema(ctx context.Context) error {
	return verifyMigrations(ctx, d.databaseURL)
}
