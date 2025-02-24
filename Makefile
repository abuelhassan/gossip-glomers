.PHONY: serve run-echo run-generator run-broadcast run-counter run-kafka

EXECUTABLE_NAME = out
PROJECT_PATH = $(GOPATH)/src/github.com/abuelhassan/gossip-glomers

serve:
	cd -P $(MAELSTROM_PATH); ./maelstrom serve

run-echo:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=echo"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w echo --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 1 --time-limit 10
	rm -f $(EXECUTABLE_NAME)

run-generator:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=generate"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w unique-ids --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --time-limit 30 --rate 1000 --node-count 3 --availability total --nemesis partition
	rm -f $(EXECUTABLE_NAME)

run-broadcast:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=broadcast"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w broadcast --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 25 --time-limit 20 --rate 100 --latency 100
	rm -f $(EXECUTABLE_NAME)

run-counter:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=counter"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w g-counter --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 3 --rate 100 --time-limit 20 --nemesis partition
	rm -f $(EXECUTABLE_NAME)

run-kafka:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=kafka"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w kafka --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 2 --concurrency 2n --time-limit 20 --rate 1000
	rm -f $(EXECUTABLE_NAME)