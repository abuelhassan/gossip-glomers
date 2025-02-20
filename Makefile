.PHONY: run-echo

EXECUTABLE_NAME = out
PROJECT_PATH = $(GOPATH)/src/github.com/abuelhassan/gossip-glomers

run-echo:
	go build -o $(EXECUTABLE_NAME)
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w echo --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 1 --time-limit 10
	rm -f $(EXECUTABLE_NAME)

run-unique-id:
	go build -o $(EXECUTABLE_NAME)
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w unique-ids --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --time-limit 30 --rate 1000 --node-count 3 --availability total --nemesis partition
	rm -f $(EXECUTABLE_NAME)

run-broadcast-a:
	go build -o $(EXECUTABLE_NAME)
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w broadcast --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 1 --time-limit 20 --rate 10
	rm -f $(EXECUTABLE_NAME)

run-broadcast-b:
	go build -o $(EXECUTABLE_NAME)
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w broadcast --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 5 --time-limit 20 --rate 10
	rm -f $(EXECUTABLE_NAME)

run-broadcast-c:
	go build -o $(EXECUTABLE_NAME)
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w broadcast --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 5 --time-limit 20 --rate 10 --nemesis partition
	rm -f $(EXECUTABLE_NAME)

run-broadcast-d:
	go build -o $(EXECUTABLE_NAME)
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w broadcast --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 25 --time-limit 20 --rate 100 --latency 100
	rm -f $(EXECUTABLE_NAME)

run-broadcast-e:
	go build -o $(EXECUTABLE_NAME)
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w broadcast --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 25 --time-limit 20 --rate 100 --latency 100
	rm -f $(EXECUTABLE_NAME)

