package main

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/abuelhassan/gossip-glomers/batcher"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

type broadcast struct {
	mu   sync.Mutex
	vals []any
	mp   map[float64]struct{}
}

type server struct {
	n       *maelstrom.Node
	bcast   broadcast
	batcher batcher.Batcher[any]
}

func (s *server) echoHandler(msg maelstrom.Message) error {
	body, err := readBody(msg)
	if err != nil {
		return err
	}

	body["type"] = "echo_ok"
	return s.n.Reply(msg, body)
}

func (s *server) generateHandler(msg maelstrom.Message) error {
	body, err := readBody(msg)
	if err != nil {
		return err
	}

	body["type"] = "generate_ok"
	body["id"] = int64(rand.Int31()) + time.Now().UTC().UnixMicro()
	return s.n.Reply(msg, body)
}

func (s *server) readHandler(msg maelstrom.Message) error {
	body, err := readBody(msg)
	if err != nil {
		return err
	}

	body["type"] = "read_ok"
	body["messages"] = s.bcast.vals
	delete(body, "message")
	return s.n.Reply(msg, body)
}

func (s *server) topologyHandler(msg maelstrom.Message) error {
	// ignoring and just using a flat tree topology.
	return s.n.Reply(msg, map[string]any{"type": "topology_ok"})
}

func (s *server) broadcastHandler(msg maelstrom.Message) error {
	body, err := readBody(msg)
	if err != nil {
		return err
	}

	var vals []any
	if msgs, ok := body["messages"]; ok {
		vals = msgs.([]any)
	} else {
		vals = []any{body["message"]}
	}

	s.bcast.mu.Lock()
	for _, it := range vals {
		val := it.(float64)
		if _, ok := s.bcast.mp[val]; ok {
			continue
		}
		s.bcast.vals = append(s.bcast.vals, val)
		s.bcast.mp[val] = struct{}{}
		s.batcher.Add(val)
	}
	s.bcast.mu.Unlock()

	body["type"] = "broadcast_ok"
	delete(body, "message")
	delete(body, "messages")

	return s.n.Reply(msg, body)
}

func (s *server) broadcast(vals []any) {
	fn := func(dest string) {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_, err := s.n.SyncRPC(ctx, dest, map[string]any{
				"type":     "broadcast",
				"messages": vals,
			})
			cancel()
			if err == nil {
				return
			}
		}
	}

	children := []string{"n0"}
	if s.n.ID() == "n0" {
		children = s.n.NodeIDs()
	}
	for _, dest := range children {
		if dest == s.n.ID() {
			continue
		}
		go fn(dest)
	}
}

func main() {
	const bcastBatchingLimit = 50
	const bcastBatchingDuration = 800 * time.Millisecond

	n := maelstrom.NewNode()
	srv := server{n: n, bcast: broadcast{mu: sync.Mutex{}, vals: []any{}, mp: map[float64]struct{}{}}}
	srv.batcher = batcher.New[any](context.Background(), bcastBatchingLimit, bcastBatchingDuration, srv.broadcast)

	n.Handle("echo", srv.echoHandler)
	n.Handle("generate", srv.generateHandler)
	n.Handle("broadcast", srv.broadcastHandler)
	n.Handle("read", srv.readHandler)
	n.Handle("topology", srv.topologyHandler)

	if err := n.Run(); err != nil {
		log.Printf("err: %s", err)
		os.Exit(1)
	}
}

func readBody(msg maelstrom.Message) (map[string]any, error) {
	var body map[string]any
	if err := json.Unmarshal(msg.Body, &body); err != nil {
		return nil, err
	}
	return body, nil
}
