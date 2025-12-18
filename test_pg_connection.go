package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

func main() {
	dsn := "host=localhost port=5432 user=postgres dbname=driver sslmode=disable"

	log.Printf("尝试连接: %s", dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("sql.Open 失败: %v", err)
	}
	defer db.Close()

	log.Println("sql.Open 成功，尝试 Ping...")

	if err := db.Ping(); err != nil {
		log.Fatalf("Ping 失败: %v", err)
	}

	log.Println("✓ Ping 成功！")

	var version string
	err = db.QueryRow("SELECT version()").Scan(&version)
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}

	fmt.Printf("✓ PostgreSQL 版本: %s\n", version)
}
