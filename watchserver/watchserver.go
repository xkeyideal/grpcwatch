package watchserver

import (
	"context"

	pb "github.com/xkeyideal/grpcwatch/watchpb"

	"go.uber.org/zap"
)

type WatchRpcServer struct {
	pb.UnimplementedWatchRPCServer

	lg *zap.Logger

	watcherStore *watcherStore
}

func NewWatchRpcServer(lg *zap.Logger, watcherStore *watcherStore) *WatchRpcServer {
	return &WatchRpcServer{
		lg:           lg,
		watcherStore: watcherStore,
	}
}

func (s *WatchRpcServer) GetAppServers(ctx context.Context, app *pb.App) (*pb.GetAppResponse, error) {
	return &pb.GetAppResponse{
		App: &pb.App{
			Name: "name",
			Env:  "qa",
		},
		Servers: []*pb.AppServer{
			&pb.AppServer{
				Ip:   "127.0.0.1",
				Port: "8080",
			},
		},
	}, nil
}

func (s *WatchRpcServer) Watch(stream pb.WatchRPC_WatchServer) error {

	var err error

	sws := &serverWatchStream{
		grpcStream:   stream,
		watcherStore: s.watcherStore,
		watchStream:  make(chan *pb.WatchResponse, 16),
		lg:           s.lg,
		closec:       make(chan struct{}),
	}

	sws.wg.Add(1)
	go func() {
		sws.sendLoop()
		sws.wg.Done()
	}()

	errc := make(chan error, 1)

	go func() {
		if rerr := sws.recvLoop(); rerr != nil {
			// 如果客户端主动断开连接，这里会记录日志
			// grpc 错误码为： code = Canceled, desc = context canceled
			if isClientCtxErr(stream.Context().Err(), rerr) {
				sws.lg.Error("isClientCtxErr", zap.String("err", rerr.Error()))
			}

			errc <- rerr
		}
	}()

	select {
	case err = <-errc:
		close(sws.watchStream)
	case <-stream.Context().Done():
		err = stream.Context().Err()
	}

	s.watcherStore.cancelWatch(sws.watchID)
	sws.close()
	return err
}
