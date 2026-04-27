package main

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	marketsvc "github.com/vant-xyz/backend-code/services/markets"
)

func main() {
	_ = godotenv.Load("../../.env")

	asset := "BTC"
	at, _ := time.Parse(time.RFC3339, "2026-04-25T15:38:42Z")

	fmt.Printf("Fetching %s price at %s\n", asset, at.Format(time.RFC3339))

	cents, err := marketsvc.GetHistoricalPrice(asset, at)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Price: %d cents ($%d.%02d)\n", cents, cents/100, cents%100)
}
