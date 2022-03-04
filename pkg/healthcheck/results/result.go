package results

import (
	"fmt"
	istioexternalapiv1 "github.com/rodbate/istio-external-service-operator/api/v1"
	"k8s.io/apimachinery/pkg/types"
	"sync"
)

type Result int

const (
	Unknown Result = iota - 1
	Success
	Failure
)

func (r Result) String() string {
	switch r {
	case Success:
		return "Success"
	case Failure:
		return "Failure"
	default:
		return "Unknown"
	}
}

type EndpointKey struct {
	IstioExternalServiceName types.NamespacedName
	ServiceName              string
	Endpoint                 istioexternalapiv1.IstioExternalServiceEndpoint
}

func (key EndpointKey) String() string {
	return fmt.Sprintf("%v/%v/%v", key.IstioExternalServiceName, key.ServiceName, key.Endpoint)
}

func (key EndpointKey) ToHostPortEndpointKey() HostPortEndpointKey {
	return HostPortEndpointKey{
		IstioExternalServiceName: key.IstioExternalServiceName,
		ServiceName:              key.ServiceName,
		Host:                     key.Endpoint.Host,
		Port:                     key.Endpoint.Port,
	}
}

type HostPortEndpointKey struct {
	IstioExternalServiceName types.NamespacedName
	ServiceName              string
	Host                     string
	Port                     int32
}

func (key HostPortEndpointKey) String() string {
	return fmt.Sprintf("%v/%v/%v:%v", key.IstioExternalServiceName, key.ServiceName, key.Host, key.Port)
}

type Update struct {
	EndpointKey
	Result
}

type Manager struct {
	rw    sync.RWMutex
	cache map[EndpointKey]Result

	updates chan Update
}

func NewManager() *Manager {
	return &Manager{
		cache:   map[EndpointKey]Result{},
		updates: make(chan Update, 30),
	}
}

func BuildEndpointKey(
	externalService *istioexternalapiv1.IstioExternalService,
	serviceEntry *istioexternalapiv1.IstioExternalServiceEntry,
	endpoint *istioexternalapiv1.IstioExternalServiceEndpoint,
) EndpointKey {
	return EndpointKey{
		IstioExternalServiceName: types.NamespacedName{
			Namespace: externalService.Namespace,
			Name:      externalService.Name,
		},
		ServiceName: serviceEntry.Name,
		Endpoint:    *endpoint,
	}
}

func BuildHostPortEndpointKey(
	externalService *istioexternalapiv1.IstioExternalService,
	serviceEntry *istioexternalapiv1.IstioExternalServiceEntry,
	endpoint *istioexternalapiv1.IstioExternalServiceEndpoint,
) HostPortEndpointKey {
	return HostPortEndpointKey{
		IstioExternalServiceName: types.NamespacedName{
			Namespace: externalService.Namespace,
			Name:      externalService.Name,
		},
		ServiceName: serviceEntry.Name,
		Host:        endpoint.Host,
		Port:        endpoint.Port,
	}
}

func (m *Manager) Get(key EndpointKey) Result {
	m.rw.RLock()
	defer m.rw.RUnlock()
	rs, ok := m.cache[key]
	if !ok {
		rs = Unknown
	}
	return rs
}

func (m *Manager) Set(key EndpointKey, rs Result) {
	if m.setInternal(key, rs) {
		m.updates <- Update{
			EndpointKey: key,
			Result:      rs,
		}
	}
}

func (m *Manager) setInternal(key EndpointKey, rs Result) bool {
	m.rw.Lock()
	defer m.rw.Unlock()
	if prev, ok := m.cache[key]; ok && prev == rs {
		return false
	}

	m.cache[key] = rs
	return true
}

func (m *Manager) Delete(key EndpointKey) {
	m.rw.Lock()
	defer m.rw.Unlock()
	delete(m.cache, key)
}

func (m *Manager) Updates() <-chan Update {
	return m.updates
}
