package hive

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
)

const (
	defaultChanSize = 256
)

// ErrJobTimeout and others are errors related to workers
var (
	ErrJobTimeout = errors.New("job timeout")
)

type worker struct {
	runner   Runnable
	workChan chan JobReference
	store    Storage
	options  workerOpts

	threads    []*workThread
	threadLock sync.Mutex

	started atomic.Value
}

// newWorker creates a new goWorker
func newWorker(runner Runnable, store Storage, opts workerOpts) *worker {
	w := &worker{
		runner:     runner,
		workChan:   make(chan JobReference, defaultChanSize),
		store:      store,
		options:    opts,
		threads:    make([]*workThread, opts.poolSize),
		threadLock: sync.Mutex{},
		started:    atomic.Value{},
	}

	w.started.Store(false)

	return w
}

func (w *worker) schedule(job JobReference) {
	go func() {
		w.workChan <- job
	}()
}

func (w *worker) start(doFunc DoFunc) error {
	// this should only be run once per worker
	if isStarted := w.started.Load().(bool); isStarted {
		return nil
	}

	w.started.Store(true)

	started := 0
	attempts := 0

	for {
		// fill the "pool" with workThreads
		for i := started; i < w.options.poolSize; i++ {
			wt := newWorkThread(w.runner, w.workChan, w.store, w.options.jobTimeoutSeconds)

			// give the runner opportunity to provision resources if needed
			if err := w.runner.OnStart(); err != nil {
				fmt.Println(errors.Wrapf(err, "Runnable returned OnStart error, will retry in %ds", w.options.retrySecs))
				break
			} else {
				started++
			}

			wt.run(doFunc)

			w.threads[i] = wt
		}

		if started == w.options.poolSize {
			break
		} else {
			if attempts >= w.options.numRetries {
				return fmt.Errorf("attempted to start worker %d times, Runnable returned error each time", w.options.numRetries)
			}

			attempts++
			<-time.After(time.Duration(time.Second * time.Duration(w.options.retrySecs)))
		}
	}

	return nil
}

func (w *worker) isStarted() bool {
	return w.started.Load().(bool)
}

type workThread struct {
	runner         Runnable
	workChan       chan JobReference
	store          Storage
	timeoutSeconds int
	ctx            context.Context
	cancelFunc     context.CancelFunc
}

func newWorkThread(runner Runnable, workChan chan JobReference, store Storage, timeoutSeconds int) *workThread {
	ctx, cancelFunc := context.WithCancel(context.Background())

	wt := &workThread{
		runner:         runner,
		workChan:       workChan,
		store:          store,
		timeoutSeconds: timeoutSeconds,
		ctx:            ctx,
		cancelFunc:     cancelFunc,
	}

	return wt
}

func (wt *workThread) run(doFunc DoFunc) {
	go func() {
		for {
			// die if the context has been cancelled
			if wt.ctx.Err() != nil {
				break
			}

			// wait for the next job
			jobRef := <-wt.workChan

			// fetch the full job from storage
			job, err := wt.store.Get(jobRef.uuid)
			if err != nil {
				jobRef.result.sendErr(err)
				continue
			}

			var result interface{}

			if wt.timeoutSeconds == 0 {
				result, err = wt.runner.Run(job, doFunc)
			} else {
				result, err = wt.runWithTimeout(job, doFunc)
			}

			wt.store.AddResult(job.UUID(), result, err)

			if err != nil {
				jobRef.result.sendErr(err)
				continue
			}

			jobRef.result.sendResult(result)
		}
	}()
}

func (wt *workThread) runWithTimeout(job Job, doFunc DoFunc) (interface{}, error) {
	resultChan := make(chan interface{})
	errChan := make(chan error)

	go func() {
		result, err := wt.runner.Run(job, doFunc)
		if err != nil {
			errChan <- err
		} else {
			resultChan <- result
		}
	}()

	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errChan:
		return nil, err
	case <-time.After(time.Duration(time.Second * time.Duration(wt.timeoutSeconds))):
		return nil, ErrJobTimeout
	}
}

func (wt *workThread) Stop() {
	wt.cancelFunc()
}

type workerOpts struct {
	jobType           string
	poolSize          int
	jobTimeoutSeconds int
	numRetries        int
	retrySecs         int
}

func defaultOpts(jobType string) workerOpts {
	o := workerOpts{
		jobType:           jobType,
		poolSize:          1,
		jobTimeoutSeconds: 0,
		retrySecs:         3,
		numRetries:        5,
	}

	return o
}
