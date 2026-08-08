package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// NewMSSQL opens a connection pool to SQL Server and pings it immediately.
// sql.Open alone only validates DSN syntax — it never dials the server —
// so without this ping, a bad host/port/credential would only surface on
// the first real request instead of failing fast at startup.
func NewMSSQL(ctx context.Context, dsn string) (*sql.DB, error) {
	sqlDB, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mssql: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping mssql: %w", err)
	}

	return sqlDB, nil
}
