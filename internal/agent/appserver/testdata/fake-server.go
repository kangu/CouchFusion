package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

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
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if record != nil {
			_, _ = record.Write(append(line, '\n'))
			_ = record.Sync()
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(line, &request) != nil || len(request.ID) == 0 {
			continue
		}
		var result any = map[string]any{}
		switch request.Method {
		case "initialize":
			result = map[string]any{"userAgent": "fake", "codexHome": "/tmp/fake-codex", "platformFamily": "unix", "platformOs": "macos"}
		case "account/read":
			result = map[string]any{"account": map[string]any{"type": "chatgpt", "email": "person@example.test", "planType": "plus"}, "requiresOpenaiAuth": true}
		}
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": result})
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
