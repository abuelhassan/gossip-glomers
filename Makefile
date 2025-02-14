.PHONY: run-echo

EXECUTABLE_NAME = out
PROJECT_PATH = $(GOPATH)/src/github.com/abuelhassan/gossip-glomers

run-echo:
	cd echo; go build -o $(EXECUTABLE_NAME)
	cd -P $(MAELSTROM_PATH); ./maelstrom test -w echo --bin $(PROJECT_PATH)/echo/out --node-count 1 --time-limit 10
	rm -f echo/$(EXECUTABLE_NAME)