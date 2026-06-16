package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	chaosModeEnabled bool
	chaosMu          sync.Mutex
)

//go:embed ui/*
var uiFS embed.FS

// streamingBody intercepts chunks during SSE streaming, broadcasts them to WS,
// and saves the full response to DB upon EOF.
type streamingBody struct {
	io.ReadCloser
	stepID     string
	sessionID  string
	stepIndex  int
	latencyMs  int
	buffer     bytes.Buffer
	lineBuffer string
}

func (s *streamingBody) Read(p []byte) (n int, err error) {
	n, err = s.ReadCloser.Read(p)
	if n > 0 {
		chunk := string(p[:n])
		s.buffer.WriteString(chunk)

		s.lineBuffer += chunk
		lines := strings.Split(s.lineBuffer, "\n")
		
		// The last element is either an incomplete line or empty (if string ended in \n)
		s.lineBuffer = lines[len(lines)-1]
		lines = lines[:len(lines)-1]

		for _, line := range lines {
			if strings.HasPrefix(line, "data: ") {
				dataStr := strings.TrimPrefix(line, "data: ")
				if dataStr == "[DONE]" || strings.TrimSpace(dataStr) == "" {
					continue
				}

				var data struct {
					Choices []struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					} `json:"choices"`
				}
				if err := json.Unmarshal([]byte(dataStr), &data); err == nil {
					if len(data.Choices) > 0 && data.Choices[0].Delta.Content != "" {
						broadcastChunk(WsMessage{
							SessionID: s.sessionID,
							StepIndex: s.stepIndex,
							StepID:    s.stepID,
							Content:   data.Choices[0].Delta.Content,
						})
					}
				}
			}
		}
	}

	if err == io.EOF {
		fullResponse := s.buffer.String()
		tokens := parseTokens(fullResponse)
		updateStepResponse(s.stepID, fullResponse, tokens, s.latencyMs)

		// Check if it's a non-streaming response, in which case we broadcast the full content
		if !strings.Contains(fullResponse, "data: ") {
			var resMap map[string]interface{}
			if errJson := json.Unmarshal([]byte(fullResponse), &resMap); errJson == nil {
				if choices, ok := resMap["choices"].([]interface{}); ok && len(choices) > 0 {
					if choice, ok := choices[0].(map[string]interface{}); ok {
						if msg, ok := choice["message"].(map[string]interface{}); ok {
							if content, ok := msg["content"].(string); ok {
								broadcastChunk(WsMessage{
									SessionID: s.sessionID,
									StepIndex: s.stepIndex,
									StepID:    s.stepID,
									Content:   content,
								})
							}
						}
					}
				}
			}
		}
	}
	return n, err
}

func parseTokens(resPayload string) int {
	var usageData struct {
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(resPayload), &usageData); err == nil && usageData.Usage.TotalTokens > 0 {
		return usageData.Usage.TotalTokens
	}

	if idx := strings.Index(resPayload, `"total_tokens"`); idx != -1 {
		sub := resPayload[idx+14:]
		var digits []rune
		started := false
		for _, r := range sub {
			if r >= '0' && r <= '9' {
				digits = append(digits, r)
				started = true
			} else if started {
				break
			}
		}
		if len(digits) > 0 {
			if val, err := strconv.Atoi(string(digits)); err == nil {
				return val
			}
		}
	}

	return len(resPayload) / 4
}

var (
	currentSessionID string
	sessionMu        sync.Mutex
	proxyStepCounter int
)

func getOrCreateSession() string {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if currentSessionID == "" {
		currentSessionID = createSession()
		proxyStepCounter = 0
		sendToCore("RESET")

		go func(sessID string) {
			time.Sleep(100 * time.Millisecond)
			broadcastChunk(WsMessage{
				SessionID: "SYSTEM",
				StepIndex: 0,
				StepID:    "NEW_SESSION",
				Content:   sessID,
			})
		}(currentSessionID)
	}
	return currentSessionID
}

type ReplayRequest struct {
	StepIndex       int    `json:"step_index"`
	ModifiedContext string `json:"modified_context"`
}


// --- C++ Integration ---
var (
	coreStdin  io.WriteCloser
	coreStdout io.ReadCloser
	coreMu     sync.Mutex
)

func initCoreEngine() {
	cmd := exec.Command("../build/core_engine")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Printf("Failed to create C++ stdin pipe: %v (Is core_engine compiled?)", err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Failed to create C++ stdout pipe: %v", err)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start C++ core_engine: %v", err)
		return
	}

	coreStdin = stdin
	coreStdout = stdout

	// Start reading C++ responses asynchronously
	go func() {
		scanner := bufio.NewScanner(coreStdout)
		for scanner.Scan() {
			text := scanner.Text()
			fmt.Println("[C++ Core]:", text)
			// Here we could parse JSON from C++ and broadcast WS alerts if a loop is detected
			if strings.Contains(text, "loop_detected") {
				fmt.Println("!!! ALERT: Agent is stuck in an infinite loop !!!")
				sessionMu.Lock()
				idx := proxyStepCounter
				sessID := currentSessionID
				sessionMu.Unlock()
				broadcastChunk(WsMessage{
					SessionID: sessID,
					StepIndex: idx,
					StepID:    "ALERT",
					Content:   "\n\n🚨 [SYSTEM ALERT: INFINITE LOOP DETECTED BY C++ CORE] 🚨\n\n",
				})
			}
		}
	}()
	log.Println("C++ Core Engine connected via IPC")
}

func sendToCore(message string) {
	coreMu.Lock()
	defer coreMu.Unlock()
	if coreStdin != nil {
		cleanMsg := strings.ReplaceAll(message, "\n", " ")
		fmt.Fprintln(coreStdin, cleanMsg)
	}
}

func main() {
	portFlag := flag.Int("port", 8080, "HTTP server port")
	dbFlag := flag.String("db", "../db/promptshark.db", "Path to SQLite database file")
	schemaFlag := flag.String("schema", "../db/schema.sql", "Path to schema.sql file")
	targetFlag := flag.String("target", "https://api.openai.com", "Upstream LLM API URL")
	flag.Parse()

	port := *portFlag
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			port = p
		}
	}
	dbPath := *dbFlag
	if envDB := os.Getenv("DB_PATH"); envDB != "" {
		dbPath = envDB
	}

	initDB(dbPath, *schemaFlag)
	initCoreEngine()

	targetURL, _ := url.Parse(*targetFlag)

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			req.Host = targetURL.Host
		},
		ModifyResponse: func(res *http.Response) error {
			stepID := res.Request.Header.Get("X-Step-ID")
			sessionID := res.Request.Header.Get("X-Session-ID")
			stepIndexStr := res.Request.Header.Get("X-Step-Index")
			startTimeStr := res.Request.Header.Get("X-Start-Time")
			
			latencyMs := 0
			if startTimeMs, err := strconv.ParseInt(startTimeStr, 10, 64); err == nil && startTimeMs > 0 {
				latencyMs = int(time.Now().UnixMilli() - startTimeMs)
			}

			if stepID != "" {
				stepIndex, _ := strconv.Atoi(stepIndexStr)
				res.Body = &streamingBody{
					ReadCloser: res.Body,
					stepID:     stepID,
					sessionID:  sessionID,
					stepIndex:  stepIndex,
					latencyMs:  latencyMs,
				}
			}
			return nil
		},
	}

	// Serve the embedded UI directory at the root /
	uiSubFS, err := fs.Sub(uiFS, "ui")
	if err != nil {
		log.Fatal(err)
	}
	http.Handle("/", http.FileServer(http.FS(uiSubFS)))

	http.HandleFunc("/ws", handleWebSocket)

	// Chaos mode API
	http.HandleFunc("/api/chaos", func(w http.ResponseWriter, r *http.Request) {
		chaosMu.Lock()
		if r.Method == http.MethodPost {
			chaosModeEnabled = !chaosModeEnabled
		}
		status := chaosModeEnabled
		chaosMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"chaos_mode": status})
	})

	// API to fetch and manage sessions
	http.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			sessions := getAllSessions()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sessions)
		} else if r.Method == http.MethodPost {
			sessionMu.Lock()
			currentSessionID = createSession()
			proxyStepCounter = 0
			sessionMu.Unlock()
			
			sendToCore("RESET")

			broadcastChunk(WsMessage{
				SessionID: "SYSTEM",
				StepIndex: 0,
				StepID:    "NEW_SESSION",
				Content:   currentSessionID,
			})

			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"new_session_created","session_id":"` + currentSessionID + `"}`))
		}
	})

	http.HandleFunc("/api/sessions/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if r.Method == http.MethodDelete {
			sessionID := parts[len(parts)-1]
			if sessionID == "" && len(parts) > 1 {
				sessionID = parts[len(parts)-2]
			}
			deleteSession(sessionID)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"deleted"}`))
			return
		}

		if len(parts) == 5 && parts[4] == "steps" && r.Method == http.MethodGet {
			sessionID := parts[3]
			steps := getSessionSteps(sessionID)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(steps)
			return
		}

		if len(parts) == 5 && parts[4] == "replay" && r.Method == http.MethodPost {
			sessionID := parts[3]
			var req ReplayRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				setReplayTarget(sessionID, req.StepIndex)
				if req.ModifiedContext != "" {
					updateStepRequest(sessionID, req.StepIndex, req.ModifiedContext)
				}

				sessionMu.Lock()
				proxyStepCounter = 0
				sessionMu.Unlock()

				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"status": "replay_started"}`))
				fmt.Printf("\n=== Replay Armed for Step %d ===\n", req.StepIndex)
				return
			}
		}
		http.Error(w, "Not found", http.StatusNotFound)
	})

	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		sessionID := getOrCreateSession()

		sessionMu.Lock()
		proxyStepCounter++
		currentStep := proxyStepCounter
		sessionMu.Unlock()

		if r.Method == http.MethodPost {
			r.Header.Set("X-Start-Time", strconv.FormatInt(time.Now().UnixMilli(), 10))
			bodyBytes, err := io.ReadAll(r.Body)
			if err == nil {
				var reqMap map[string]interface{}
				json.Unmarshal(bodyBytes, &reqMap)
				isStream := false
				if s, ok := reqMap["stream"].(bool); ok {
					isStream = s
				}

				chaosMu.Lock()
				isChaos := chaosModeEnabled
				chaosMu.Unlock()

				replayTarget := getReplayTarget(sessionID)

				if isChaos && replayTarget == 0 {
					if time.Now().UnixNano()%2 == 0 { // 50% chance
						fmt.Println("[CHAOS MODE] Injecting HTTP 429 Too Many Requests")
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusTooManyRequests)
						errBody := `{"error":{"message":"Rate limit reached for requests","type":"requests","code":"rate_limit_exceeded"}}`
						w.Write([]byte(errBody))
						
						stepID := createStep(sessionID, currentStep, string(bodyBytes))
						updateStepResponse(stepID, errBody, 0, 15) // 15ms fake latency
						return
					}
				}

				if replayTarget > 0 && currentStep < replayTarget {
					savedResp := getStepResponse(sessionID, currentStep)
					fmt.Printf("[Time-Travel] Replaying Step %d from DB\n", currentStep)

					if isStream {
						w.Header().Set("Content-Type", "text/event-stream")
						w.Header().Set("Cache-Control", "no-cache")
						w.Header().Set("Connection", "keep-alive")
					} else {
						w.Header().Set("Content-Type", "application/json")
					}
					
					w.Write([]byte(savedResp))
					return
				} else if replayTarget > 0 && currentStep == replayTarget {
					fmt.Printf("[Time-Travel] Reached Target Step %d. Sending REAL request to OpenAI.\n", currentStep)
					
					// OVERRIDE the request body with the modified context from DB
					var modifiedReq string
					err := db.QueryRow("SELECT request_payload FROM Steps WHERE session_id = ? AND step_index = ?", sessionID, currentStep).Scan(&modifiedReq)
					if err == nil && modifiedReq != "" {
						bodyBytes = []byte(modifiedReq)
						r.ContentLength = int64(len(bodyBytes))
						fmt.Println("[Time-Travel] Overwrote agent request with modified context from UI!")
					}

					setReplayTarget(sessionID, 0)
				}

				// REAL REQUEST
				stepID := createStep(sessionID, currentStep, string(bodyBytes))
				r.Header.Set("X-Step-ID", stepID)
				r.Header.Set("X-Session-ID", sessionID)
				r.Header.Set("X-Step-Index", strconv.Itoa(currentStep))
				
				// Extract tool call for loop detection
				var lastFuncName, lastFuncArgs string
				if msgs, ok := reqMap["messages"].([]interface{}); ok && len(msgs) > 0 {
					for i := len(msgs) - 1; i >= 0; i-- {
						if msg, ok := msgs[i].(map[string]interface{}); ok && msg["role"] == "assistant" {
							if tcs, ok := msg["tool_calls"].([]interface{}); ok && len(tcs) > 0 {
								if tc, ok := tcs[0].(map[string]interface{}); ok {
									if f, ok := tc["function"].(map[string]interface{}); ok {
										if name, ok := f["name"].(string); ok {
											lastFuncName = name
										}
										if args, ok := f["arguments"].(string); ok {
											lastFuncArgs = args
										}
									}
								}
							}
							break
						}
					}
				}

				if lastFuncName != "" {
					sendToCore(fmt.Sprintf("CHECK_LOOP\t%s\t%s", lastFuncName, lastFuncArgs))
				}
				
				fmt.Printf("\n=== [AgentSupervisor] Real Request (Step: %d) ===\n", currentStep)
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
		}

		proxy.ServeHTTP(w, r)
	})

	// Print banner
	addr := fmt.Sprintf(":%d", port)
	banner := `
    ____                       __  _____ __               __  
   / __ \_________  ____ ___  / /_/ ___// /_  ____ ______/ /__
  / /_/ / ___/ __ \/ __ '__ \/ __ \__ \/ __ \/ __ '/ ___/ //_/
 / ____/ /  / /_/ / / / / / / /_/ /__/ / / / / /_/ / /  / ,<   
/_/   /_/   \____/_/ /_/ /_/ .___/____/_/ /_/\__,_/_/  /_/|_|  
                          /_/                                   
`
	fmt.Print(banner)
	fmt.Println("  ──────────────────────────────────────────────────")
	fmt.Printf("  🦈 Version     : 0.1.0\n")
	fmt.Printf("  🌐 Dashboard   : http://localhost:%d\n", port)
	fmt.Printf("  📦 Database    : %s\n", dbPath)
	fmt.Printf("  🎯 Target API  : %s\n", *targetFlag)
	fmt.Printf("  🔗 Proxy URL   : http://localhost:%d/v1\n", port)
	fmt.Println("  ──────────────────────────────────────────────────")
	fmt.Println()
	fmt.Println("  Point your OpenAI SDK base_url to the Proxy URL above.")
	fmt.Println("  Press Ctrl+C to stop.")
	fmt.Println()

	// Graceful shutdown
	srv := &http.Server{Addr: addr}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n  🛑 Shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Forced shutdown: %v", err)
	}
	if db != nil {
		db.Close()
	}
	fmt.Println("  ✅ Goodbye!")
}
