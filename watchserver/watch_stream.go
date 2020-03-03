package watchserver

import (
	"io"
	"strings"
	"sync"

	pb "github.com/xkeyideal/grpcwatch/watchpb"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type serverWatchStream struct {
	watchID string

	grpcStream pb.WatchRPC_WatchServer

	watchStream chan *pb.WatchResponse

	watcherStore *watcherStore

	wg sync.WaitGroup

	lg *zap.Logger

	closec chan struct{}
}

func (sws *serverWatchStream) sendLoop() {
	for {
		select {
		case wresp := <-sws.watchStream:
			serr := sws.grpcStream.Send(wresp)
			if serr != nil {
				if isClientCtxErr(sws.grpcStream.Context().Err(), serr) {
					if sws.lg != nil {
						sws.lg.Debug("failed to send watch response to gRPC stream", zap.Error(serr))
					}
				} else {
					if sws.lg != nil {
						sws.lg.Warn("failed to send watch response to gRPC stream", zap.Error(serr))
					}
				}
				return
			}
		case <-sws.closec:
			return
		}
	}
}

func (sws *serverWatchStream) recvLoop() error {
	for {
		req, err := sws.grpcStream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		switch uv := req.RequestUnion.(type) {
		case *pb.WatchRequest_CreateRequest:
			if uv.CreateRequest == nil {
				goto exit
			}

			sws.lg.Info("WatchRequest_CreateRequest", zap.String("watchID", uv.CreateRequest.WatchId), zap.Any("req", uv.CreateRequest.App))

			w := newWatcher(uv.CreateRequest.App, sws.watchStream)
			sws.watcherStore.createWatch(uv.CreateRequest.WatchId, w)
			sws.watchID = uv.CreateRequest.WatchId
		case *pb.WatchRequest_CancelRequest:
			sws.lg.Info("WatchRequest_CancelRequest", zap.String("watchID", uv.CancelRequest.WatchId))

			if uv.CancelRequest != nil {
				id := uv.CancelRequest.WatchId
				sws.watcherStore.cancelWatch(id)
			}
			goto exit
		default:
			continue
		}
	}

exit:
	return nil
}

func (sws *serverWatchStream) close() {
	close(sws.closec)
	sws.wg.Wait()
}

func isClientCtxErr(ctxErr error, err error) bool {
	if ctxErr != nil {
		return true
	}

	ev, ok := status.FromError(err)
	if !ok {
		return false
	}

	switch ev.Code() {
	case codes.Canceled, codes.DeadlineExceeded:
		// client-side context cancel or deadline exceeded
		// "rpc error: code = Canceled desc = context canceled"
		// "rpc error: code = DeadlineExceeded desc = context deadline exceeded"
		return true
	case codes.Unavailable:
		msg := ev.Message()
		// client-side context cancel or deadline exceeded with TLS ("http2.errClientDisconnected")
		// "rpc error: code = Unavailable desc = client disconnected"
		if msg == "client disconnected" {
			return true
		}
		// "grpc/transport.ClientTransport.CloseStream" on canceled streams
		// "rpc error: code = Unavailable desc = stream error: stream ID 21; CANCEL")
		if strings.HasPrefix(msg, "stream error: ") && strings.HasSuffix(msg, "; CANCEL") {
			return true
		}
	}
	return false
}
