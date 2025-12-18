package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

func main() {
	ports := []int{20000, 20001, 20002, 20003, 20004}

	for i, port := range ports {
		fmt.Printf("\n=== Testing postgres-%d (port %d) ===\n", i, port)

		connStr := fmt.Sprintf("host=localhost port=%d user=postgres dbname=driver sslmode=disable", port)
		db, err := sql.Open("postgres", connStr)
		if err != nil {
			log.Printf("❌ Failed to open connection: %v", err)
			continue
		}
		defer db.Close()

		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(time.Second * 5)

		// Test connection
		if err := db.Ping(); err != nil {
			log.Printf("❌ Ping failed: %v", err)
			continue
		}
		fmt.Println("✅ Connection: OK")

		// Test tables
		var fileCount, dirCount int
		err = db.QueryRow("SELECT COUNT(*) FROM files").Scan(&fileCount)
		if err != nil {
			log.Printf("❌ Files table query failed: %v", err)
			continue
		}

		err = db.QueryRow("SELECT COUNT(*) FROM directory_tree").Scan(&dirCount)
		if err != nil {
			log.Printf("❌ Directory_tree table query failed: %v", err)
			continue
		}

		fmt.Printf("✅ Tables initialized: files(%d), directory_tree(%d)\n", fileCount, dirCount)
	}

	fmt.Println("\n=== All database checks complete ===")
}
