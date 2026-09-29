// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Implemented coordinated listeners and graceful shutdown for gRPC and JWKS.
// Author review: ZI YANG - verified correctness

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	jwksListenAddress = ":8085"
	shutdownTimeout   = 10 * time.Second
)

type listenFunc func(network, address string) (net.Listener, error)

type grpcLifecycle interface {
	Serve(net.Listener) error
	GracefulStop()
	Stop()
}

type httpLifecycle interface {
	Serve(net.Listener) error
	Shutdown(context.Context) error
	Close() error
}

type serveResult struct {
	name string
	err  error
}

func openServiceListeners(listen listenFunc, grpcPort string) (net.Listener, net.Listener, error) {
	grpcListener, err := listen("tcp", ":"+grpcPort)
	if err != nil {
		return nil, nil, fmt.Errorf("listen for gRPC: %w", err)
	}
	jwksListener, err := listen("tcp", jwksListenAddress)
	if err != nil {
		_ = grpcListener.Close()
		return nil, nil, fmt.Errorf("listen for JWKS HTTP: %w", err)
	}
	return grpcListener, jwksListener, nil
}

func serveServers(ctx context.Context, grpcServer grpcLifecycle, grpcListener net.Listener, jwksServer httpLifecycle, jwksListener net.Listener) error {
	results := make(chan serveResult, 2)
	go func() { results <- serveResult{name: "gRPC", err: grpcServer.Serve(grpcListener)} }()
	go func() { results <- serveResult{name: "JWKS HTTP", err: jwksServer.Serve(jwksListener)} }()

	select {
	case <-ctx.Done():
		return shutdownServers(grpcServer, jwksServer)
	case result := <-results:
		if ctx.Err() != nil {
			return shutdownServers(grpcServer, jwksServer)
		}
		serveErr := result.err
		if serveErr == nil {
			serveErr = errors.New("server stopped unexpectedly")
		}
		return errors.Join(
			fmt.Errorf("serve %s: %w", result.name, serveErr),
			shutdownServers(grpcServer, jwksServer),
		)
	}
}

func shutdownServers(grpcServer grpcLifecycle, jwksServer httpLifecycle) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	grpcStopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcStopped)
	}()

	httpErr := jwksServer.Shutdown(ctx)
	if httpErr != nil {
		httpErr = errors.Join(httpErr, jwksServer.Close())
	}

	select {
	case <-grpcStopped:
		return httpErr
	case <-ctx.Done():
		grpcServer.Stop()
		return errors.Join(httpErr, ctx.Err())
	}
}
