package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type WsManager struct {
	clients map[*websocket.Conn]bool
	mu      sync.Mutex
}

var wsManager = &WsManager{
	clients: make(map[*websocket.Conn]bool),
}

type WsMessage struct {
	SessionID string `json:"session_id"`
	StepIndex int    `json:"step_index"`
	StepID    string `json:"step_id"`
	Content   string `json:"content"`
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS Upgrade error: %v", err)
		return
	}

	wsManager.mu.Lock()
	wsManager.clients[conn] = true
	wsManager.mu.Unlock()

	log.Println("New WebSocket client connected")

	// Read loop to keep connection alive and detect disconnects
	go func() {
		defer func() {
			wsManager.mu.Lock()
			delete(wsManager.clients, conn)
			wsManager.mu.Unlock()
			conn.Close()
			log.Println("WebSocket client disconnected")
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}()
}

func broadcastChunk(msg WsMessage) {
	wsManager.mu.Lock()
	defer wsManager.mu.Unlock()

	for client := range wsManager.clients {
		err := client.WriteJSON(msg)
		if err != nil {
			log.Printf("WS Write error: %v", err)
			client.Close()
			delete(wsManager.clients, client)
		}
	}
}
