.PHONY: run-echo run-unique-id run-broadcast-a run-broadcast-b run-broadcast-c run-broadcast-d run-broadcast-e run-counter

EXECUTABLE_NAME = out
PROJECT_PATH = $(GOPATH)/src/github.com/abuelhassan/gossip-glomers

run-echo:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=echo"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w echo --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 1 --time-limit 10
	rm -f $(EXECUTABLE_NAME)

run-unique-id:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=generate"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w unique-ids --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --time-limit 30 --rate 1000 --node-count 3 --availability total --nemesis partition
	rm -f $(EXECUTABLE_NAME)

run-broadcast-a:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=broadcast"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w broadcast --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 1 --time-limit 20 --rate 10
	rm -f $(EXECUTABLE_NAME)

run-broadcast-b:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=broadcast"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w broadcast --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 5 --time-limit 20 --rate 10
	rm -f $(EXECUTABLE_NAME)

run-broadcast-c:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=broadcast"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w broadcast --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 5 --time-limit 20 --rate 10 --nemesis partition
	rm -f $(EXECUTABLE_NAME)

run-broadcast-d:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=broadcast"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w broadcast --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 25 --time-limit 20 --rate 100 --latency 100
	rm -f $(EXECUTABLE_NAME)

run-broadcast-e:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=broadcast"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w broadcast --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 25 --time-limit 20 --rate 100 --latency 100
	rm -f $(EXECUTABLE_NAME)

run-counter:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=counter"
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w g-counter --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 3 --rate 100 --time-limit 20 --nemesis partition
	rm -f $(EXECUTABLE_NAME)
