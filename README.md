# Run locally
Assuming all steps of installation from https://fly.io/dist-sys/1/ are done.

Set up MAELSTROM_PATH into .zshrc/.bashrc with maelstrom directory.
No need for any extra setups. Just run the commands in the Makefile.

# Unique IDs
The solution is flaky. Some runs would pass, others would generate a few duplicates.

Tried using rand.Seed, though it's mentioned in the documentation it's not required anymore in Go 1.23.

# Broadcast

## Topology
 Seems that a flat tree topology was suitable for all the sub-challenges.
 I hardcoded the root as "n0". So all nodes other than "n0" would only broadcast to "n0".
 While "n0" would broadcast to all nodes.

## Batching
Batching messages is used for the last challenge. Instead of broadcasting each message immediately. A batch of messages
is awaited to be sent together. In case the size of messages is not met during a configured duration, the current batch
is sent anyway, regardless of its size.

The broadcast handler expects two different types of messages. One with the key "message" that maelstrom sends. And 
another with key "messages" that nodes use to communicate together.
It could have been better to just introduce a separate handler for internal communication.

# Grow-Only Counter
Went for the simplest solution of just reading the local sum of all nodes with every "read" command.

For the challenge of network partitions, I have considered two options
* Retry on failure -> Consistency
* Cache the local results locally and use the cache on failures -> Availability

But I went with an easier solution of just ignoring the failures, and it was good enough for the challenge.