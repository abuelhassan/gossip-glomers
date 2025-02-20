package main

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

type broadcast struct {
	mu   sync.Mutex
	vals []any
	mp   map[float64]struct{}
}

type server struct {
	n     *maelstrom.Node
	bcast broadcast
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

	val := body["message"].(float64)
	s.bcast.mu.Lock()
	if _, ok := s.bcast.mp[val]; !ok {
		s.bcast.vals = append(s.bcast.vals, val)
		s.bcast.mp[val] = struct{}{}
		children := []string{"n0"}
		if s.n.ID() == "n0" {
			children = s.n.NodeIDs()
		}
		for _, dest := range children {
			if dest == s.n.ID() || dest == msg.Src {
				continue
			}
			go s.broadcast(dest, msg.Body)
		}
	}
	s.bcast.mu.Unlock()

	body["type"] = "broadcast_ok"
	delete(body, "message")

	return s.n.Reply(msg, body)
}

func (s *server) broadcast(dest string, body json.RawMessage) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := s.n.SyncRPC(ctx, dest, body)
		cancel()
		if err == nil {
			return
		}
	}
}

func main() {
	n := maelstrom.NewNode()
	srv := server{n: n, bcast: broadcast{mu: sync.Mutex{}, vals: []any{}, mp: map[float64]struct{}{}}}

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
