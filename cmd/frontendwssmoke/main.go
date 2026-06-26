package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	url := env("FRONTEND_WS_URL", "ws://127.0.0.1:18080/frontend/ws/CP-LIMIT-AUTO-001")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dialer := websocket.DefaultDialer

	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		log.Fatalf("frontend ws dial failed: %v", err)
	}
	defer conn.Close()

	fmt.Println("frontend ws connected:", url)

	for i := 0; i < 3; i++ {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Fatalf("frontend ws read failed: %v", err)
		}

		fmt.Println(string(msg))
	}
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
