package retryer

import (
	"context"
	"math/rand/v2"
	"time"
)

func Retry(maxRetry int, dur time.Duration, fn func(context.Context) error) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := fn(ctx)
		cancel()
		if err == nil {
			return
		}
		if dur > 0 {
			time.Sleep(dur + time.Duration(rand.IntN(1001))*time.Nanosecond) // add jitter - max 1ms
		}
		if maxRetry == 0 {
			return
		}
		maxRetry--
	}
}
