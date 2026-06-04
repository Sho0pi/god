package socket

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/sho0pi/god/internal/connector"
)

// Server is the gateway-side socket connector. It listens on a Unix socket and
// treats each accepted connection as an independent chat session: inbound lines
// become connector.Messages, and agent replies (via Send) are written back to
// the connection that owns the destination chat.
type Server struct {
	path    string
	handler func(ctx context.Context, msg connector.Message)

	listener net.Listener
	nextID   atomic.Uint64

	mu    sync.RWMutex
	conns map[string]*clientConn // chatID → connection
}

// clientConn is one accepted client connection plus a write mutex (Send may be
// called concurrently with the read loop).
type clientConn struct {
	net  net.Conn
	wmu  sync.Mutex
	enc  *json.Encoder
	user string
}

// NewServer builds a socket Server bound to path.
func NewServer(path string) *Server {
	return &Server{path: path, conns: make(map[string]*clientConn)}
}

func (s *Server) SetMessageHandler(handler func(ctx context.Context, msg connector.Message)) {
	s.handler = handler
}

// Start removes any stale socket file, binds the listener, and serves accepts
// in the background until ctx is cancelled (handled by Stop).
func (s *Server) Start(ctx context.Context) error {
	// A leftover socket file from a crashed gateway would make Listen fail with
	// "address already in use"; remove it first. (Safe: it is a socket, not data.)
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("socket: remove stale %s: %w", s.path, err)
	}
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("socket: listen %s: %w", s.path, err)
	}
	s.listener = ln
	slog.Info("socket: listening", "path", s.path)

	go s.acceptLoop(ctx)
	return nil
}

func (s *Server) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Accept fails permanently once the listener is closed by Stop.
			if ctx.Err() != nil {
				return
			}
			slog.Error("socket: accept", "err", err)
			return
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	id := "cli:" + strconv.FormatUint(s.nextID.Add(1), 10)
	cc := &clientConn{net: conn, enc: json.NewEncoder(conn)}

	s.mu.Lock()
	s.conns[id] = cc
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, id)
		s.mu.Unlock()
		_ = conn.Close()
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var f clientFrame
		if err := json.Unmarshal(scanner.Bytes(), &f); err != nil {
			slog.Warn("socket: bad frame", "id", id, "err", err)
			continue
		}
		user := f.User
		if user == "" {
			user = "local"
		}
		cc.user = user
		if s.handler == nil {
			continue
		}
		s.handler(ctx, connector.Message{
			Connector: connectorName,
			UserID:    user,
			ChatID:    id,
			SenderID:  user,
			Text:      f.Text,
		})
	}
}

// Send writes a reply to the connection that owns chatID.
func (s *Server) Send(_ context.Context, chatID, text string) error {
	s.mu.RLock()
	cc := s.conns[chatID]
	s.mu.RUnlock()
	if cc == nil {
		return fmt.Errorf("socket: no client for chat %q", chatID)
	}
	cc.wmu.Lock()
	defer cc.wmu.Unlock()
	return cc.enc.Encode(serverFrame{Text: text})
}

// Stop closes the listener (ending the accept loop) and removes the socket file.
func (s *Server) Stop(_ context.Context) error {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
