package worker

type Worker interface {
	Do(func())
}

func New(n int) Worker {
	ch := make(chan msg)
	for i := 0; i < n; i++ {
		go func() {
			for {
				select {
				case m := <-ch:
					m.fn()
					m.done <- struct{}{}
				}
			}
		}()
	}
	return &worker{n: n, ch: ch}
}

type msg struct {
	fn   func()
	done chan struct{}
}

type worker struct {
	n  int
	ch chan msg
}

func (w *worker) Do(fn func()) {
	done := make(chan struct{}, 1)
	w.ch <- msg{fn, done}
	<-done
}
