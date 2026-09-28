// Package devclient speaks the provisional local harness-owner binding for
// opted-in development use. It is not a public binding or a production adapter.
package devclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"
)

const (
	maxRequest  = 4096
	maxResponse = 65536
)

// Client addresses one independently running owner's private Unix socket.
// Token is an authenticated development caller, not a Duo subject grant.
type Client struct {
	Socket string
	Token  string
}

// Call returns the subject's raw result. It never retries a possibly admitted
// write, and does not translate an owner command into a Duo prompt command.
func (c Client) Call(ctx context.Context, operation string, fields map[string]any) (json.RawMessage, error) {
	request := map[string]any{
		"revision": "agent.harness/v0", "operation": operation, "token": c.Token,
	}
	for name, value := range fields {
		if _, reserved := request[name]; reserved {
			return nil, errors.New("private owner request overrides a reserved field")
		}
		request[name] = value
	}
	data, err := json.Marshal(request)
	if err != nil || len(data) > maxRequest-1 {
		return nil, errors.New("private owner request cannot be encoded within the binding limit")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Second)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	frame := append(data, '\n')
	n, err := conn.Write(frame)
	if err != nil {
		return nil, err
	}
	if n != len(frame) {
		return nil, io.ErrShortWrite
	}
	line, err := bufio.NewReader(io.LimitReader(conn, maxResponse+1)).ReadBytes('\n')
	if err != nil || len(line) > maxResponse {
		return nil, errors.New("private owner response incomplete or oversized")
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return nil, errors.New("private owner response is not JSON")
	}
	if len(envelope.Error) != 0 || len(envelope.Result) == 0 {
		return nil, errors.New("private owner returned no semantic result")
	}
	return envelope.Result, nil
}
