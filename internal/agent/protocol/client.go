package protocol

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
)

var (
	ErrClosed     = errors.New("app-server protocol client is closed")
	ErrProtocol   = errors.New("app-server protocol error")
	ErrOverloaded = errors.New("app-server protocol queue is overloaded")
)

const (
	defaultMaxMessageBytes = 2 << 20
	defaultQueueSize       = 128
)

type Options struct {
	MaxMessageBytes int
	QueueSize       int
}

type callResult struct {
	result json.RawMessage
	err    error
}

type Client struct {
	input  io.WriteCloser
	output io.ReadCloser

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]chan callResult
	err     error
	closed  bool

	notifications  chan Notification
	serverRequests chan ServerRequest
	done           chan struct{}
	closeOnce      sync.Once
	options        Options
}

func NewClient(input io.WriteCloser, output io.ReadCloser, options Options) *Client {
	if options.MaxMessageBytes <= 0 {
		options.MaxMessageBytes = defaultMaxMessageBytes
	}
	if options.QueueSize <= 0 {
		options.QueueSize = defaultQueueSize
	}
	client := &Client{
		input:          input,
		output:         output,
		pending:        map[uint64]chan callResult{},
		notifications:  make(chan Notification, options.QueueSize),
		serverRequests: make(chan ServerRequest, options.QueueSize),
		done:           make(chan struct{}),
		options:        options,
	}
	go client.readLoop()
	return client
}

func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal %s params: %w", method, err)
	}

	c.mu.Lock()
	if c.closed {
		err := c.closedErrorLocked()
		c.mu.Unlock()
		return err
	}
	c.nextID++
	id := c.nextID
	response := make(chan callResult, 1)
	c.pending[id] = response
	c.mu.Unlock()

	request := struct {
		ID     uint64          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params,omitempty"`
	}{ID: id, Method: method, Params: paramsJSON}
	if err := c.write(ctx, request); err != nil {
		c.removePending(id)
		return err
	}

	select {
	case reply := <-response:
		if reply.err != nil {
			return reply.err
		}
		if result == nil || len(reply.result) == 0 || string(reply.result) == "null" {
			return nil
		}
		if err := json.Unmarshal(reply.result, result); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
}

func (c *Client) Notify(ctx context.Context, method string, params any) error {
	return c.write(ctx, struct {
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{Method: method, Params: params})
}

func (c *Client) Respond(ctx context.Context, id json.RawMessage, result any, rpcErr *RPCError) error {
	if len(id) == 0 || !json.Valid(id) {
		return fmt.Errorf("%w: invalid response id", ErrProtocol)
	}
	message := struct {
		ID     json.RawMessage `json:"id"`
		Result any             `json:"result,omitempty"`
		Error  *RPCError       `json:"error,omitempty"`
	}{ID: append(json.RawMessage(nil), id...), Result: result, Error: rpcErr}
	return c.write(ctx, message)
}

func (c *Client) Notifications() <-chan Notification   { return c.notifications }
func (c *Client) ServerRequests() <-chan ServerRequest { return c.serverRequests }
func (c *Client) Done() <-chan struct{}                { return c.done }

func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closedErrorLocked()
}

func (c *Client) Close() error {
	c.finish(nil)
	return nil
}

func (c *Client) write(ctx context.Context, message any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	closed := c.closed
	closedErr := c.closedErrorLocked()
	c.mu.Unlock()
	if closed {
		return closedErr
	}
	if _, err := c.input.Write(encoded); err != nil {
		c.finish(err)
		return err
	}
	return nil
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.output)
	initialBuffer := 64 << 10
	if c.options.MaxMessageBytes < initialBuffer {
		initialBuffer = c.options.MaxMessageBytes
	}
	scanner.Buffer(make([]byte, initialBuffer), c.options.MaxMessageBytes)
	for scanner.Scan() {
		if err := c.dispatch(scanner.Bytes()); err != nil {
			c.finish(err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		c.finish(fmt.Errorf("%w: read message: %v", ErrProtocol, err))
		return
	}
	c.finish(io.EOF)
}

func (c *Client) dispatch(line []byte) error {
	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return fmt.Errorf("%w: decode message: %v", ErrProtocol, err)
	}
	if envelope.Method != "" {
		if len(envelope.ID) > 0 {
			request := ServerRequest{ID: append(json.RawMessage(nil), envelope.ID...), Method: envelope.Method, Params: append(json.RawMessage(nil), envelope.Params...)}
			select {
			case c.serverRequests <- request:
				return nil
			default:
				return ErrOverloaded
			}
		}
		notification := Notification{Method: envelope.Method, Params: append(json.RawMessage(nil), envelope.Params...)}
		select {
		case c.notifications <- notification:
			return nil
		default:
			return ErrOverloaded
		}
	}
	if len(envelope.ID) == 0 {
		return fmt.Errorf("%w: message has neither method nor id", ErrProtocol)
	}
	id, err := strconv.ParseUint(string(envelope.ID), 10, 64)
	if err != nil {
		return fmt.Errorf("%w: response id is not numeric", ErrProtocol)
	}
	c.mu.Lock()
	pending := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if pending == nil {
		return nil
	}
	if envelope.Error != nil {
		pending <- callResult{err: envelope.Error}
	} else {
		pending <- callResult{result: append(json.RawMessage(nil), envelope.Result...)}
	}
	return nil
}

func (c *Client) removePending(id uint64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) finish(err error) {
	c.closeOnce.Do(func() {
		_ = c.input.Close()
		_ = c.output.Close()
		c.mu.Lock()
		c.closed = true
		c.err = err
		pending := c.pending
		c.pending = map[uint64]chan callResult{}
		c.mu.Unlock()
		for _, response := range pending {
			response <- callResult{err: c.closedError(err)}
		}
		close(c.done)
	})
}

func (c *Client) closedErrorLocked() error { return c.closedError(c.err) }

func (c *Client) closedError(err error) error {
	if err == nil {
		return ErrClosed
	}
	if errors.Is(err, ErrProtocol) || errors.Is(err, ErrOverloaded) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrClosed, err)
}
