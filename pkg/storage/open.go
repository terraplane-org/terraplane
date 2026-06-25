package storage

import (
	"fmt"
	"strings"

	"github.com/xyzjace/terraplane/config"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openDB(cfg *config.Config) (*gorm.DB, error) {
	if cfg.DatabaseURL == "" {
		return nil, errDatabaseURLRequired()
	}

	driver := strings.ToLower(strings.TrimSpace(cfg.DatabaseDriver))
	if driver == "" {
		driver = "postgres"
	}

	switch driver {
	case "postgres":
		db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("connect to postgres database: %w", err)
		}
		return db, nil
	case "mysql":
		db, err := gorm.Open(mysql.Open(cfg.DatabaseURL), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("connect to mysql database: %w", err)
		}
		return db, nil
	default:
		return nil, fmt.Errorf(
			"unsupported DATABASE_DRIVER %q; terraplane supports postgres and mysql",
			cfg.DatabaseDriver,
		)
	}
}
