package linkcodes

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// Redis is a minimal RESP client: GET and SET ... EX, one short connection per call. The link page
// makes a handful of calls a minute, so there is no pool to get wrong.
type Redis struct {
	Addr     string
	Password string
	Timeout  time.Duration
}

func (r *Redis) do(ctx context.Context, args ...string) (any, error) {
	d := net.Dialer{Timeout: r.timeout()}
	conn, err := d.DialContext(ctx, "tcp", r.Addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(r.timeout()))
	br := bufio.NewReader(conn)
	if r.Password != "" {
		if _, err := roundTrip(conn, br, "AUTH", r.Password); err != nil {
			return nil, err
		}
	}
	return roundTrip(conn, br, args...)
}

func (r *Redis) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return 3 * time.Second
}

func roundTrip(w io.Writer, br *bufio.Reader, args ...string) (any, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return nil, err
	}
	return readReply(br)
}

func readReply(br *bufio.Reader) (any, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil, errors.New("redis: empty reply")
	}
	switch line[0] {
	case '+':
		return line[1:], nil
	case '-':
		return nil, errors.New("redis: " + line[1:])
	case ':':
		return strconv.ParseInt(line[1:], 10, 64)
	case '$':
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(br, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	}
	return nil, fmt.Errorf("redis: unexpected reply %q", line)
}

// Get implements KV.
func (r *Redis) Get(ctx context.Context, key string) (string, bool, error) {
	v, err := r.do(ctx, "GET", key)
	if err != nil {
		return "", false, err
	}
	s, ok := v.(string)
	return s, ok, nil
}

// Set implements KV.
func (r *Redis) Set(ctx context.Context, key, value string, ex int) error {
	_, err := r.do(ctx, "SET", key, value, "EX", strconv.Itoa(ex))
	return err
}

// Memory is a KV in memory, for tests and the demo.
type Memory struct{ m map[string]string }

// NewMemory returns an empty Memory.
func NewMemory() *Memory { return &Memory{m: map[string]string{}} }

// Get implements KV.
func (m *Memory) Get(_ context.Context, k string) (string, bool, error) {
	v, ok := m.m[k]
	return v, ok, nil
}

// Set implements KV.
func (m *Memory) Set(_ context.Context, k, v string, _ int) error { m.m[k] = v; return nil }
