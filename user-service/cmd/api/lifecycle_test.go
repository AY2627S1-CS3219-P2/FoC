// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added lifecycle tests for coordinated gRPC and JWKS serving.
// Author review: ZI YANG - validated correctness

package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type listenerStub struct {
	closed atomic.Bool
}

func (*listenerStub) Accept() (net.Conn, error) { return nil, errors.New("not implemented") }
func (l *listenerStub) Close() error            { l.closed.Store(true); return nil }
func (*listenerStub) Addr() net.Addr            { return addrStub("test") }

type addrStub string

func (a addrStub) Network() string { return string(a) }
func (a addrStub) String() string  { return string(a) }

type grpcLifecycleStub struct {
	serveErr      error
	started       chan struct{}
	stopped       chan struct{}
	startOnce     sync.Once
	stopOnce      sync.Once
	gracefulCalls atomic.Int32
	forceCalls    atomic.Int32
}

func newGRPCLifecycleStub() *grpcLifecycleStub {
	return &grpcLifecycleStub{started: make(chan struct{}), stopped: make(chan struct{})}
}

func (s *grpcLifecycleStub) Serve(net.Listener) error {
	s.startOnce.Do(func() { close(s.started) })
	if s.serveErr != nil {
		return s.serveErr
	}
	<-s.stopped
	return nil
}

func (s *grpcLifecycleStub) GracefulStop() {
	s.gracefulCalls.Add(1)
	s.stopOnce.Do(func() { close(s.stopped) })
}

func (s *grpcLifecycleStub) Stop() {
	s.forceCalls.Add(1)
	s.stopOnce.Do(func() { close(s.stopped) })
}

type httpLifecycleStub struct {
	serveErr      error
	started       chan struct{}
	stopped       chan struct{}
	startOnce     sync.Once
	stopOnce      sync.Once
	shutdownCalls atomic.Int32
	closeCalls    atomic.Int32
}

func newHTTPLifecycleStub() *httpLifecycleStub {
	return &httpLifecycleStub{started: make(chan struct{}), stopped: make(chan struct{})}
}

func (s *httpLifecycleStub) Serve(net.Listener) error {
	s.startOnce.Do(func() { close(s.started) })
	if s.serveErr != nil {
		return s.serveErr
	}
	<-s.stopped
	return nil
}

func (s *httpLifecycleStub) Shutdown(context.Context) error {
	s.shutdownCalls.Add(1)
	s.stopOnce.Do(func() { close(s.stopped) })
	return nil
}

func (s *httpLifecycleStub) Close() error {
	s.closeCalls.Add(1)
	s.stopOnce.Do(func() { close(s.stopped) })
	return nil
}

func TestOpenServiceListenersUsesGRPCPortAndRecordedJWKSPort(t *testing.T) {
	var addresses []string
	listen := func(_ string, address string) (net.Listener, error) {
		addresses = append(addresses, address)
		return &listenerStub{}, nil
	}

	grpcListener, jwksListener, err := openServiceListeners(listen, "8081")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = grpcListener.Close()
		_ = jwksListener.Close()
	})
	if strings.Join(addresses, ",") != ":8081,:8085" {
		t.Fatalf("listen addresses = %v, want [:8081 :8085]", addresses)
	}
}

func TestOpenServiceListenersClosesGRPCListenerWhenJWKSBindFails(t *testing.T) {
	grpcListener := &listenerStub{}
	wantErr := errors.New("JWKS port already in use")
	calls := 0
	listen := func(_ string, _ string) (net.Listener, error) {
		calls++
		if calls == 1 {
			return grpcListener, nil
		}
		return nil, wantErr
	}

	_, _, err := openServiceListeners(listen, "8081")
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if !grpcListener.closed.Load() {
		t.Fatal("gRPC listener remained open after JWKS bind failure")
	}
}

func TestServeServersGracefullyStopsBothOnCancellation(t *testing.T) {
	grpcServer := newGRPCLifecycleStub()
	httpServer := newHTTPLifecycleStub()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-grpcServer.started
		<-httpServer.started
		cancel()
	}()

	err := serveServers(ctx, grpcServer, &listenerStub{}, httpServer, &listenerStub{})
	if err != nil {
		t.Fatalf("serveServers() error = %v", err)
	}
	if grpcServer.gracefulCalls.Load() != 1 || grpcServer.forceCalls.Load() != 0 {
		t.Fatalf("gRPC stop calls = graceful %d, force %d", grpcServer.gracefulCalls.Load(), grpcServer.forceCalls.Load())
	}
	if httpServer.shutdownCalls.Load() != 1 || httpServer.closeCalls.Load() != 0 {
		t.Fatalf("HTTP stop calls = shutdown %d, close %d", httpServer.shutdownCalls.Load(), httpServer.closeCalls.Load())
	}
}

func TestServeServersStopsPeerWhenOneServerFails(t *testing.T) {
	wantErr := errors.New("gRPC serve failed")
	grpcServer := newGRPCLifecycleStub()
	grpcServer.serveErr = wantErr
	httpServer := newHTTPLifecycleStub()

	err := serveServers(context.Background(), grpcServer, &listenerStub{}, httpServer, &listenerStub{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if grpcServer.gracefulCalls.Load() != 1 || httpServer.shutdownCalls.Load() != 1 {
		t.Fatalf("shutdown calls = gRPC %d, HTTP %d", grpcServer.gracefulCalls.Load(), httpServer.shutdownCalls.Load())
	}
}
