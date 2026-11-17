package node

import (
	"context"
	"errors"
	"io"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/ruiange/sing-box-panel/internal/node/connectionwire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestSubscribeConnectionEvents(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	const createdAt = 1760000000123
	newEvent := connectionwire.ConnectionEventType_CONNECTION_EVENT_NEW
	updateEvent := connectionwire.ConnectionEventType_CONNECTION_EVENT_UPDATE
	closedEvent := connectionwire.ConnectionEventType_CONNECTION_EVENT_CLOSED
	const alphaID = "c9305b9f-ac4d-4a35-aa0d-46ab4ec0f16d"
	const betaID = "58de02e7-cc0c-4e4e-9945-3facda0fd7cc"
	event := func(kind connectionwire.ConnectionEventType, id, user, destination string, timestamp int64) *connectionwire.ConnectionEvent {
		return &connectionwire.ConnectionEvent{
			Type: kind,
			Connection: &connectionwire.Connection{
				Id: id, User: user, Destination: destination, CreatedAt: timestamp,
			},
		}
	}
	grpcServer := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		method, _ := grpc.MethodFromServerStream(stream)
		if method != "/daemon.StartedService/SubscribeConnections" {
			return status.Errorf(codes.Unimplemented, "unexpected method %q", method)
		}
		values := metadata.ValueFromIncomingContext(stream.Context(), "authorization")
		if len(values) != 1 || values[0] != "Bearer local-secret" {
			return status.Error(codes.Unauthenticated, "missing bearer authorization")
		}
		var request connectionwire.SubscribeConnectionsRequest
		if err := stream.RecvMsg(&request); err != nil {
			return err
		}
		if request.GetInterval() != int64(time.Second) {
			return status.Error(codes.InvalidArgument, "unexpected interval")
		}
		return stream.SendMsg(&connectionwire.ConnectionEvents{Events: []*connectionwire.ConnectionEvent{
			event(newEvent, alphaID, "device_alpha", "Example.COM:443", createdAt),
			event(updateEvent, alphaID, "device_alpha", "Example.COM:443", createdAt+1),
			event(closedEvent, betaID, "device_beta", "[2001:db8::42]:8443", createdAt+2),
			{Type: closedEvent},
			event(newEvent, alphaID, "internal", "localhost:10", createdAt),
			event(newEvent, alphaID, "device_", "example.com:443", createdAt),
			event(newEvent, alphaID, "device_wrong/id", "example.com:443", createdAt),
			event(newEvent, alphaID, "device_alpha", "", createdAt),
			event(newEvent, alphaID, "device_alpha", "example.com:443", 0),
			event(newEvent, "", "device_alpha", "example.com:443", createdAt),
		}})
	}))
	t.Cleanup(grpcServer.Stop)
	go grpcServer.Serve(listener)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var observed []ObservedConnection
	err = SubscribeConnectionEvents(ctx, listener.Addr().String(), "local-secret", func(batch []ObservedConnection) error {
		observed = append(observed, batch...)
		return nil
	})
	if !errors.Is(err, io.EOF) {
		t.Fatalf("event stream error: %v", err)
	}
	want := []ObservedConnection{
		{ConnectionID: alphaID, DeviceID: "alpha", Address: "Example.COM:443", SeenAt: time.UnixMilli(createdAt).UTC()},
		{ConnectionID: betaID, DeviceID: "beta", Address: "[2001:db8::42]:8443", SeenAt: time.UnixMilli(createdAt + 2).UTC()},
	}
	if !reflect.DeepEqual(observed, want) {
		t.Fatalf("observed %+v, want %+v", observed, want)
	}
}

func TestSubscribeConnectionEventsRequiresLocalAuthenticatedListener(t *testing.T) {
	consume := func([]ObservedConnection) error { return nil }
	for _, tc := range []struct{ address, secret string }{
		{"192.0.2.1:10086", "secret"},
		{"127.0.0.1:0", "secret"},
		{"127.0.0.1:10086", ""},
	} {
		if err := SubscribeConnectionEvents(context.Background(), tc.address, tc.secret, consume); err == nil {
			t.Fatalf("allowed invalid event listener %q", tc.address)
		}
	}
}
