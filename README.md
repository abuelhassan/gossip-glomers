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

# Grow-Only Counter
The KV store provided by maelstrom, creates separate node(s) that work as a KV store.

I utilized the KV store. And used the CompareAndSwap function to assure the correctness of the sum.
 
1. Read sum from KV (curSum)
2. CompareAndSwap(curSum, curSum+delta)

If the second step returned the error PreconditionFailed, that means the value was updated between the two steps,
in which case I just retry starting from step 1.

Note: I had to read a random number to the KV store, so that the last read is consistent as the challenge requires.
This can be avoided if using the Linearizable KV store! But the point of the challenge is to use sequentially consistent
stores.