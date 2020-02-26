package watchclient

import (
	"context"
	"time"

	pb "github.com/xkeyideal/grpcwatch/watchpb"

	"go.uber.org/zap"
	"google.golang.org/grpc"
)

var maxBackoff = 2000 * time.Millisecond

// GRPC stream管理
type watchGrpcStream struct {
	remote   pb.WatchRPCClient
	callOpts []grpc.CallOption

	watchID string

	// ctx controls internal remote.Watch requests
	ctx    context.Context
	cancel context.CancelFunc

	// 原始创建watch的请求, 主要用于断线重连
	initReq watchStreamRequest

	// reqc sends a watch request from Watch() to the main goroutine
	reqc chan watchStreamRequest

	// respc receives data from the watch client
	respc chan *pb.WatchResponse

	// donec closes to broadcast shutdown
	donec chan struct{}

	// errc transmits errors from grpc Recv to the watch stream reconnect logic
	errc chan error

	// 日志
	lg *zap.Logger
}

func (wgs *watchGrpcStream) run() {
	var wc pb.WatchRPC_WatchClient
	var closeErr error

	// 处理异常退出时，记录错误日志
	defer func() {
		if closeErr != nil {
			wgs.lg.Error("watch_grpc_stream client error closed", zap.String("watchID", wgs.watchID), zap.String("err", closeErr.Error()))
		}
	}()

	// 尝试连接服务端，若失败会不断的采用回退算法进行重试
	if wc, closeErr = wgs.newWatchClient(); closeErr != nil {
		return
	}

	for {
		select {
		case req := <-wgs.reqc:
			switch wreq := req.(type) {
			// watch request 创建处理
			case *watchCreateRequest:
				if err := wc.Send(wreq.toPB()); err != nil {
					wgs.lg.Error("createwatch", zap.String("watchID", wgs.watchID), zap.Any("request", wreq),
						zap.String("err", err.Error()))
				}
			// 取消watch request的处理
			case *watchCancelRequest:
				if err := wc.Send(wreq.toPB()); err != nil {
					wgs.lg.Error("cancelwatch", zap.String("watchID", wgs.watchID), zap.Any("request", wreq),
						zap.String("err", err.Error()))
				}
			}
		// watch client failed on Recv; spawn another if possible
		case err := <-wgs.errc:
			if isHaltErr(wgs.ctx, err) {
				closeErr = err
				return
			}

			// 重试
			if wc, closeErr = wgs.newWatchClient(); closeErr != nil {
				return
			}

			// 重试连接成功后，在此发送 watch request
			if err := wc.Send(wgs.initReq.(*watchCreateRequest).toPB()); err != nil {
				wgs.lg.Error("recreatewatch", zap.String("watchID", wgs.watchID), zap.Any("request", wgs.initReq),
					zap.String("err", err.Error()))
			}
		case <-wgs.ctx.Done():
			return
		case <-wgs.donec:
			return
		}
	}
}

func (wgs *watchGrpcStream) newWatchClient() (pb.WatchRPC_WatchClient, error) {
	wc, err := wgs.openWatchClient()
	if err != nil {
		return nil, err
	}

	// receive data from new grpc stream
	go wgs.serveWatchClient(wc)
	return wc, nil
}

// 接收服务端推送的数据
func (wgs *watchGrpcStream) serveWatchClient(wc pb.WatchRPC_WatchClient) {
	for {
		resp, err := wc.Recv()
		// 接收服务端数据的时候，发生错误，需要判断code，进行重试或断开处理
		if err != nil {
			select {
			case wgs.errc <- err:
			case <-wgs.donec:
			}
			return
		}
		select {
		// 放入channel，供业务逻辑消费
		case wgs.respc <- resp:
		case <-wgs.donec:
			return
		}
	}
}

// 开启创建与服务端的连接，并处理断线重连的问题
func (wgs *watchGrpcStream) openWatchClient() (pb.WatchRPC_WatchClient, error) {
	backoff := time.Millisecond
	retryTimes := 0
	for {
		select {
		case <-wgs.ctx.Done():
			return nil, wgs.ctx.Err()
		default:
		}
		ws, err := wgs.remote.Watch(wgs.ctx, wgs.callOpts...)
		if ws != nil && err == nil {
			return ws, nil
		}

		// 各种错误类型，判断是重连还是断开连接
		// 非网络错误，停止重连
		if isHaltErr(wgs.ctx, err) {
			return nil, err
		}

		// 只有网络不可达的时候才重连
		// TODO: 此处可以适当的修改，例如追加报警策略等，好让调用方显示的知晓，而非隐式的重试
		if isUnavailableErr(wgs.ctx, err) {
			// retry, but backoff
			if backoff < maxBackoff {
				// 25% backoff factor
				backoff = backoff + backoff/4
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			retryTimes++
			wgs.lg.Warn("watch internet unavailable", zap.String("watchID", wgs.watchID), zap.Int("retrytimes", retryTimes),
				zap.Int64("backoff", backoff.Milliseconds()))
			time.Sleep(backoff)
		}
	}
}

func (wgs *watchGrpcStream) close() {
	wgs.cancel()
	close(wgs.donec)

	wgs.lg.Info("watch_grpc_stream", zap.String(wgs.watchID, "close"))
}
