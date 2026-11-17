package node

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ruiange/sing-box-panel/internal/node/connectionwire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const (
	connectionEventsMethod = "/daemon.StartedService/SubscribeConnections"
	connectionEventsLimit  = 8 << 20
)

type ObservedConnection struct {
	ConnectionID string
	DeviceID     string
	Address      string
	SeenAt       time.Time
}

// SubscribeConnectionEvents reads the native sing-box connection stream until
// cancellation or an error. The caller owns reconnects and persistence.
func SubscribeConnectionEvents(ctx context.Context, address, secret string, consume func([]ObservedConnection) error) error {
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil || !endpoint.Addr().IsLoopback() || endpoint.Port() == 0 {
		return errors.New("sing-box 连接事件接口必须使用本机回环地址和有效端口")
	}
	if secret == "" || consume == nil {
		return errors.New("sing-box 连接事件流需要访问凭据和处理函数")
	}
	client, err := grpc.NewClient("passthrough:///"+endpoint.String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(connectionEventsLimit)),
	)
	if err != nil {
		return fmt.Errorf("连接 sing-box 事件接口: %w", err)
	}
	defer client.Close()

	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+secret)
	stream, err := client.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, connectionEventsMethod)
	if err != nil {
		return fmt.Errorf("订阅 sing-box 连接事件: %w", err)
	}
	if err := stream.SendMsg(&connectionwire.SubscribeConnectionsRequest{Interval: int64(time.Second)}); err != nil {
		return fmt.Errorf("发送 sing-box 连接订阅请求: %w", err)
	}
	if err := stream.CloseSend(); err != nil {
		return fmt.Errorf("结束 sing-box 连接订阅请求: %w", err)
	}

	for {
		var message connectionwire.ConnectionEvents
		if err := stream.RecvMsg(&message); err != nil {
			return fmt.Errorf("读取 sing-box 连接事件: %w", err)
		}
		connections := observedConnections(message.GetEvents())
		if len(connections) != 0 {
			if err := consume(connections); err != nil {
				return err
			}
		}
	}
}

func observedConnections(events []*connectionwire.ConnectionEvent) []ObservedConnection {
	var observed []ObservedConnection
	for _, event := range events {
		switch event.GetType() {
		case connectionwire.ConnectionEventType_CONNECTION_EVENT_NEW,
			connectionwire.ConnectionEventType_CONNECTION_EVENT_CLOSED:
		default:
			continue
		}
		connection := event.GetConnection()
		deviceID, isDevice := strings.CutPrefix(connection.GetUser(), counterUserPrefix)
		if !isDevice || !deviceIDPattern.MatchString(deviceID) || connection.GetDestination() == "" || connection.GetCreatedAt() <= 0 {
			continue
		}
		connectionID, err := uuid.Parse(connection.GetId())
		if err != nil || connectionID == uuid.Nil {
			continue
		}
		observed = append(observed, ObservedConnection{
			ConnectionID: connectionID.String(),
			DeviceID:     deviceID,
			Address:      connection.GetDestination(),
			// Replay and closure events refer to the original connection time.
			SeenAt: time.UnixMilli(connection.GetCreatedAt()).UTC(),
		})
	}
	return observed
}
