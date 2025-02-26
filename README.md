# Run locally
Assuming all steps of installation from https://fly.io/dist-sys/1/ are done.

Set up MAELSTROM_PATH into .zshrc/.bashrc with maelstrom directory.
No need for any extra setups. Just run the commands in the Makefile.

# Challenge 1 - Echo
```shell
make run-echo
```
Just following steps...

# Challenge 2 - Unique ID Generation
```shell
make run-generator
```
Just using `rand.Int64 + time.Now().UnixNano()`.  
It didn't pass until I used "math/rand/v2". Using "math/rand" was flaky and generated some duplicates.

# Challenge 3 - Broadcast
```shell
make run-broadcast
```
## Topology
Seems that a flat tree topology was suitable for all the sub-challenges.  
I hardcoded the root as "n0". So all nodes other than "n0" would only broadcast to "n0". 
While "n0" would broadcast to all nodes.

## Batching
Batching messages is used for the last challenge. Instead of broadcasting each message immediately. A batch of messages
is awaited to be sent together. In case the size of messages is not met during a configured duration, the current batch
is sent anyway, regardless of its size.  
The job of the time ticker (batching duration), is to make sure I don't get a stale buffer, as well putting a threshold 
for delaying the messages.

The broadcast handler expects two different types of messages. One with the key "message" that maelstrom sends. And
another with key "messages" that nodes use to communicate together (in batches).

With Batching duration = 500ms - Batch size = 50, it was good enough to achieve the challenge constraints
* msgs-per-op = ~4.5
* latencies: {0 0, 0.5 745, 0.95 981, 0.99 995, 1 1077} 

# Challenge 4 - Grow-Only Counter
```shell
make run-counter
```
The KV store provided by maelstrom, creates separate node(s) that work as a KV store.  
I utilized the KV store and used the CompareAndSwap function to assure the correctness of the sum.
 
1. Read sum from KV (curSum)
2. CompareAndSwap(curSum, curSum+delta)

If the second step returned the error PreconditionFailed, that means the value was updated between the two steps,
in which case I just retry starting from step 1.

Note: I had to write a random number to the KV store, so that the last read is consistent as the challenge requires.
This can be avoided if using the Linearizable KV store! But the point of the challenge is to use sequentially consistent
stores.

# Challenge 5 - Kafka-Style Logs
```shell
make run-kafka
```
Mentioned in the book "Designing Data-Intensive Applications", Linearizability is the illusion of having just one copy of the data.  
If a client queried X and received v2 of X, v1 should not be returned for any following queries.  
I have utilized the Linearizabile KV store to maintain an ordered offset for each key.

I have only implemented the multi node challenge. But I didn't try optimizations after.
