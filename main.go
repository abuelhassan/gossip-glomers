package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
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
	typeKafka     = "kafka"
)

const (
	counterSumKey = "SUM"

	kafkaOffsetsKey   = "__OFFSETS"
	kafkaCommittedKey = "__COMMITTED"
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
	kafkaKV      *maelstrom.KV
}

// Echo Challenge
func (s *server) echoHandler(msg maelstrom.Message) error {
	body := readBody(msg)
	body["type"] = "echo_ok"
	return s.n.Reply(msg, body)
}

// Unique ID Challenge
func (s *server) generateHandler(msg maelstrom.Message) error {
	return s.n.Reply(msg, map[string]any{
		"type": "generate_ok",
		"id":   uint64(rand.Int64() + time.Now().UTC().UnixNano()),
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
	body := readBody(msg)

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
	fn := func(dest string) func(ctx context.Context) error {
		return func(ctx context.Context) error {
			_, err := s.n.SyncRPC(ctx, dest, map[string]any{
				"type":     "broadcast",
				"messages": vals,
			})
			return err
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
		go retryWrapped(fn(dest))
	}
}

// Grow-Only Counter Challenge
func (s *server) counterAddHandler(msg maelstrom.Message) error {
	body := readBody(msg)
	delta := int(body["delta"].(float64))

	kvUpdate[int](s.cntrKV, counterSumKey, func() int { return 0 }, func(cur int) int { return cur + delta })

	return s.n.Reply(msg, map[string]any{
		"type": "add_ok",
	})
}

func (s *server) counterReadHandler(msg maelstrom.Message) error {
	retryWrapped(func(ctx context.Context) error {
		return s.cntrKV.Write(ctx, "rand", rand.Int())
	})
	sum, _, _ := kvRead[int](s.cntrKV, counterSumKey)
	return s.n.Reply(msg, map[string]any{
		"type":  "read_ok",
		"value": sum,
	})
}

// Kafka-Style Logs
func (s *server) kafkaSendHandler(msg maelstrom.Message) error {
	body := readBody(msg)
	key, val := body["key"].(string), int(body["msg"].(float64))

	var offset int
	kvUpdate[int](
		s.kafkaKV,
		fmt.Sprintf("%s_%s", kafkaOffsetsKey, key),
		func() int { return -1 },
		func(cur int) int {
			offset = cur + 1
			return offset
		},
	)

	retryWrapped(func(ctx context.Context) error {
		return s.kafkaKV.Write(ctx, fmt.Sprintf("%s_%d", key, offset), val)
	})
	return s.n.Reply(msg, map[string]any{
		"type":   "send_ok",
		"offset": offset,
	})
}

func (s *server) kafkaPollHandler(msg maelstrom.Message) error {
	body := readBody(msg)
	offsets := body["offsets"].(map[string]any)

	resp := map[string][][]int{}
	for key, val := range offsets {
		offset := int(val.(float64))
		for ; ; offset++ {
			lg, ok, _ := kvRead[int](s.kafkaKV, fmt.Sprintf("%s_%d", key, offset))
			if ok {
				resp[key] = append(resp[key], []int{offset, lg})
			} else {
				break
			}
		}
	}

	return s.n.Reply(msg, map[string]any{
		"type": "poll_ok",
		"msgs": resp,
	})
}

func (s *server) kafkaCommitHandler(msg maelstrom.Message) error {
	body := readBody(msg)
	offsets := body["offsets"].(map[string]any)

	for key, val := range offsets {
		offset := int(val.(float64))
		offsetKey := fmt.Sprintf("%s_%s", kafkaCommittedKey, key)
		retryWrapped(func(ctx context.Context) error {
			committed, _, _ := kvRead[int](s.kafkaKV, offsetKey)
			if committed > offset {
				return nil
			}
			return s.kafkaKV.CompareAndSwap(ctx, offsetKey, committed, offset, true)
		})
	}
	return s.n.Reply(msg, map[string]any{
		"type": "commit_offsets_ok",
	})
}

func (s *server) kafkaListCommitsHandler(msg maelstrom.Message) error {
	body := readBody(msg)
	keys := body["keys"].([]any)
	resp := map[string]int{}
	for _, key := range keys {
		val, ok, _ := kvRead[int](s.kafkaKV, fmt.Sprintf("%s_%s", kafkaCommittedKey, key))
		if ok {
			resp[key.(string)] = val
		}
	}
	return s.n.Reply(msg, map[string]any{
		"type":    "list_committed_offsets_ok",
		"offsets": resp,
	})
}

func main() {
	const bcastBatchingLimit = 50
	const bcastBatchingDuration = 450 * time.Millisecond

	n := maelstrom.NewNode()
	srv := server{n: n}

	switch AppType {
	case typeEcho:
		n.Handle("echo", srv.echoHandler)
	case typeGenerate:
		n.Handle("generate", srv.generateHandler)
	case typeBroadcast:
		srv.bcast = broadcast{vals: []any{}, mp: map[float64]struct{}{}}
		srv.bcastBatcher = batcher.New[any](context.Background(), bcastBatchingLimit, bcastBatchingDuration, srv.broadcast)
		n.Handle("broadcast", srv.broadcastHandler)
		n.Handle("topology", srv.broadcastTopologyHandler)
		n.Handle("read", srv.broadcastReadHandler)
	case typeCounter:
		srv.cntrKV = maelstrom.NewSeqKV(n)
		n.Handle("add", srv.counterAddHandler)
		n.Handle("read", srv.counterReadHandler)
	case typeKafka:
		srv.kafkaKV = maelstrom.NewLinKV(n)
		n.Handle("send", srv.kafkaSendHandler)
		n.Handle("poll", srv.kafkaPollHandler)
		n.Handle("commit_offsets", srv.kafkaCommitHandler)
		n.Handle("list_committed_offsets", srv.kafkaListCommitsHandler)
	}

	if err := n.Run(); err != nil {
		log.Printf("err: %s", err)
		os.Exit(1)
	}
}

func readBody(msg maelstrom.Message) map[string]any {
	var body map[string]any
	if err := json.Unmarshal(msg.Body, &body); err != nil {
		log.Panicf("Failed to unmarshal %s, err: %s", msg.Body, err.Error())
	}
	return body
}

func retryWrapped(fn func(context.Context) error) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := fn(ctx)
		cancel()
		if err == nil {
			return
		}
	}
}

// kvUpdate Reads a value, then updates using CompareAndSwap. It performs retries till success.
func kvUpdate[VT any](kv *maelstrom.KV, key string, defaultValue func() VT, newValue func(VT) VT) {
	retryWrapped(func(ctx context.Context) error {
		curValue, ok, err := kvRead[VT](kv, key)
		if err != nil {
			return err
		}
		if !ok {
			curValue = defaultValue()
		}
		return kv.CompareAndSwap(ctx, key, curValue, newValue(curValue), true)
	})
}

func kvRead[VT any](kv *maelstrom.KV, key string) (VT, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var val VT
	err := kv.ReadInto(ctx, key, &val)
	if err == nil {
		return val, true, nil
	}
	var rpcErr *maelstrom.RPCError
	if errors.As(err, &rpcErr) && rpcErr.Code == maelstrom.KeyDoesNotExist {
		return val, false, nil
	}
	return val, false, err
}
