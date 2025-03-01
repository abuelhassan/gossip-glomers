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
	"github.com/abuelhassan/gossip-glomers/retryer"
	"github.com/abuelhassan/gossip-glomers/worker"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

const (
	appTypeEcho      = "echo"
	appTypeGenerator = "generator"
	appTypeBroadcast = "broadcast"
	appTypeCounter   = "counter"
	appTypeKafka     = "kafka"
	appTypeTXN       = "txn"
)

const (
	counterSumKey = "__SUM"

	kafkaOffsetsKey   = "__OFFSETS"
	kafkaCommittedKey = "__COMMITTED"
)

const (
	txnSyncLock = iota
	txnSyncAbort
	txnSyncCommit
)

var AppType string

type broadcast struct {
	mu   sync.Mutex
	vals []int
	mp   map[int]struct{}
}

type txnStore struct {
	mu    sync.Mutex
	mp    map[int]int
	locks map[int]txnLock
}

type txnLock struct {
	timestamp time.Time
	txnID     int64
}

type server struct {
	n            *maelstrom.Node
	bcast        broadcast
	bcastBatcher batcher.Batcher[int]
	cntrKV       *maelstrom.KV
	kafkaKV      *maelstrom.KV
	txnStore     txnStore
	txnWorker    worker.Worker
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
		go retryer.Retry(-1, 0, fn(dest))
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
	retryer.Retry(-1, 0, func(ctx context.Context) error {
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

	retryer.Retry(-1, 0, func(ctx context.Context) error {
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
		retryer.Retry(-1, 0, func(ctx context.Context) error {
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

// Transactions - Read Committed
func (s *server) txnHandler(m maelstrom.Message) error {
	var body struct {
		Txn [][]any `json:"txn"`
	}
	readInto(m.Body, &body)

	writes := make(map[int]int)
	for _, op := range body.Txn {
		if op[0] == "r" {
			continue
		}
		writes[int(op[1].(float64))] = int(op[2].(float64))
	}

	retryer.Retry(10, 13*time.Millisecond, func(ctx context.Context) error {
		var returnErr error
		s.txnWorker.Do(func() {
			txnID := time.Now().UnixNano() + rand.Int64()

			// Lock all nodes
			var wg sync.WaitGroup
			badDests := map[string]struct{}{}
			var badMu sync.Mutex
			for _, dest := range s.n.NodeIDs() {
				wg.Add(1)
				go func(dest string) {
					defer wg.Done()
					_, err := s.n.SyncRPC(ctx, dest, map[string]any{
						"type":   "sync",
						"cmd":    txnSyncLock,
						"writes": writes,
						"txn_id": txnID,
					})
					if err != nil {
						badMu.Lock()
						badDests[dest] = struct{}{}
						badMu.Unlock()
					}
				}(dest)
			}
			wg.Wait()

			if len(badDests) >= (len(s.n.NodeIDs())+1)/2 {
				for _, dest := range s.n.NodeIDs() {
					go func(dest string) {
						retryer.Retry(5, 13*time.Millisecond, func(ctx context.Context) error {
							_, err := s.n.SyncRPC(ctx, dest, map[string]any{
								"type":   "sync",
								"cmd":    txnSyncAbort,
								"writes": writes,
								"txn_id": txnID,
							})
							return err
						})
					}(dest)
				}
				returnErr = fmt.Errorf("txn conflict")
				return
			}

			// When majority is locked successfully, conflicting transactions would fail.
			s.txnStore.mu.Lock()
			curMp := map[int]int{}
			for idx, op := range body.Txn {
				if op[0] == "r" {
					if val, ok := curMp[int(op[1].(float64))]; ok {
						body.Txn[idx][2] = val
					} else {
						body.Txn[idx][2] = s.txnStore.mp[int(op[1].(float64))]
					}
				} else {
					curMp[int(op[1].(float64))] = int(op[2].(float64))
				}
			}
			s.txnStore.mu.Unlock()
			for _, dest := range s.n.NodeIDs() {
				go func(dest string) {
					// Limiting the retries here may lead to inconsistencies between nodes, specially those that are unavailable for a while.
					// It can be fixed by multiple reads on client side, to compare the reads and choose latest.
					retryer.Retry(10, 13*time.Millisecond, func(ctx context.Context) error {
						_, err := s.n.SyncRPC(ctx, dest, map[string]any{
							"type":   "sync",
							"cmd":    txnSyncCommit,
							"writes": writes,
							"txn_id": txnID,
						})
						return err
					})
				}(dest)
			}
		})
		return returnErr
	})

	return s.n.Reply(m, map[string]any{
		"type": "txn_ok",
		"txn":  body.Txn,
	})
}

func (s *server) txnSyncHandler(m maelstrom.Message) error {
	var body struct {
		Cmd    int         `json:"cmd"`
		Writes map[int]int `json:"writes"`
		TxnID  int64       `json:"txn_id"`
	}
	readInto(m.Body, &body)

	switch body.Cmd {
	case txnSyncLock:
		ok := s.txnLock(body.TxnID, body.Writes)
		if !ok {
			return s.n.Reply(m, map[string]any{
				"type": "error",
				"code": maelstrom.TxnConflict,
				"text": "txn abort",
			})
		}
	case txnSyncAbort:
		s.txnUnlock(body.TxnID, body.Writes)
	case txnSyncCommit:
		s.txnCommit(body.TxnID, body.Writes)
	}
	return s.n.Reply(m, map[string]any{
		"type": "sync_ok",
	})
}

func (s *server) txnLock(txnID int64, writes map[int]int) bool {
	const txnLockDur = 100 * time.Millisecond

	s.txnStore.mu.Lock()
	defer s.txnStore.mu.Unlock()
	for k := range writes {
		lock, ok := s.txnStore.locks[k]
		if ok && time.Since(lock.timestamp) < txnLockDur {
			return false
		}
	}
	for k := range writes {
		s.txnStore.locks[k] = txnLock{
			timestamp: time.Now(),
			txnID:     txnID,
		}
	}
	return true
}

func (s *server) txnUnlock(txnID int64, writes map[int]int) {
	s.txnStore.mu.Lock()
	defer s.txnStore.mu.Unlock()
	for k := range writes {
		if s.txnStore.locks[k].txnID == txnID {
			delete(s.txnStore.locks, k)
		}
	}
}

func (s *server) txnCommit(txnID int64, writes map[int]int) {
	s.txnStore.mu.Lock()
	defer s.txnStore.mu.Unlock()
	for k, v := range writes {
		if s.txnStore.locks[k].txnID == txnID {
			delete(s.txnStore.locks, k)
		}
		s.txnStore.mp[k] = v
	}
}

func main() {
	const bcastBatchingLimit = 50
	const bcastBatchingDuration = 500 * time.Millisecond

	n := maelstrom.NewNode()
	srv := server{n: n}

	switch AppType {
	case appTypeEcho:
		n.Handle("echo", srv.echoHandler)
	case appTypeGenerator:
		n.Handle("generate", srv.generateHandler)
	case appTypeBroadcast:
		srv.bcast = broadcast{vals: []int{}, mp: map[int]struct{}{}}
		srv.bcastBatcher = batcher.New[int](context.Background(), bcastBatchingLimit, bcastBatchingDuration, srv.broadcast)
		n.Handle("broadcast", srv.broadcastHandler)
		n.Handle("topology", srv.broadcastTopologyHandler)
		n.Handle("read", srv.broadcastReadHandler)
	case appTypeCounter:
		srv.cntrKV = maelstrom.NewSeqKV(n)
		n.Handle("add", srv.counterAddHandler)
		n.Handle("read", srv.counterReadHandler)
	case appTypeKafka:
		srv.kafkaKV = maelstrom.NewLinKV(n)
		n.Handle("send", srv.kafkaSendHandler)
		n.Handle("poll", srv.kafkaPollHandler)
		n.Handle("commit_offsets", srv.kafkaCommitHandler)
		n.Handle("list_committed_offsets", srv.kafkaListCommitsHandler)
	case appTypeTXN:
		srv.txnStore = txnStore{mp: map[int]int{}, locks: map[int]txnLock{}}
		srv.txnWorker = worker.New(30)
		n.Handle("txn", srv.txnHandler)
		n.Handle("sync", srv.txnSyncHandler)
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

// kvUpdate Reads a value, then updates using CompareAndSwap. It performs retries till success.
func kvUpdate[VT any](kv *maelstrom.KV, key string, defaultValue func() VT, newValue func(VT) VT) {
	retryer.Retry(-1, 0, func(ctx context.Context) error {
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
