package healthcheck

import (
	istioexternalapiv1 "github.com/rodbate/istio-external-service-operator/api/v1"
	"github.com/rodbate/istio-external-service-operator/pkg/healthcheck/results"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sync"
)

var (
	logger         = ctrl.Log.WithName("HealthCheckManager")
	DefaultManager = NewManager()
)

type Manager struct {
	rw                sync.RWMutex
	workers           map[results.EndpointKey]*worker
	hostPortEndpoints map[results.HostPortEndpointKey]*results.EndpointKey

	resultManager *results.Manager
}

func NewManager() *Manager {
	return &Manager{
		workers:           map[results.EndpointKey]*worker{},
		hostPortEndpoints: map[results.HostPortEndpointKey]*results.EndpointKey{},
		resultManager:     results.NewManager(),
	}
}

func AddEndpoint(
	externalService *istioexternalapiv1.IstioExternalService,
	serviceEntry *istioexternalapiv1.IstioExternalServiceEntry,
	endpoint *istioexternalapiv1.IstioExternalServiceEndpoint,
) bool {
	return DefaultManager.AddEndpoint(externalService, serviceEntry, endpoint)
}

func (m *Manager) AddEndpoint(
	externalService *istioexternalapiv1.IstioExternalService,
	serviceEntry *istioexternalapiv1.IstioExternalServiceEntry,
	endpoint *istioexternalapiv1.IstioExternalServiceEndpoint,
) bool {
	m.rw.Lock()
	defer m.rw.Unlock()

	endpointKey := results.BuildEndpointKey(externalService, serviceEntry, endpoint)
	if _, ok := m.workers[endpointKey]; ok {
		return false
	}

	hostPortEndpointKey := results.BuildHostPortEndpointKey(externalService, serviceEntry, endpoint)
	if epk, ok := m.hostPortEndpoints[hostPortEndpointKey]; ok {
		//Endpoint health check config changed
		m.doRemoveEndpointByKey(*epk)
	}

	worker := newWorker(endpointKey, m.resultManager)
	m.workers[endpointKey] = worker
	m.hostPortEndpoints[hostPortEndpointKey] = &endpointKey
	go worker.start()
	logger.Info("Add an endpoint health check worker", "endpoint", endpointKey)
	return true
}

func RemoveEndpoint(
	externalService *istioexternalapiv1.IstioExternalService,
	serviceEntry *istioexternalapiv1.IstioExternalServiceEntry,
	endpoint *istioexternalapiv1.IstioExternalServiceEndpoint,
) bool {
	return DefaultManager.RemoveEndpoint(externalService, serviceEntry, endpoint)
}

func (m *Manager) RemoveEndpoint(
	externalService *istioexternalapiv1.IstioExternalService,
	serviceEntry *istioexternalapiv1.IstioExternalServiceEntry,
	endpoint *istioexternalapiv1.IstioExternalServiceEndpoint,
) bool {
	endpointKey := results.BuildEndpointKey(externalService, serviceEntry, endpoint)
	return m.RemoveEndpointByKey(endpointKey)
}

func RemoveEndpointByKey(endpointKey results.EndpointKey) bool {
	return DefaultManager.RemoveEndpointByKey(endpointKey)
}

func (m *Manager) RemoveEndpointByKey(endpointKey results.EndpointKey) bool {
	m.rw.Lock()
	defer m.rw.Unlock()
	return m.doRemoveEndpointByKey(endpointKey)
}

func (m *Manager) doRemoveEndpointByKey(endpointKey results.EndpointKey) bool {
	worker, ok := m.workers[endpointKey]
	if !ok {
		epk, ok := m.hostPortEndpoints[endpointKey.ToHostPortEndpointKey()]
		if !ok {
			return false
		}
		w, ok := m.workers[*epk]
		if !ok {
			return false
		}
		endpointKey = *epk
		worker = w
	}

	worker.stop()

	delete(m.workers, endpointKey)
	delete(m.hostPortEndpoints, endpointKey.ToHostPortEndpointKey())
	logger.Info("Remove an endpoint health check worker", "endpoint", endpointKey)
	return true
}

func ResultUpdates() <-chan results.Update {
	return DefaultManager.ResultUpdates()
}

func (m *Manager) ResultUpdates() <-chan results.Update {
	return m.resultManager.Updates()
}

func CleanUp(externalService types.NamespacedName, serviceName string) {
	DefaultManager.CleanUp(externalService, serviceName)
}

func (m *Manager) CleanUp(externalService types.NamespacedName, serviceName string) {
	filter := func(externalService types.NamespacedName, serviceName string, epk results.EndpointKey) bool {
		rs := externalService == epk.IstioExternalServiceName
		if serviceName != "" {
			rs = rs && serviceName == epk.ServiceName
		}
		return rs
	}

	m.rw.Lock()
	defer m.rw.Unlock()

	for epk := range m.workers {
		if filter(externalService, serviceName, epk) {
			m.doRemoveEndpointByKey(epk)
		}
	}
}

func Destroy() {
	DefaultManager.Destroy()
}

func (m *Manager) Destroy() {
	logger.Info("Destroying ... ")
	for k, v := range m.workers {
		m.RemoveEndpointByKey(k)
		v.waitForStopping()
	}
	logger.Info("Destroyed ... ")
}
