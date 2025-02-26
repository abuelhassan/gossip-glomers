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
	typeEcho           = "echo"
	typeGenerator      = "generator"
	typeBroadcast      = "broadcast"
	typeCounter        = "counter"
	typeKafka          = "kafka"
	typeTXNUncommitted = "txn-uncommitted"
)

const (
	counterSumKey = "__SUM"

	kafkaOffsetsKey   = "__OFFSETS"
	kafkaCommittedKey = "__COMMITTED"
)

var AppType string

type broadcast struct {
	mu   sync.Mutex
	vals []int
	mp   map[int]struct{}
}

type txnStore struct {
	mu sync.Mutex
	mp map[int]*int
}

type server struct {
	n            *maelstrom.Node
	bcast        broadcast
	bcastBatcher batcher.Batcher[int]
	cntrKV       *maelstrom.KV
	kafkaKV      *maelstrom.KV
	txnStore     txnStore
}

// Echo Challenge
func (s *server) echoHandler(m maelstrom.Message) error {
	var body map[string]any
	readInto(m.Body, &body)
	body["type"] = "echo_ok"
	return s.n.Reply(m, body)
}

// Unique ID Challenge
func (s *server) generateHandler(m maelstrom.Message) error {
	return s.n.Reply(m, map[string]any{
		"type": "generate_ok",
		"id":   uint64(rand.Int64() + time.Now().UnixNano()),
	})
}

// Broadcast Challenge
func (s *server) broadcastReadHandler(m maelstrom.Message) error {
	return s.n.Reply(m, map[string]any{
		"type":     "read_ok",
		"messages": s.bcast.vals,
	})
}

func (s *server) broadcastTopologyHandler(m maelstrom.Message) error {
	// ignoring and just using a flat tree topology.
	return s.n.Reply(m, map[string]any{"type": "topology_ok"})
}

func (s *server) broadcastHandler(m maelstrom.Message) error {
	var body struct {
		Message  int   `json:"message"`
		Messages []int `json:"messages"`
	}
	readInto(m.Body, &body)

	var vals []int
	if body.Messages != nil {
		vals = body.Messages
	} else {
		vals = []int{body.Message}
	}

	s.bcast.mu.Lock()
	for _, val := range vals {
		if _, ok := s.bcast.mp[val]; ok {
			continue
		}
		s.bcast.vals = append(s.bcast.vals, val)
		s.bcast.mp[val] = struct{}{}
		s.bcastBatcher.Add(val)
	}
	s.bcast.mu.Unlock()

	return s.n.Reply(m, map[string]any{"type": "broadcast_ok"})
}

func (s *server) broadcast(vals []int) {
	const rootNode = "n0"

	fn := func(dest string) func(ctx context.Context) error {
		return func(ctx context.Context) error {
			_, err := s.n.SyncRPC(ctx, dest, map[string]any{
				"type":     "broadcast",
				"messages": vals,
			})
			return err
		}
	}

	children := []string{rootNode}
	if s.n.ID() == rootNode {
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
func (s *server) counterAddHandler(m maelstrom.Message) error {
	var body struct {
		Delta int `json:"delta"`
	}
	readInto(m.Body, &body)
	kvUpdate[int](s.cntrKV, counterSumKey, func() int { return 0 }, func(cur int) int { return cur + body.Delta })
	return s.n.Reply(m, map[string]any{
		"type": "add_ok",
	})
}

func (s *server) counterReadHandler(m maelstrom.Message) error {
	retryWrapped(func(ctx context.Context) error {
		return s.cntrKV.Write(ctx, "rand", rand.Int())
	})
	sum, _, _ := kvRead[int](s.cntrKV, counterSumKey)
	return s.n.Reply(m, map[string]any{
		"type":  "read_ok",
		"value": sum,
	})
}

// Kafka-Style Logs
func (s *server) kafkaSendHandler(m maelstrom.Message) error {
	var body struct {
		Key string `json:"key"`
		Msg int    `json:"msg"`
	}
	readInto(m.Body, &body)

	var offset int
	kvUpdate[int](
		s.kafkaKV,
		fmt.Sprintf("%s_%s", kafkaOffsetsKey, body.Key),
		func() int { return -1 },
		func(cur int) int {
			offset = cur + 1
			return offset
		},
	)

	retryWrapped(func(ctx context.Context) error {
		return s.kafkaKV.Write(ctx, fmt.Sprintf("%s_%d", body.Key, offset), body.Msg)
	})
	return s.n.Reply(m, map[string]any{
		"type":   "send_ok",
		"offset": offset,
	})
}

func (s *server) kafkaPollHandler(m maelstrom.Message) error {
	var body struct {
		Offsets map[string]int
	}
	readInto(m.Body, &body)

	resp := map[string][][]int{}
	for key, offset := range body.Offsets {
		for ; ; offset++ {
			lg, ok, _ := kvRead[int](s.kafkaKV, fmt.Sprintf("%s_%d", key, offset))
			if ok {
				resp[key] = append(resp[key], []int{offset, lg})
			} else {
				break
			}
		}
	}

	return s.n.Reply(m, map[string]any{
		"type": "poll_ok",
		"msgs": resp,
	})
}

func (s *server) kafkaCommitHandler(m maelstrom.Message) error {
	var body struct {
		Offsets map[string]int
	}
	readInto(m.Body, &body)
	offsets := body.Offsets

	for key, offset := range offsets {
		offsetKey := fmt.Sprintf("%s_%s", kafkaCommittedKey, key)
		retryWrapped(func(ctx context.Context) error {
			committed, _, _ := kvRead[int](s.kafkaKV, offsetKey)
			if committed > offset {
				return nil
			}
			return s.kafkaKV.CompareAndSwap(ctx, offsetKey, committed, offset, true)
		})
	}
	return s.n.Reply(m, map[string]any{
		"type": "commit_offsets_ok",
	})
}

func (s *server) kafkaListCommitsHandler(m maelstrom.Message) error {
	var body struct {
		Keys []string `json:"keys"`
	}
	readInto(m.Body, &body)
	resp := map[string]int{}
	for _, key := range body.Keys {
		val, ok, _ := kvRead[int](s.kafkaKV, fmt.Sprintf("%s_%s", kafkaCommittedKey, key))
		if ok {
			resp[key] = val
		}
	}
	return s.n.Reply(m, map[string]any{
		"type":    "list_committed_offsets_ok",
		"offsets": resp,
	})
}

// Transactions - Read Uncommitted - In Progress
func (s *server) txnHandler(m maelstrom.Message) error {
	var body struct {
		Txn [][]any `json:"txn"`
	}
	readInto(m.Body, &body)

	s.txnStore.mu.Lock()
	for idx, op := range body.Txn {
		switch op[0].(string) {
		case "r":
			body.Txn[idx][2] = s.txnStore.mp[int(op[1].(float64))]
		case "w":
			key, value := int(op[1].(float64)), int(op[2].(float64))
			s.txnStore.mp[key] = &value
		}
	}
	s.txnStore.mu.Unlock()

	if m.Src[0] != 'n' { // Not coming from another node
		go func() {
			for _, dest := range s.n.NodeIDs() {
				if dest == s.n.ID() {
					continue
				}
				retryWrapped(func(ctx context.Context) error {
					_, err := s.n.SyncRPC(ctx, dest, m.Body)
					return err
				})
			}
		}()
	}

	return s.n.Reply(m, map[string]any{
		"type": "txn_ok",
		"txn":  body.Txn,
	})
}

func main() {
	const bcastBatchingLimit = 50
	const bcastBatchingDuration = 500 * time.Millisecond

	n := maelstrom.NewNode()
	srv := server{n: n}

	switch AppType {
	case typeEcho:
		n.Handle("echo", srv.echoHandler)
	case typeGenerator:
		n.Handle("generate", srv.generateHandler)
	case typeBroadcast:
		srv.bcast = broadcast{vals: []int{}, mp: map[int]struct{}{}}
		srv.bcastBatcher = batcher.New[int](context.Background(), bcastBatchingLimit, bcastBatchingDuration, srv.broadcast)
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
	case typeTXNUncommitted:
		srv.txnStore = txnStore{mp: map[int]*int{}}
		n.Handle("txn", srv.txnHandler)
	}

	if err := n.Run(); err != nil {
		log.Printf("err: %s", err)
		os.Exit(1)
	}
}

func readInto(body json.RawMessage, val any) {
	if err := json.Unmarshal(body, val); err != nil {
		log.Panicf("Failed to unmarshal %s, err: %s", body, err.Error())
	}
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
