package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// JSON-RPC 2.0 types
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Notification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type Handler func(params json.RawMessage) (interface{}, error)
type SubscribeHandler func(params json.RawMessage, send func(method string, data interface{}) error, done <-chan struct{}) error

type Server struct {
	socketPath string
	listener   net.Listener
	handlers   map[string]Handler
	subHandler SubscribeHandler
	mu         sync.Mutex
	done       chan struct{}
	wg         sync.WaitGroup
}

func NewServer(socketPath string) *Server {
	return &Server{
		socketPath: socketPath,
		handlers:   make(map[string]Handler),
		done:       make(chan struct{}),
	}
}

func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

func (s *Server) HandleSubscribe(h SubscribeHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subHandler = h
}

func (s *Server) Start() error {
	dir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}

	// Remove stale socket
	os.Remove(s.socketPath)

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	if err := os.Chmod(s.socketPath, 0600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	s.listener = ln

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop()
	}()

	return nil
}

func (s *Server) Stop() error {
	close(s.done)
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.wg.Wait()
	os.Remove(s.socketPath)
	return nil
}

func (s *Server) SocketPath() string {
	return s.socketPath
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				log.Printf("ipc: accept error: %v", err)
				continue
			}
		}

		if err := s.verifyPeer(conn); err != nil {
			log.Printf("ipc: peer rejected: %v", err)
			conn.Close()
			continue
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

func (s *Server) verifyPeer(conn net.Conn) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("not a unix connection")
	}

	raw, err := uc.SyscallConn()
	if err != nil {
		return fmt.Errorf("syscall conn: %w", err)
	}

	var cred *unix.Ucred
	var credErr error
	err = raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil {
		return fmt.Errorf("control: %w", err)
	}
	if credErr != nil {
		return fmt.Errorf("getsockopt: %w", credErr)
	}

	myUID := uint32(os.Getuid())
	if cred.Uid != myUID {
		return fmt.Errorf("UID mismatch: peer=%d daemon=%d", cred.Uid, myUID)
	}

	return nil
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	connDone := make(chan struct{})
	defer close(connDone)

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB max message

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			if err := s.writeResponse(conn, Response{
				JSONRPC: "2.0",
				Error:   &RPCError{Code: -32700, Message: "parse error"},
			}); err != nil {
				log.Printf("ipc: write error: %v", err)
				return
			}
			continue
		}

		if req.Method == "audit.subscribe" {
			s.handleSubscribe(conn, req, connDone)
			return // subscribe takes over the connection
		}

		s.mu.Lock()
		handler, ok := s.handlers[req.Method]
		s.mu.Unlock()

		if !ok {
			if err := s.writeResponse(conn, Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &RPCError{Code: -32601, Message: "method not found"},
			}); err != nil {
				log.Printf("ipc: write error: %v", err)
				return
			}
			continue
		}

		result, err := handler(req.Params)
		if err != nil {
			if err := s.writeResponse(conn, Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &RPCError{Code: -32000, Message: err.Error()},
			}); err != nil {
				log.Printf("ipc: write error: %v", err)
				return
			}
			continue
		}

		if err := s.writeResponse(conn, Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}); err != nil {
			log.Printf("ipc: write error: %v", err)
			return
		}
	}
}

func (s *Server) handleSubscribe(conn net.Conn, req Request, connDone chan struct{}) {
	s.mu.Lock()
	handler := s.subHandler
	s.mu.Unlock()

	if handler == nil {
		if err := s.writeResponse(conn, Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32601, Message: "subscribe not configured"},
		}); err != nil {
			log.Printf("ipc: write error: %v", err)
		}
		return
	}

	// Send ack
	if err := s.writeResponse(conn, Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"ok": true},
	}); err != nil {
		log.Printf("ipc: write error: %v", err)
		return
	}

	sendFn := func(method string, data interface{}) error {
		notif := Notification{
			JSONRPC: "2.0",
			Method:  method,
			Params:  data,
		}
		return s.writeResponse(conn, notif)
	}

	// Merge done signals
	merged := make(chan struct{})
	go func() {
		select {
		case <-s.done:
			close(merged)
		case <-connDone:
			close(merged)
		}
	}()

	// Start heartbeat
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-merged:
				return
			case <-ticker.C:
				if err := sendFn("audit.heartbeat", map[string]string{
					"ts": time.Now().UTC().Format(time.RFC3339),
				}); err != nil {
					return
				}
			}
		}
	}()

	if err := handler(req.Params, sendFn, merged); err != nil {
		log.Printf("ipc: subscribe handler: %v", err)
	}
}

func (s *Server) writeResponse(conn net.Conn, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = conn.Write(data)
	return err
}
