package watchserver

import (
	"fmt"
	"math"
	"net"
	"time"

	pb "github.com/xkeyideal/grpcwatch/watchpb"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

const (
	grpcOverheadBytes = 512 * 1024
	maxStreams        = math.MaxUint32
	maxSendBytes      = math.MaxInt32
)

type GrpcServerConfig struct {
	Port                  uint
	MaxConnectionIdle     uint32
	PingInterval          uint32
	Timeout               uint32
	KeepAliveMinTime      int
	MaxConnectionAge      uint32
	MaxConnectionAgeGrace uint32
	WriteBufferSize       int
	ReadBufferSize        int
	MaxRecvMsgSize        int
	MaxSendMsgSize        int
	MaxConcurrentStreams  uint32
}

func NewGrpcServer(cfg *GrpcServerConfig, lg *zap.Logger) error {
	gopts := []grpc.ServerOption{}

	var kaep = keepalive.EnforcementPolicy{
		MinTime:             time.Duration(cfg.KeepAliveMinTime) * time.Second, // If a client pings more than once every 5 seconds, terminate the connection
		PermitWithoutStream: true,                                              // Allow pings even when there are no active streams
	}

	var kasp = keepalive.ServerParameters{
		MaxConnectionIdle:     time.Duration(cfg.MaxConnectionIdle) * time.Second,     // 如果客户端30s不进行通讯，则断开链接
		MaxConnectionAge:      time.Duration(cfg.MaxConnectionAge) * time.Second,      // 设置连接最长时间，默认为无限
		MaxConnectionAgeGrace: time.Duration(cfg.MaxConnectionAgeGrace) * time.Second, // Allow 5 seconds for pending RPCs to complete before forcibly closing connections
		Time:                  time.Duration(cfg.PingInterval) * time.Second,          // Ping the client if it is idle for 5 seconds to ensure the connection is still active
		Timeout:               time.Duration(cfg.Timeout) * time.Second,               // Wait 5 second for the ping ack before assuming the connection is dead
	}

	gopts = append(gopts, grpc.KeepaliveEnforcementPolicy(kaep), grpc.KeepaliveParams(kasp))

	// 单位都是Byte
	gopts = append(gopts, grpc.WriteBufferSize(cfg.WriteBufferSize))
	gopts = append(gopts, grpc.ReadBufferSize(cfg.ReadBufferSize))
	gopts = append(gopts, grpc.MaxRecvMsgSize(cfg.MaxRecvMsgSize))
	gopts = append(gopts, grpc.MaxSendMsgSize(cfg.MaxSendMsgSize))
	gopts = append(gopts, grpc.MaxConcurrentStreams(cfg.MaxConcurrentStreams))

	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", cfg.Port))
	if err != nil {
		return err
	}

	server := grpc.NewServer(gopts...)

	s := NewWatchRpcServer(lg, newWatcherStore())

	pb.RegisterWatchRPCServer(server, s)

	return server.Serve(listener)
}
