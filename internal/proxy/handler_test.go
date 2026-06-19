/*
Copyright 2026 Brian Morton.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package proxy

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const (
	sourceBackendName = "source"
	targetBackendName = "target"
)

// fakeBackend echoes its name, or returns NotFound when notFound is true.
type fakeBackend struct {
	name     string
	notFound bool
	delay    time.Duration
}

func (b *fakeBackend) handler(srv any, stream grpc.ServerStream) error {
	var in Frame
	if err := stream.RecvMsg(&in); err != nil {
		return err
	}
	if b.notFound {
		return status.Error(codes.NotFound, "workflow not found")
	}
	if b.delay > 0 {
		time.Sleep(b.delay)
	}
	return stream.SendMsg(&Frame{Payload: []byte(b.name)})
}

func startFake(t *testing.T, b *fakeBackend) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.ForceServerCodec(RawCodec{}), grpc.UnknownServiceHandler(b.handler))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestHandlerFallbackOnNotFound(t *testing.T) {
	target := &fakeBackend{name: targetBackendName, notFound: true}
	source := &fakeBackend{name: sourceBackendName}
	h := &Handler{Director: Director{Mode: ModeCutover}, Source: startFake(t, source), Target: startFake(t, target)}
	resp := callThroughProxy(t, h, "/temporal.api.workflowservice.v1.WorkflowService/SignalWorkflowExecution", []byte("req"))
	if string(resp) != sourceBackendName {
		t.Fatalf("response = %q, want source (fallback)", resp)
	}
}

func TestHandlerStartGoesToTarget(t *testing.T) {
	target := &fakeBackend{name: targetBackendName}
	source := &fakeBackend{name: sourceBackendName}
	h := &Handler{Director: Director{Mode: ModeCutover}, Source: startFake(t, source), Target: startFake(t, target)}
	resp := callThroughProxy(t, h, "/temporal.api.workflowservice.v1.WorkflowService/StartWorkflowExecution", []byte("req"))
	if string(resp) != targetBackendName {
		t.Fatalf("response = %q, want target", resp)
	}
}

func TestHandlerPassthroughGoesToSource(t *testing.T) {
	target := &fakeBackend{name: targetBackendName}
	source := &fakeBackend{name: sourceBackendName}
	h := &Handler{Director: Director{Mode: ModePassthrough}, Source: startFake(t, source), Target: startFake(t, target)}
	resp := callThroughProxy(t, h, "/temporal.api.workflowservice.v1.WorkflowService/StartWorkflowExecution", []byte("req"))
	if string(resp) != sourceBackendName {
		t.Fatalf("response = %q, want source", resp)
	}
}

func TestHandlerPollPipesResponse(t *testing.T) {
	target := &fakeBackend{name: targetBackendName}
	source := &fakeBackend{name: sourceBackendName, delay: 20 * time.Millisecond}
	h := &Handler{Director: Director{Mode: ModeCutover}, Source: startFake(t, source), Target: startFake(t, target)}
	resp := callThroughProxy(t, h, "/temporal.api.workflowservice.v1.WorkflowService/PollWorkflowTaskQueue", []byte("req"))
	if string(resp) != sourceBackendName {
		t.Fatalf("response = %q, want source", resp)
	}
}

// callThroughProxy runs the proxy handler as a server and makes one raw unary call.
func callThroughProxy(t *testing.T, h *Handler, method string, req []byte) []byte {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.ForceServerCodec(RawCodec{}), grpc.UnknownServiceHandler(h.Stream))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	out := &Frame{}
	if err := conn.Invoke(context.Background(), method, &Frame{Payload: req}, out, grpc.ForceCodec(RawCodec{})); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	return out.Payload
}
