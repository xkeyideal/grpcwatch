package main

import (
	"context"
	"fmt"
	"time"

	pb "github.com/xkeyideal/grpcwatch/watchpb"

	"github.com/xkeyideal/grpcwatch/grpclient"
	"github.com/xkeyideal/grpcwatch/watchclient"

	"go.uber.org/zap/zapcore"
)

func main() {
	cfg := &grpclient.GrpcClientConfig{
		Endpoints:            []string{"127.0.0.1:5853"},
		DialTimeout:          2 * time.Second,
		DialKeepAliveTime:    5 * time.Second,
		DialKeepAliveTimeout: 1 * time.Second,
		PermitWithoutStream:  true,
	}

	client, err := grpclient.NewGRPCClient(cfg)
	if err != nil {
		fmt.Println("new client:", err)
		return
	}

	defer client.Close()

	app := &pb.App{
		Name: "1122",
		Env:  "qa",
	}

	queryServer := watchclient.NewAppServer(client)

	resp, err := queryServer.GetAppServers(app)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(resp)
	}

	watcherServer := watchclient.NewWatcher("", zapcore.DebugLevel, client)
	ch := watcherServer.Watch(context.Background(), "watchertest", app)

	go func() {
		created := false
		for {
			select {
			case resp := <-ch:
				fmt.Println(time.Now(), resp)
				// canceled分为两种情况
				// 1. 客户端出现异常，处理方案在watchclient/watch_grpc_stream.go 的 run() defer中，此时会主动调用closeStream退出watch steam
				// 2. 客户端主动发起CancelRequest退出，告知服务端，服务端响应canceled，此情况是发生在客户端调用CloseStream退出watch steam
				if resp.Canceled {
					fmt.Println("server canceled", resp.CancelReason)
					return
				}

				if !created {
					if !resp.Created {
						fmt.Println("server created failed", resp.CancelReason)
						return
					}
					created = true
				}
			}
		}
	}()

	time.Sleep(5 * time.Second)
	// CloseStream做了watchID是否存在的判断
	// 当出现canceled的case 1时，也不会因为close(wgs.donec)两次出现panic
	watcherServer.CloseStream("watchertest")
	time.Sleep(2 * time.Second)
}
