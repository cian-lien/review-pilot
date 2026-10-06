package persistence

import (
	"context"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"time"
)

type Store struct{ db *gorm.DB }

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	if err != nil {
		return nil, err
	}
	sql, err := db.DB()
	if err != nil {
		return nil, err
	}
	sql.SetMaxOpenConns(10)
	sql.SetMaxIdleConns(5)
	sql.SetConnMaxLifetime(30 * time.Minute)
	if err = sql.PingContext(ctx); err != nil {
		_ = sql.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}
func (s *Store) DB() *gorm.DB { return s.db }
func (s *Store) Close() error {
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}
