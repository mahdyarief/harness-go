package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"hsg/internal/lib"
	"hsg/internal/repositories"
)

func connectTenantDB(dsn string) (*sql.DB, error) {
	db, err := lib.OpenDB(dsn)
	if err != nil {
		return nil, fmt.Errorf("koneksi database tenant gagal")
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unsafe, err := repositories.TenantRoleUnsafe(ctx, db)
	if err != nil || unsafe {
		_ = db.Close()
		return nil, fmt.Errorf("role database tenant harus non-owner tanpa hak istimewa atau bypass RLS")
	}
	if err = repositories.NewKnowledgeEmbeddingRepository(db).ValidateRuntimeRole(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("role database embedding harus terisolasi dengan FORCE RLS")
	}
	return db, nil
}
