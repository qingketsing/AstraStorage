// 这个文件是用于连接数据库的模块，提供了数据库连接的初始化和关闭功能。

package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq" // PostgreSQL 驱动
)

type DBConnection struct {
	// 数据库连接相关字段
	db *sql.DB
}

func NewDBConnection(dataSourceName string) (*DBConnection, error) {
	db, err := sql.Open("postgres", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("打开数据库连接失败: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	log.Println("数据库连接成功")
	return &DBConnection{db: db}, nil
}

func (dbc *DBConnection) Close() error {
	if dbc.db != nil {
		return dbc.db.Close()
	}
	return nil
}
