package main

import (
	"fmt"
	"log"
)

func main() {
	db, err := InitDB("hermem.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	fmt.Println("Database initialized successfully")
}
