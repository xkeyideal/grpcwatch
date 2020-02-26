package watchclient

import (
	"context"

	"github.com/xkeyideal/grpcwatch/grpclient"
	pb "github.com/xkeyideal/grpcwatch/watchpb"

	"google.golang.org/grpc"
)

type AppServer struct {
	remote   pb.WatchRPCClient
	callOpts []grpc.CallOption
}

func NewAppServer(c *grpclient.GrpcClient) *AppServer {
	s := &AppServer{
		remote: pb.NewWatchRPCClient(c.Conn),
	}

	if c != nil {
		s.callOpts = c.GetCallOpts()
	}

	return s
}

func (s *AppServer) GetAppServers(app *pb.App) (*pb.GetAppResponse, error) {
	return s.remote.GetAppServers(context.Background(), app, s.callOpts...)
}
