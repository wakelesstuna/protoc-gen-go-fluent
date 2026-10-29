// Package fluent is the runtime used by code generated with protoc-gen-go-fluent.
//
// Generated clients embed *Conn, which holds the gRPC connection and a chain of
// context functions applied to every call (for metadata such as auth headers).
// The paging helpers drive the Page, All and Collect methods of generated queries.
package fluent

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ContextFunc decorates the context of every outgoing call, e.g. to add metadata.
type ContextFunc func(context.Context) context.Context

// Metadata returns a ContextFunc that appends fixed key/value pairs to the outgoing metadata.
func Metadata(kv ...string) ContextFunc {
	return func(ctx context.Context) context.Context {
		return metadata.AppendToOutgoingContext(ctx, kv...)
	}
}

// Conn is a gRPC connection plus per-call context functions. It is safe for concurrent use.
type Conn struct {
	cc    grpc.ClientConnInterface
	owned *grpc.ClientConn // closed by Close; nil when the caller owns the connection
	fns   []ContextFunc
}

// Dial opens a connection to target with grpc.NewClient. Close it when done.
// Pass transport credentials in opts, e.g. grpc.WithTransportCredentials(credentials.NewTLS(nil)).
func Dial(target string, opts ...grpc.DialOption) (*Conn, error) {
	cc, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, err
	}
	return &Conn{cc: cc, owned: cc}, nil
}

// NewConn wraps an existing connection. The caller keeps ownership; Close is a no-op.
func NewConn(cc grpc.ClientConnInterface) *Conn {
	return &Conn{cc: cc}
}

// With returns a Conn sharing c's connection that also applies fns to every call,
// after c's own functions. Closing the returned Conn does not close the connection.
func (c *Conn) With(fns ...ContextFunc) *Conn {
	return &Conn{
		cc:  c.cc,
		fns: append(c.fns[:len(c.fns):len(c.fns)], fns...),
	}
}

// ClientConn returns the underlying connection, for constructing generated gRPC stubs.
func (c *Conn) ClientConn() grpc.ClientConnInterface {
	return c.cc
}

// Outgoing applies the context functions to ctx. Generated code calls it for every request.
func (c *Conn) Outgoing(ctx context.Context) context.Context {
	for _, fn := range c.fns {
		ctx = fn(ctx)
	}
	return ctx
}

// Close closes the connection if Dial created it.
func (c *Conn) Close() error {
	if c.owned == nil {
		return nil
	}
	return c.owned.Close()
}
