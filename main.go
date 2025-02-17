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

type app struct {
	mu           sync.Mutex
	messages     []any
	uniqueFloats map[float64]struct{}
}

func main() {
	n := maelstrom.NewNode()
	msgs := app{mu: sync.Mutex{}, messages: []any{}, uniqueFloats: map[float64]struct{}{}}

	n.Handle("echo", func(msg maelstrom.Message) error {
		body, err := readBody(msg)
		if err != nil {
			return err
		}

		body["type"] = "echo_ok"
		return n.Reply(msg, body)
	})

	n.Handle("generate", func(msg maelstrom.Message) error {
		body, err := readBody(msg)
		if err != nil {
			return err
		}

		body["type"] = "generate_ok"
		body["id"] = int64(rand.Int31()) + time.Now().UTC().UnixMicro()
		return n.Reply(msg, body)
	})

	n.Handle("broadcast", func(msg maelstrom.Message) error {
		body, err := readBody(msg)
		if err != nil {
			return err
		}

		val := body["message"].(float64)
		msgs.mu.Lock()
		defer msgs.mu.Unlock()
		if _, ok := msgs.uniqueFloats[val]; !ok {
			msgs.messages = append(msgs.messages, val)
			msgs.uniqueFloats[val] = struct{}{}
			for _, dest := range n.NodeIDs() {
				if dest == n.ID() || dest == msg.Src {
					continue
				}
				// broadcast in the background
				go func() {
					_ = n.Send(dest, msg.Body)
				}()
			}
		}
		body["type"] = "broadcast_ok"
		delete(body, "message")

		return n.Reply(msg, body)
	})

	n.Handle("read", func(msg maelstrom.Message) error {
		body, err := readBody(msg)
		if err != nil {
			return err
		}

		body["type"] = "read_ok"
		body["messages"] = msgs.messages
		delete(body, "message")
		return n.Reply(msg, body)
	})

	n.Handle("topology", func(msg maelstrom.Message) error {
		return n.Reply(msg, map[string]any{"type": "topology_ok"})
	})

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
