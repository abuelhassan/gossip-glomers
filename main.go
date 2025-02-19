package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

type server struct {
	n *maelstrom.Node

	bcastMu   sync.Mutex
	bcastVals []any
	bcastMp   map[float64]struct{}
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
	body["messages"] = s.bcastVals
	delete(body, "message")
	return s.n.Reply(msg, body)
}

func (s *server) topologyHandler(msg maelstrom.Message) error {
	return s.n.Reply(msg, map[string]any{"type": "topology_ok"})
}

func (s *server) broadcastHandler(msg maelstrom.Message) error {
	body, err := readBody(msg)
	if err != nil {
		return err
	}

	val := body["message"].(float64)
	if _, ok := s.bcastMp[val]; !ok {
		s.bcastMu.Lock()
		s.bcastVals = append(s.bcastVals, val)
		s.bcastMp[val] = struct{}{}
		s.bcastMu.Unlock()

		children := []string{"n0"}
		if s.n.ID() == "n0" {
			children = s.n.NodeIDs()
		}
		for _, dest := range children {
			if dest == s.n.ID() || dest == msg.Src {
				continue
			}
			// broadcast in the background
			go s.broadcast(dest, msg.Body)
		}
	}

	body["type"] = "broadcast_ok"
	delete(body, "message")

	return s.n.Reply(msg, body)
}

func (s *server) broadcast(dest string, body json.RawMessage) {
	for {
		ch := make(chan struct{})
		err := s.n.RPC(dest, body, func(msg maelstrom.Message) error {
			ch <- struct{}{}
			return nil
		})
		if err != nil {
			panic(err)
		}
		select {
		case <-ch:
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func main() {
	n := maelstrom.NewNode()
	srv := server{n: n, bcastMu: sync.Mutex{}, bcastVals: []any{}, bcastMp: map[float64]struct{}{}}

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
