package services

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
)

var DB *sql.DB

func InitDB() {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Fatal("DATABASE_URL not set")
	}

	var err error
	DB, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}

	err = DB.Ping()
	if err != nil {
		log.Fatal("Cannot connect to database:", err)
	}

	fmt.Println("Connected to Supabase Postgres")

	createTableQuery := `
	CREATE TABLE IF NOT EXISTS waitlist (
		id SERIAL PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		referral_code TEXT UNIQUE NOT NULL,
		referred_by TEXT,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = DB.Exec(createTableQuery)
	if err != nil {
		log.Printf("Error creating table: %v", err)
	}
}
