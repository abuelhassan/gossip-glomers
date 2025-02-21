package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"os"
	"sync"
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

const (
	counterSumKey = "SUM"
)

var AppType string

type broadcast struct {
	mu   sync.Mutex
	vals []any
	mp   map[float64]struct{}
}

type server struct {
	n            *maelstrom.Node
	bcast        broadcast
	bcastBatcher batcher.Batcher[any]
	cntrKV       *maelstrom.KV
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
	return s.n.Reply(msg, map[string]any{
		"type": "generate_ok",
		"id":   int64(rand.Int31()) + time.Now().UTC().UnixMicro(),
	})
}

// Broadcast Challenge
func (s *server) broadcastReadHandler(msg maelstrom.Message) error {
	return s.n.Reply(msg, map[string]any{
		"type":     "read_ok",
		"messages": s.bcast.vals,
	})
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
		s.bcastBatcher.Add(val)
	}
	s.bcast.mu.Unlock()

	return s.n.Reply(msg, map[string]any{"type": "broadcast_ok"})
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
	delta := int(body["delta"].(float64))

	func() {
		for {
			var curSum int
			var rpc *maelstrom.RPCError
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			val, err := s.cntrKV.Read(ctx, counterSumKey)
			cancel()
			if err == nil {
				curSum = val.(int)
			} else if errors.As(err, &rpc) && rpc.Code == maelstrom.KeyDoesNotExist {
				curSum = 0
			} else {
				continue // retry
			}

			ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			err = s.cntrKV.CompareAndSwap(ctx, counterSumKey, curSum, curSum+delta, true)
			cancel()
			if err == nil {
				return
			}
			// retry
		}
	}()

	return s.n.Reply(msg, map[string]any{
		"type": "add_ok",
	})
}

func (s *server) counterReadHandler(msg maelstrom.Message) error {
	sum := 0
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	_ = s.cntrKV.Write(ctx, "rand", rand.Int()) // Hacky way to keep the last read consistent!
	_ = s.cntrKV.ReadInto(ctx, counterSumKey, &sum)
	cancel()
	return s.n.Reply(msg, map[string]any{
		"type":  "read_ok",
		"value": sum,
	})
}

func main() {
	const bcastBatchingLimit = 50
	const bcastBatchingDuration = 450 * time.Millisecond

	n := maelstrom.NewNode()
	srv := server{n: n, bcast: broadcast{mu: sync.Mutex{}, vals: []any{}, mp: map[float64]struct{}{}}, cntrKV: maelstrom.NewSeqKV(n)}
	srv.bcastBatcher = batcher.New[any](context.Background(), bcastBatchingLimit, bcastBatchingDuration, srv.broadcast)

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
