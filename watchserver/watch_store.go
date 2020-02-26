package watchserver

import (
	"sync"
	"time"

	pb "github.com/xkeyideal/grpcwatch/watchpb"
)

type watcher struct {
	name string
	env  string

	ch chan<- *pb.WatchResponse
}

func newWatcher(app *pb.App, ch chan<- *pb.WatchResponse) *watcher {
	return &watcher{
		name: app.Name,
		env:  app.Env,
		ch:   ch,
	}
}

type watcherStore struct {
	mu sync.RWMutex

	watchers map[string]*watcher
}

func newWatcherStore() *watcherStore {
	ws := &watcherStore{
		watchers: make(map[string]*watcher),
	}

	go ws.mockWatch()

	return ws
}

func (ws *watcherStore) mockWatch() {
	ticker := time.NewTicker(2 * time.Second)

	for range ticker.C {
		ws.mu.RLock()
		for _, w := range ws.watchers {
			w.ch <- &pb.WatchResponse{
				Event: pb.EventType_UPDATE,
				App: &pb.App{
					Name: w.name,
					Env:  w.env,
				},
				Servers: []*pb.AppServer{
					&pb.AppServer{
						Ip:   "127.0.0.1",
						Port: "9090",
					},
				},
			}
		}
		ws.mu.RUnlock()
	}
}

func (ws *watcherStore) createWatch(watchID string, watcher *watcher) {
	watcher.ch <- &pb.WatchResponse{
		Created: true,
		Event:   pb.EventType_UPDATE,
		App: &pb.App{
			Name: watcher.name,
			Env:  watcher.env,
		},
		Servers: []*pb.AppServer{
			&pb.AppServer{
				Ip:   "127.0.0.1",
				Port: "9090",
			},
		},
	}
	ws.mu.Lock()
	ws.watchers[watchID] = watcher
	ws.mu.Unlock()

}

func (ws *watcherStore) cancelWatch(watchID string) {
	ws.mu.Lock()
	if w, ok := ws.watchers[watchID]; ok {
		w.ch <- &pb.WatchResponse{
			Canceled:     true,
			CancelReason: "client stop",
			Event:        pb.EventType_UPDATE,
			App: &pb.App{
				Name: w.name,
				Env:  w.env,
			},
			Servers: []*pb.AppServer{
				&pb.AppServer{
					Ip:   "127.0.0.1",
					Port: "9090",
				},
			},
		}
	}
	delete(ws.watchers, watchID)
	ws.mu.Unlock()
}
