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
	watcherServer.CloseStream("watchertest")
	time.Sleep(2 * time.Second)
}
