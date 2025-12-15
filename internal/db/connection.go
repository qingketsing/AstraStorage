// 这个文件是用于连接数据库的包声明

package db

import (
	"database/sql"
)

type DBConnection struct {
	// 数据库连接相关字段
	db *sql.DB
}

func NewDBConnection(dataSourceName string) (*DBConnection, error) {
	// db, err := sql.Open("postgres", "host=localhost port=5432 user=postgres password=postgre dbname=driver sslmode=disable")
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// if err := db.Ping(); err != nil {
	// 	db.Close()
	// 	log.Fatal(err)
	// }
	// log.Println("数据库连接成功")
	// return &DBConnection{db}, err
	return &DBConnection{}, nil
}
