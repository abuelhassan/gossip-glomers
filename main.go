package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"time"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

func main() {
	n := maelstrom.NewNode()
	messages := make([]any, 0)

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

		messages = append(messages, body["message"].(float64))
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
		body["messages"] = messages
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
