package bootstrap

import (
	"context"
	"sync"
)

func cancelAndWait(cancel context.CancelFunc, done <-chan struct{}) context.CancelFunc {
	if cancel == nil {
		return nil
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			if done != nil {
				<-done
			}
		})
	}
}
