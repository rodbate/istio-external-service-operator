package healthcheck

import (
	"github.com/rodbate/istio-external-service-operator/pkg/healthcheck/results"
	"go.uber.org/atomic"
	"net"
	"time"
)

type worker struct {
	endpointKey results.EndpointKey

	resultManager *results.Manager

	lastResult             results.Result
	consecutiveResultCount int32

	stopped        atomic.Bool
	stopCh         chan struct{}
	stopCompleteCh chan struct{}
}

func newWorker(
	endpointKey results.EndpointKey,
	resultManager *results.Manager,
) *worker {
	return &worker{
		endpointKey:            endpointKey,
		resultManager:          resultManager,
		lastResult:             results.Unknown,
		consecutiveResultCount: 0,
		stopCh:                 make(chan struct{}),
		stopCompleteCh:         make(chan struct{}),
	}
}

func (w *worker) start() {
	ticker := time.NewTicker(time.Duration(w.endpointKey.Endpoint.HealthCheck.PeriodSeconds) * time.Second)
	defer func() {
		ticker.Stop()
		close(w.stopCh)

		w.resultManager.Delete(w.endpointKey)
		logger.Info("Stopped health check worker", "endpoint", w.endpointKey)
		close(w.stopCompleteCh)
	}()

	for {
		w.probe()

		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
		}
	}
}

func (w *worker) probe() {
	conn, err := net.DialTimeout(
		"tcp",
		w.endpointKey.Endpoint.String(),
		time.Duration(w.endpointKey.Endpoint.HealthCheck.TimeoutSeconds)*time.Second,
	)

	var result results.Result
	if err != nil {
		result = results.Failure
	} else {
		result = results.Success
		_ = conn.Close()
	}

	if result == w.lastResult {
		w.consecutiveResultCount++
	} else {
		w.lastResult = result
		w.consecutiveResultCount = 1
	}

	if (result == results.Success && w.consecutiveResultCount < w.endpointKey.Endpoint.HealthCheck.SuccessThreshold) ||
		(result == results.Failure && w.consecutiveResultCount < w.endpointKey.Endpoint.HealthCheck.FailureThreshold) {
		return
	}

	w.resultManager.Set(w.endpointKey, result)
}

func (w *worker) stop() {
	if w.stopped.Load() {
		return
	}

	select {
	case w.stopCh <- struct{}{}:
		w.stopped.Store(true)
	default:
	}
}

func (w *worker) waitForStopping() {
	<-w.stopCompleteCh
}
