.PHONY: serve run-%

EXECUTABLE_NAME = out
PROJECT_PATH = $(GOPATH)/src/github.com/abuelhassan/gossip-glomers

echo := ./maelstrom test -w echo --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 1 --time-limit 10
generator := ./maelstrom test -w unique-ids --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --time-limit 30 --rate 1000 --node-count 3 --availability total --nemesis partition
broadcast := ./maelstrom test -w broadcast --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 25 --time-limit 20 --rate 100 --latency 100
counter := ./maelstrom test -w g-counter --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 3 --rate 100 --time-limit 20 --nemesis partition
kafka := ./maelstrom test -w kafka --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 2 --concurrency 2n --time-limit 20 --rate 1000
txn := ./maelstrom test -w txn-rw-register --bin $(PROJECT_PATH)/$(EXECUTABLE_NAME) --node-count 2 --concurrency 2n --time-limit 20 --rate 1000 --consistency-models read-committed --availability total --nemesis partition

run-%:
	go build -o $(EXECUTABLE_NAME) -ldflags "-X main.AppType=$*"
	cd -P $(MAELSTROM_PATH); $($*)
	rm -f $(EXECUTABLE_NAME)

serve:
	cd -P $(MAELSTROM_PATH); ./maelstrom serve
