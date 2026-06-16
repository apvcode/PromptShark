package main

import (
	"database/sql"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

type Session struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Steps     int    `json:"steps"`
}

type Step struct {
	ID              string `json:"id"`
	SessionID       string `json:"session_id"`
	StepIndex       int    `json:"step_index"`
	RequestPayload  string `json:"request_payload"`
	ResponsePayload string `json:"response_payload"`
	TokensUsed      int    `json:"tokens_used"`
	LatencyMs       int    `json:"latency_ms"`
}

func initDB(dbPath string, schemaPath string) {
	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}

	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		log.Fatalf("Failed to read schema.sql: %v", err)
	}

	_, err = db.Exec(string(schemaBytes))
	if err != nil {
		log.Fatalf("Failed to execute schema: %v", err)
	}

	// Try to alter table to add latency_ms (will fail silently if already exists)
	db.Exec("ALTER TABLE Steps ADD COLUMN latency_ms INTEGER DEFAULT 0")

	log.Println("SQLite DB initialized")
}

func createSession() string {
	id := uuid.New().String()
	_, err := db.Exec("INSERT INTO Sessions (id, timestamp, replay_target) VALUES (?, ?, 0)", id, time.Now())
	if err != nil {
		log.Printf("Failed to create session: %v", err)
	}
	return id
}

func createStep(sessionID string, stepIndex int, reqPayload string) string {
	id := uuid.New().String()
	_, err := db.Exec(`
		INSERT INTO Steps (id, session_id, step_index, request_payload, tokens_used) 
		VALUES (?, ?, ?, ?, ?)
	`, id, sessionID, stepIndex, reqPayload, 0)
	if err != nil {
		log.Printf("Failed to create step: %v", err)
	}
	return id
}

func updateStepResponse(stepID string, resPayload string, tokensUsed int, latencyMs int) {
	_, err := db.Exec(`
		UPDATE Steps SET response_payload = ?, tokens_used = ?, latency_ms = ? WHERE id = ?
	`, resPayload, tokensUsed, latencyMs, stepID)
	if err != nil {
		log.Printf("Failed to update step: %v", err)
	}
}

func getReplayTarget(sessionID string) int {
	var target int
	err := db.QueryRow("SELECT replay_target FROM Sessions WHERE id = ?", sessionID).Scan(&target)
	if err != nil {
		return 0
	}
	return target
}

func setReplayTarget(sessionID string, target int) {
	_, err := db.Exec("UPDATE Sessions SET replay_target = ? WHERE id = ?", target, sessionID)
	if err != nil {
		log.Printf("Failed to set replay target: %v", err)
	}
}

func getStepResponse(sessionID string, stepIndex int) string {
	var payload sql.NullString
	err := db.QueryRow("SELECT response_payload FROM Steps WHERE session_id = ? AND step_index = ?", sessionID, stepIndex).Scan(&payload)
	if err != nil || !payload.Valid {
		return ""
	}
	return payload.String
}

func updateStepRequest(sessionID string, stepIndex int, newPayload string) {
	_, err := db.Exec("UPDATE Steps SET request_payload = ? WHERE session_id = ? AND step_index = ?", newPayload, sessionID, stepIndex)
	if err != nil {
		log.Printf("Failed to update step request: %v", err)
	}
}

func getAllSessions() []Session {
	rows, err := db.Query(`
		SELECT s.id, s.timestamp, COUNT(st.id) as step_count 
		FROM Sessions s 
		LEFT JOIN Steps st ON s.id = st.session_id 
		GROUP BY s.id 
		ORDER BY s.timestamp DESC
	`)
	if err != nil {
		return []Session{}
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Timestamp, &s.Steps); err == nil {
			sessions = append(sessions, s)
		}
	}
	return sessions
}

func getSessionSteps(sessionID string) []Step {
	rows, err := db.Query(`
		SELECT id, session_id, step_index, IFNULL(request_payload, ''), IFNULL(response_payload, ''), tokens_used, IFNULL(latency_ms, 0)
		FROM Steps 
		WHERE session_id = ? 
		ORDER BY step_index ASC
	`, sessionID)
	if err != nil {
		return []Step{}
	}
	defer rows.Close()

	var steps []Step
	for rows.Next() {
		var s Step
		if err := rows.Scan(&s.ID, &s.SessionID, &s.StepIndex, &s.RequestPayload, &s.ResponsePayload, &s.TokensUsed, &s.LatencyMs); err == nil {
			steps = append(steps, s)
		}
	}
	return steps
}

func deleteSession(sessionID string) {
	_, err := db.Exec("DELETE FROM Sessions WHERE id = ?", sessionID)
	if err != nil {
		log.Printf("Failed to delete session: %v", err)
	}
}
