package mysql

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var db *sql.DB

// Init 初始化 MySQL 连接池
func Init(dsn string) error {
	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open DB failed: %w", err)
	}

	if err = db.Ping(); err != nil {
		return fmt.Errorf("ping DB failed: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	slog.Info("MySQL connected")
	return nil
}

// DBConn 返回数据库连接对象
func DBConn() *sql.DB {
	return db
}
