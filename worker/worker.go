package worker

type Worker interface {
	Do(func())
}

func New(n int) Worker {
	ch := make(chan func())
	for i := 0; i < n; i++ {
		go func() {
			for {
				select {
				case fn := <-ch:
					fn()
				}
			}
		}()
	}
	return &worker{n: n, ch: ch}
}

type worker struct {
	n  int
	ch chan func()
}

func (w *worker) Do(fn func()) {
	done := make(chan struct{}, 1)
	go func() {
		fn()
		done <- struct{}{}
	}()
	w.ch <- fn
}
