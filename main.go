package main

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abuelhassan/gossip-glomers/batcher"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

const (
	typeEcho      = "echo"
	typeGenerate  = "generate"
	typeBroadcast = "broadcast"
	typeCounter   = "counter"
)

var AppType string

type broadcast struct {
	mu   sync.Mutex
	vals []any
	mp   map[float64]struct{}
}

type counter struct {
	val atomic.Int64
}

type server struct {
	n       *maelstrom.Node
	bcast   broadcast
	batcher batcher.Batcher[any]
	counter counter
}

// Echo Challenge
func (s *server) echoHandler(msg maelstrom.Message) error {
	body, err := readBody(msg)
	if err != nil {
		return err
	}

	body["type"] = "echo_ok"
	return s.n.Reply(msg, body)
}

// Unique ID Challenge
func (s *server) generateHandler(msg maelstrom.Message) error {
	body, err := readBody(msg)
	if err != nil {
		return err
	}

	body["type"] = "generate_ok"
	body["id"] = int64(rand.Int31()) + time.Now().UTC().UnixMicro()
	return s.n.Reply(msg, body)
}

// Broadcast Challenge
func (s *server) broadcastReadHandler(msg maelstrom.Message) error {
	body, err := readBody(msg)
	if err != nil {
		return err
	}

	body["type"] = "read_ok"
	body["messages"] = s.bcast.vals
	delete(body, "message")
	return s.n.Reply(msg, body)
}

func (s *server) broadcastTopologyHandler(msg maelstrom.Message) error {
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

// Grow-Only Counter Challenge
func (s *server) counterAddHandler(msg maelstrom.Message) error {
	body, err := readBody(msg)
	if err != nil {
		return err
	}

	s.counter.val.Add(int64(body["delta"].(float64)))
	return s.n.Reply(msg, map[string]any{
		"type": "add_ok",
	})
}

func (s *server) counterReadHandler(msg maelstrom.Message) error {
	sum := atomic.Int64{}
	wg := sync.WaitGroup{}
	for _, dest := range s.n.NodeIDs() {
		if dest == s.n.ID() {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			resp, err := s.n.SyncRPC(ctx, dest, map[string]any{
				"type": "readown",
			})
			cancel()
			if err != nil {
				return // Assume 0 - Can do retry or Cache locally instead
			}
			body, err := readBody(resp)
			if err != nil {
				panic(err)
			}
			sum.Add(int64(body["value"].(float64)))
			return
		}()
	}
	wg.Wait()
	sum.Add(s.counter.val.Load())
	return s.n.Reply(msg, map[string]any{
		"type":  "read_ok",
		"value": sum.Load(),
	})
}

func (s *server) counterReadOwnHandler(msg maelstrom.Message) error {
	return s.n.Reply(msg, map[string]any{
		"type":  "readown_ok",
		"value": s.counter.val.Load(),
	})
}

func main() {
	const bcastBatchingLimit = 50
	const bcastBatchingDuration = 450 * time.Millisecond

	n := maelstrom.NewNode()
	srv := server{n: n, bcast: broadcast{mu: sync.Mutex{}, vals: []any{}, mp: map[float64]struct{}{}}, counter: counter{val: atomic.Int64{}}}
	srv.batcher = batcher.New[any](context.Background(), bcastBatchingLimit, bcastBatchingDuration, srv.broadcast)

	switch AppType {
	case typeEcho:
		n.Handle("echo", srv.echoHandler)
	case typeGenerate:
		n.Handle("generate", srv.generateHandler)
	case typeBroadcast:
		n.Handle("broadcast", srv.broadcastHandler)
		n.Handle("topology", srv.broadcastTopologyHandler)
		n.Handle("read", srv.broadcastReadHandler)
	case typeCounter:
		n.Handle("add", srv.counterAddHandler)
		n.Handle("read", srv.counterReadHandler)
		n.Handle("readown", srv.counterReadOwnHandler)
	}

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
