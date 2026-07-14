package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type scenarioWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

func (w *scenarioWriter) send(value any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if values, ok := value.([]any); ok {
		for _, item := range values {
			_ = w.encoder.Encode(item)
		}
		return
	}
	_ = w.encoder.Encode(value)
}

func (w *scenarioWriter) later(value any) {
	go func() {
		time.Sleep(40 * time.Millisecond)
		w.send(value)
	}()
}

func main() {
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "--version=") {
		fmt.Println("codex-cli " + strings.TrimPrefix(os.Args[1], "--version="))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("codex-cli 0.144.4")
		return
	}
	record, _ := os.OpenFile(os.Getenv("FAKE_APP_SERVER_RECORD"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if record != nil {
		defer record.Close()
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	scenario := os.Getenv("FAKE_AGENT_SCENARIO") == "component-generation"
	writer := &scenarioWriter{encoder: encoder}
	threadID, turnID, cwd, turnNumber := "thread-component-generation", "", "", 0
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if record != nil {
			_, _ = record.Write(append(line, '\n'))
			_ = record.Sync()
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &request) != nil {
			continue
		}
		if scenario && request.Method == "" && len(request.ID) > 0 {
			switch string(request.ID) {
			case `"question-request"`:
				writer.later(fileChangeSequence(threadID, turnID, cwd, "file-request"))
			case `"file-request"`:
				writer.later(map[string]any{"id": "dependency-request", "method": "item/commandExecution/requestApproval", "params": map[string]any{
					"threadId": threadID, "turnId": turnID, "itemId": "dependency-command", "command": "bun add carousel-helper", "cwd": cwd,
				}})
			case `"dependency-request"`:
				if cwd != "" {
					path := filepath.Join(cwd, "app", "components", "content", "Hero.vue")
					_ = os.MkdirAll(filepath.Dir(path), 0o755)
					_ = os.WriteFile(path, []byte("<template><section>Generated hero</section></template>\n"), 0o644)
				}
				writer.later(completionSequence(threadID, turnID))
			}
			continue
		}
		if len(request.ID) == 0 {
			continue
		}
		var result any = map[string]any{}
		switch request.Method {
		case "initialize":
			result = map[string]any{"userAgent": "fake", "codexHome": "/tmp/fake-codex", "platformFamily": "unix", "platformOs": "macos"}
		case "account/read":
			result = map[string]any{"account": map[string]any{"type": "chatgpt", "email": "person@example.test", "planType": "plus"}, "requiresOpenaiAuth": true}
		case "thread/start":
			if scenario {
				var params struct {
					CWD string `json:"cwd"`
				}
				_ = json.Unmarshal(request.Params, &params)
				cwd = params.CWD
				result = map[string]any{"thread": map[string]any{"id": threadID}}
			}
		case "thread/read", "thread/resume":
			if scenario {
				result = map[string]any{"thread": map[string]any{"id": threadID, "turns": []any{}}}
			}
		case "turn/start":
			if scenario {
				turnNumber++
				turnID = fmt.Sprintf("turn-%d", turnNumber)
				result = map[string]any{"turn": map[string]any{"id": turnID, "status": "inProgress"}}
			}
		}
		writer.send(map[string]any{"id": request.ID, "result": result})
		if scenario && request.Method == "turn/start" {
			if turnNumber == 1 {
				writer.later(map[string]any{"id": "question-request", "method": "item/tool/requestUserInput", "params": map[string]any{
					"threadId": threadID, "turnId": turnID, "itemId": "question", "questions": []any{map[string]any{
						"id": "tone", "header": "Visual tone", "question": "Which visual tone?", "options": []any{map[string]any{"label": "Quiet"}, map[string]any{"label": "Bold"}},
					}},
				}})
			} else {
				writer.later(fileChangeSequence(threadID, turnID, cwd, "follow-up-file-request"))
			}
		}
		if request.Method == "account/read" {
			marker := os.Getenv("FAKE_EXIT_ONCE_MARKER")
			if marker != "" {
				if _, err := os.Stat(marker); os.IsNotExist(err) {
					_ = os.WriteFile(marker, []byte("exited"), 0o600)
					return
				}
			}
		}
	}
}

func fileChangeSequence(threadID, turnID, cwd, requestID string) []any {
	path := filepath.Join(cwd, "app", "components", "content", "Hero.vue")
	return []any{
		map[string]any{"method": "item/started", "params": map[string]any{
			"threadId": threadID, "turnId": turnID, "item": map[string]any{"id": "file-change", "type": "fileChange", "status": "inProgress", "changes": []any{map[string]any{"path": path}}},
		}},
		map[string]any{"id": requestID, "method": "item/fileChange/requestApproval", "params": map[string]any{
			"threadId": threadID, "turnId": turnID, "itemId": "file-change", "reason": "Create the component source",
		}},
	}
}

func completionSequence(threadID, turnID string) []any {
	return []any{
		map[string]any{"method": "item/completed", "params": map[string]any{
			"threadId": threadID, "turnId": turnID, "item": map[string]any{"id": "message", "type": "agentMessage", "status": "completed", "text": "Generated the component and validated its defaults."},
		}},
		map[string]any{"method": "turn/completed", "params": map[string]any{
			"threadId": threadID, "turn": map[string]any{"id": turnID, "status": "completed"},
		}},
	}
}
