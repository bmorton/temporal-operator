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
	"io"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Handler relays gRPC calls to the source/target backends per the Director.
type Handler struct {
	mu       sync.RWMutex
	Director Director
	Source   *grpc.ClientConn
	Target   *grpc.ClientConn
}

// SetMode atomically updates the routing mode (used on config reload).
func (h *Handler) SetMode(m Mode) {
	h.mu.Lock()
	h.Director.Mode = m
	h.mu.Unlock()
}

func (h *Handler) conn(b Backend) *grpc.ClientConn {
	switch b {
	case BackendSource:
		return h.Source
	case BackendTarget:
		return h.Target
	default:
		return nil
	}
}

// Stream is the gRPC UnknownServiceHandler entrypoint.
func (h *Handler) Stream(_ any, stream grpc.ServerStream) error {
	method, ok := grpc.MethodFromServerStream(stream)
	if !ok {
		return status.Error(codes.Internal, "proxy: no method in stream")
	}
	h.mu.RLock()
	director := h.Director
	h.mu.RUnlock()

	class := Classify(method)
	primary, fallback := director.Route(class)

	if class == ClassPoll {
		return h.pipe(stream, h.conn(primary), method)
	}
	return h.unary(stream, method, primary, fallback)
}

// unary buffers one request frame, invokes the primary backend, and retries the
// fallback backend on NotFound.
func (h *Handler) unary(stream grpc.ServerStream, method string, primary, fallback Backend) error {
	var req Frame
	if err := stream.RecvMsg(&req); err != nil {
		if err == io.EOF {
			return status.Error(codes.Internal, "proxy: empty request")
		}
		return err
	}
	outCtx := forwardContext(stream.Context())

	resp, header, trailer, err := invoke(outCtx, h.conn(primary), method, &req)
	if err != nil && fallback != BackendNone && status.Code(err) == codes.NotFound {
		resp, header, trailer, err = invoke(outCtx, h.conn(fallback), method, &req)
	}
	if len(header) > 0 {
		_ = stream.SetHeader(header)
	}
	if len(trailer) > 0 {
		stream.SetTrailer(trailer)
	}
	if err != nil {
		return err
	}
	return stream.SendMsg(resp)
}

func invoke(ctx context.Context, conn *grpc.ClientConn, method string, req *Frame) (*Frame, metadata.MD, metadata.MD, error) {
	resp := &Frame{}
	var header, trailer metadata.MD
	err := conn.Invoke(ctx, method, req, resp,
		grpc.ForceCodec(RawCodec{}), grpc.Header(&header), grpc.Trailer(&trailer))
	return resp, header, trailer, err
}

// pipe transparently relays a (possibly streaming) call to a single backend.
func (h *Handler) pipe(stream grpc.ServerStream, conn *grpc.ClientConn, method string) error {
	outCtx, cancel := context.WithCancel(forwardContext(stream.Context()))
	defer cancel()
	clientStream, err := conn.NewStream(outCtx,
		&grpc.StreamDesc{ServerStreams: true, ClientStreams: true}, method, grpc.ForceCodec(RawCodec{}))
	if err != nil {
		return err
	}
	errc := make(chan error, 2)
	go func() {
		for {
			var f Frame
			if err := stream.RecvMsg(&f); err != nil {
				if err == io.EOF {
					if closeErr := clientStream.CloseSend(); closeErr != nil {
						errc <- closeErr
					}
					return
				}
				errc <- err
				return
			}
			if err := clientStream.SendMsg(&f); err != nil {
				errc <- err
				return
			}
		}
	}()
	go func() {
		if md, err := clientStream.Header(); err != nil {
			errc <- err
			return
		} else if len(md) > 0 {
			_ = stream.SetHeader(md)
		}
		for {
			var f Frame
			if err := clientStream.RecvMsg(&f); err != nil {
				if err == io.EOF {
					stream.SetTrailer(clientStream.Trailer())
					errc <- nil
					return
				}
				errc <- err
				return
			}
			if err := stream.SendMsg(&f); err != nil {
				errc <- err
				return
			}
		}
	}()
	err = <-errc
	return err
}

// forwardContext copies inbound metadata onto an outbound context.
func forwardContext(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	return metadata.NewOutgoingContext(ctx, md.Copy())
}
