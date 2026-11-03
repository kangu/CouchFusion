package protocol

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

type pipeServer struct {
	input  *bufio.Scanner
	output *io.PipeWriter
}

func newPipeClient(t *testing.T, options Options) (*Client, pipeServer) {
	t.Helper()
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	client := NewClient(clientInput, clientOutput, options)
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverInput.Close()
		_ = serverOutput.Close()
	})
	return client, pipeServer{input: bufio.NewScanner(serverInput), output: serverOutput}
}

func TestClientCorrelatesConcurrentOutOfOrderResponses(t *testing.T) {
	client, server := newPipeClient(t, Options{})
	type result struct {
		Value string `json:"value"`
	}
	type outcome struct {
		method string
		value  string
		err    error
	}
	outcomes := make(chan outcome, 2)
	var wait sync.WaitGroup
	for _, method := range []string{"first", "second"} {
		method := method
		wait.Add(1)
		go func() {
			defer wait.Done()
			var response result
			err := client.Call(context.Background(), method, map[string]string{"method": method}, &response)
			outcomes <- outcome{method: method, value: response.Value, err: err}
		}()
	}

	requests := []RequestEnvelope{readRequest(t, server.input), readRequest(t, server.input)}
	for index := len(requests) - 1; index >= 0; index-- {
		request := requests[index]
		writeServerMessage(t, server.output, map[string]any{
			"id":     request.ID,
			"result": map[string]string{"value": request.Method + "-result"},
		})
	}
	wait.Wait()

	close(outcomes)
	for result := range outcomes {
		if result.err != nil || result.value != result.method+"-result" {
			t.Fatalf("%s result = %q, error = %v", result.method, result.value, result.err)
		}
	}
}

func TestClientDispatchesNotificationsAndServerRequests(t *testing.T) {
	client, server := newPipeClient(t, Options{})
	writeServerMessage(t, server.output, map[string]any{
		"method": "turn/started",
		"params": map[string]string{"threadId": "thread-1"},
	})
	notification := receive(t, client.Notifications())
	if notification.Method != "turn/started" || !json.Valid(notification.Params) {
		t.Fatalf("notification = %#v", notification)
	}

	writeServerMessage(t, server.output, map[string]any{
		"id":     "request-1",
		"method": "item/commandExecution/requestApproval",
		"params": map[string]string{"threadId": "thread-1", "turnId": "turn-1"},
	})
	request := receive(t, client.ServerRequests())
	if request.Method != "item/commandExecution/requestApproval" || string(request.ID) != `"request-1"` {
		t.Fatalf("server request = %#v", request)
	}
	responded := make(chan error, 1)
	go func() {
		responded <- client.Respond(context.Background(), request.ID, map[string]string{"decision": "accept"}, nil)
	}()
	response := readRawMessage(t, server.input)
	if err := <-responded; err != nil {
		t.Fatal(err)
	}
	if string(response["id"]) != `"request-1"` || !json.Valid(response["result"]) {
		t.Fatalf("response = %#v", response)
	}
}

func TestClientReturnsTypedRPCError(t *testing.T) {
	client, server := newPipeClient(t, Options{})
	done := make(chan error, 1)
	go func() { done <- client.Call(context.Background(), "thread/read", struct{}{}, nil) }()
	request := readRequest(t, server.input)
	writeServerMessage(t, server.output, map[string]any{
		"id":    request.ID,
		"error": map[string]any{"code": -32001, "message": "Server overloaded; retry later."},
	})
	var rpcError *RPCError
	if err := <-done; !errors.As(err, &rpcError) || rpcError.Code != -32001 {
		t.Fatalf("Call() error = %#v", err)
	}
}

func TestClientCancellationRemovesPendingCall(t *testing.T) {
	client, server := newPipeClient(t, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- client.Call(ctx, "thread/read", struct{}{}, nil) }()
	request := readRequest(t, server.input)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Call() error = %v, want context.Canceled", err)
	}
	writeServerMessage(t, server.output, map[string]any{"id": request.ID, "result": map[string]any{}})

	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		next := readRequest(t, server.input)
		writeServerMessage(t, server.output, map[string]any{"id": next.ID, "result": map[string]any{}})
	}()
	if err := client.Call(ctx, "thread/read", struct{}{}, nil); err != nil {
		t.Fatalf("next Call() error = %v", err)
	}
}

func TestClientClosesOnMalformedOrOversizedMessages(t *testing.T) {
	for _, test := range []struct {
		name            string
		options         Options
		line            string
		allowClosedPipe bool
	}{
		{name: "malformed", line: "{not-json}\n"},
		{name: "oversized", options: Options{MaxMessageBytes: 32}, line: `{"method":"notification","params":{"value":"this line is deliberately too large"}}` + "\n", allowClosedPipe: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, server := newPipeClient(t, test.options)
			if _, err := io.WriteString(server.output, test.line); err != nil && !(test.allowClosedPipe && errors.Is(err, io.ErrClosedPipe)) {
				t.Fatal(err)
			}
			select {
			case <-client.Done():
			case <-time.After(time.Second):
				t.Fatal("client did not close")
			}
			if !errors.Is(client.Err(), ErrProtocol) {
				t.Fatalf("Err() = %v, want ErrProtocol", client.Err())
			}
		})
	}
}

func readRequest(t *testing.T, scanner *bufio.Scanner) RequestEnvelope {
	t.Helper()
	if !scanner.Scan() {
		t.Fatalf("read request: %v", scanner.Err())
	}
	var request RequestEnvelope
	if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
		t.Fatal(err)
	}
	if request.ID == 0 || request.Method == "" {
		t.Fatalf("request = %#v", request)
	}
	return request
}

func readRawMessage(t *testing.T, scanner *bufio.Scanner) map[string]json.RawMessage {
	t.Helper()
	if !scanner.Scan() {
		t.Fatalf("read message: %v", scanner.Err())
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
		t.Fatal(err)
	}
	return message
}

func writeServerMessage(t *testing.T, writer io.Writer, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(writer, "%s\n", encoded); err != nil {
		t.Fatal(err)
	}
}

func receive[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for value")
		var zero T
		return zero
	}
}
