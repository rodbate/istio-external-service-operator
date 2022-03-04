package resources

import (
	"context"
	"errors"
	"fmt"
	istioexternalapiv1 "github.com/rodbate/istio-external-service-operator/api/v1"
	"github.com/rodbate/istio-external-service-operator/pkg/healthcheck"
	"github.com/rodbate/istio-external-service-operator/pkg/healthcheck/results"
	istionetworkingapiv1beta1 "istio.io/api/networking/v1beta1"
	istionetworkingapisv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"net"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const ManagedByLabelName = "external-service.istio.rodbate.github.com/managed-by"

var logger = ctrl.Log.WithName("SyncResource")

func SyncExternalService(
	ctx context.Context,
	ies *istioexternalapiv1.IstioExternalService,
	serviceEntry *istioexternalapiv1.IstioExternalServiceEntry,
	c client.Client,
	scheme *runtime.Scheme,
) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceEntry.Name,
			Namespace: ies.Namespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
	}
	err := c.Get(ctx, client.ObjectKeyFromObject(service), service)
	if err != nil && !k8sapierrors.IsNotFound(err) {
		return errors.New("failed to get service: " + err.Error())
	}

	if len(service.ObjectMeta.Labels) == 0 {
		service.ObjectMeta.Labels = map[string]string{}
	}
	service.ObjectMeta.Labels[ManagedByLabelName] = ies.Name

	ipFamilyPolicy := corev1.IPFamilyPolicySingleStack
	service.Spec = corev1.ServiceSpec{
		IPFamilies: []corev1.IPFamily{
			corev1.IPv4Protocol,
		},
		IPFamilyPolicy: &ipFamilyPolicy,
		ClusterIP:      "None",
		Ports: []corev1.ServicePort{
			{
				Port:     80,
				Protocol: corev1.ProtocolTCP,
			},
		},
	}

	if err := controllerutil.SetOwnerReference(ies, service, scheme); err != nil {
		return err
	}

	if k8sapierrors.IsNotFound(err) {
		if err := c.Create(ctx, service); err != nil {
			return err
		}
	} else {
		if err := c.Update(ctx, service); err != nil {
			return err
		}
	}
	return nil
}

func SyncExternalServiceEndpoints(
	ctx context.Context,
	ies *istioexternalapiv1.IstioExternalService,
	serviceEntry *istioexternalapiv1.IstioExternalServiceEntry,
	c client.Client,
	scheme *runtime.Scheme,
) error {
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceEntry.Name,
			Namespace: ies.Namespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
	}
	err := c.Get(ctx, client.ObjectKeyFromObject(endpoints), endpoints)
	if err != nil && !k8sapierrors.IsNotFound(err) {
		return errors.New("failed to get endpoints: " + err.Error())
	}

	if len(endpoints.ObjectMeta.Labels) == 0 {
		endpoints.ObjectMeta.Labels = map[string]string{}
	}
	endpoints.ObjectMeta.Labels[ManagedByLabelName] = ies.Name

	desiredEndpointsMap := make(map[string]istioexternalapiv1.IstioExternalServiceEndpoint, len(serviceEntry.Endpoints))
	desiredEndpoints := make([]istioexternalapiv1.IstioExternalServiceEndpoint, 0, len(serviceEntry.Endpoints))
	for _, ep := range serviceEntry.Endpoints {
		serviceEndpoints, err := expandEndpoints(ep)
		if err != nil {
			return err
		}
		desiredEndpoints = append(desiredEndpoints, serviceEndpoints...)
		for _, sep := range serviceEndpoints {
			desiredEndpointsMap[fmt.Sprintf("%v:%v", sep.Host, sep.Port)] = sep
		}
	}

	var currentReadyEndpoints []istioexternalapiv1.IstioExternalServiceEndpoint
	var currentNotReadyEndpoints []istioexternalapiv1.IstioExternalServiceEndpoint
	var toDeleteEndpoints []istioexternalapiv1.IstioExternalServiceEndpoint
	for _, es := range endpoints.Subsets {
		for _, port := range es.Ports {
			for _, readyAddr := range es.Addresses {
				if ep, ok := desiredEndpointsMap[fmt.Sprintf("%v:%v", readyAddr.IP, port.Port)]; ok {
					currentReadyEndpoints = append(currentReadyEndpoints, ep)
				} else {
					toDeleteEndpoints = append(toDeleteEndpoints, buildEndpoint(readyAddr.IP, port.Port))
				}
			}

			for _, notReadyAddr := range es.NotReadyAddresses {
				if ep, ok := desiredEndpointsMap[fmt.Sprintf("%v:%v", notReadyAddr.IP, port.Port)]; ok {
					currentNotReadyEndpoints = append(currentNotReadyEndpoints, ep)
				} else {
					toDeleteEndpoints = append(toDeleteEndpoints, buildEndpoint(notReadyAddr.IP, port.Port))
				}
			}
		}
	}

	currentNotReadyEndpoints = append(currentNotReadyEndpoints, desiredEndpoints...)
	endpoints.Subsets = buildSubsets(currentReadyEndpoints, currentNotReadyEndpoints)

	if err := controllerutil.SetOwnerReference(ies, endpoints, scheme); err != nil {
		return err
	}
	if k8sapierrors.IsNotFound(err) {
		if err := c.Create(ctx, endpoints); err != nil {
			return err
		}
	} else {
		if err := c.Update(ctx, endpoints); err != nil {
			return err
		}
	}

	for _, ep := range append(currentReadyEndpoints, currentNotReadyEndpoints...) {
		healthcheck.AddEndpoint(ies, serviceEntry, &ep)
	}

	for _, ep := range toDeleteEndpoints {
		healthcheck.RemoveEndpoint(ies, serviceEntry, &ep)
	}

	return nil
}

func expandEndpoints(ep istioexternalapiv1.IstioExternalServiceEndpoint) ([]istioexternalapiv1.IstioExternalServiceEndpoint, error) {
	if net.ParseIP(ep.Host) != nil {
		return []istioexternalapiv1.IstioExternalServiceEndpoint{ep}, nil
	}

	ips, err := net.LookupIP(ep.Host)
	if err != nil {
		return nil, err
	}

	endpoints := make([]istioexternalapiv1.IstioExternalServiceEndpoint, 0, len(ips))
	for _, ip := range ips {
		endpoints = append(endpoints, istioexternalapiv1.IstioExternalServiceEndpoint{
			Host:        ip.String(),
			Port:        ep.Port,
			HealthCheck: ep.HealthCheck,
		})
	}
	return endpoints, nil
}

func buildEndpoint(ip string, port int32) istioexternalapiv1.IstioExternalServiceEndpoint {
	return istioexternalapiv1.IstioExternalServiceEndpoint{
		Host: ip,
		Port: port,
	}
}

func buildSubsets(
	readyEndpoints []istioexternalapiv1.IstioExternalServiceEndpoint,
	notReadyEndpoints []istioexternalapiv1.IstioExternalServiceEndpoint,
) []corev1.EndpointSubset {
	var subsets []corev1.EndpointSubset
	addedEndpoints := sets.NewString()
	for _, ep := range readyEndpoints {
		if addedEndpoints.Has(ep.String()) {
			continue
		}

		addedEndpoints.Insert(ep.String())
		subsets = append(subsets, corev1.EndpointSubset{
			Addresses: []corev1.EndpointAddress{
				{
					IP: ep.Host,
				},
			},
			Ports: []corev1.EndpointPort{
				{
					Port: ep.Port,
				},
			},
		})
	}

	for _, ep := range notReadyEndpoints {
		if addedEndpoints.Has(ep.String()) {
			continue
		}

		addedEndpoints.Insert(ep.String())
		subsets = append(subsets, corev1.EndpointSubset{
			NotReadyAddresses: []corev1.EndpointAddress{
				{
					IP: ep.Host,
				},
			},
			Ports: []corev1.EndpointPort{
				{
					Port: ep.Port,
				},
			},
		})
	}
	return subsets
}

func SyncExternalIstioServiceEntry(
	ctx context.Context,
	ies *istioexternalapiv1.IstioExternalService,
	serviceEntry *istioexternalapiv1.IstioExternalServiceEntry,
	c client.Client,
	scheme *runtime.Scheme,
) error {
	istioServiceEntry := &istionetworkingapisv1beta1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceEntry.Name,
			Namespace: ies.Namespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceEntry",
			APIVersion: istionetworkingapisv1beta1.SchemeGroupVersion.String(),
		},
	}
	err := c.Get(ctx, client.ObjectKeyFromObject(istioServiceEntry), istioServiceEntry)
	if err != nil && !k8sapierrors.IsNotFound(err) {
		return errors.New("failed to get ServiceEntry: " + err.Error())
	}

	if len(istioServiceEntry.ObjectMeta.Labels) == 0 {
		istioServiceEntry.ObjectMeta.Labels = map[string]string{}
	}
	istioServiceEntry.ObjectMeta.Labels[ManagedByLabelName] = ies.Name

	istioServiceEntry.Spec = istionetworkingapiv1beta1.ServiceEntry{
		Hosts: []string{
			fmt.Sprintf("%v.%v.svc.cluster.local", istioServiceEntry.Name, istioServiceEntry.Namespace),
		},
		Ports: []*istionetworkingapiv1beta1.Port{
			{
				Name:     "http",
				Number:   80,
				Protocol: "HTTP",
			},
		},
		Location:   istionetworkingapiv1beta1.ServiceEntry_MESH_EXTERNAL,
		Resolution: istionetworkingapiv1beta1.ServiceEntry_STATIC,
		ExportTo: []string{
			".",
		},
	}

	if err := controllerutil.SetOwnerReference(ies, istioServiceEntry, scheme); err != nil {
		return err
	}

	if k8sapierrors.IsNotFound(err) {
		if err := c.Create(ctx, istioServiceEntry); err != nil {
			return err
		}
	} else {
		if err := c.Update(ctx, istioServiceEntry); err != nil {
			return err
		}
	}
	return nil
}

func SyncEndpointsStatus(c client.Client) {
	go func() {
		for {
			select {
			case update, ok := <-healthcheck.ResultUpdates():
				if !ok {
					return
				}
				updateEndpointsStatus(c, update)
			}
		}
	}()
}

func updateEndpointsStatus(c client.Client, update results.Update) {
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      update.ServiceName,
			Namespace: update.IstioExternalServiceName.Namespace,
		},
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(endpoints), endpoints); err != nil {
		logger.Error(err, "failed to get endpoints", "endpoints", endpoints)
	}

	addresses := getEndpointsAddresses(endpoints)

	readyIndex := -1
	notReadyIndex := -1
	for i, ep := range addresses.readyEndpoints {
		if ep.Host == update.Endpoint.Host && ep.Port == update.Endpoint.Port && update.Result != results.Success {
			readyIndex = i
		}
	}

	if readyIndex == -1 {
		for i, ep := range addresses.notReadyEndpoints {
			if ep.Host == update.Endpoint.Host && ep.Port == update.Endpoint.Port && update.Result == results.Success {
				notReadyIndex = i
			}
		}
	}

	if readyIndex == -1 && notReadyIndex == -1 {
		return
	}

	newReadyEndpoints := addresses.readyEndpoints
	newNotReadyEndpoints := addresses.notReadyEndpoints

	if readyIndex != -1 {
		newReadyEndpoints = append(newReadyEndpoints[:readyIndex], newReadyEndpoints[readyIndex+1:]...)
		newNotReadyEndpoints = append(newNotReadyEndpoints, addresses.readyEndpoints[readyIndex])
	} else {
		newNotReadyEndpoints = append(newNotReadyEndpoints[:notReadyIndex], newNotReadyEndpoints[notReadyIndex+1:]...)
		newReadyEndpoints = append(newReadyEndpoints, addresses.notReadyEndpoints[notReadyIndex])
	}

	endpoints.Subsets = buildSubsets(newReadyEndpoints, newNotReadyEndpoints)

	if err := c.Update(context.Background(), endpoints); err != nil {
		logger.Error(err, "failed to update endpoints", "endpoints", endpoints)
	}

	logger.Info("Updated Endpoint status", "Endpoint", update.EndpointKey, "TargetState", update.Result)
}

type endpointsAddresses struct {
	readyEndpoints    []istioexternalapiv1.IstioExternalServiceEndpoint
	notReadyEndpoints []istioexternalapiv1.IstioExternalServiceEndpoint
}

func getEndpointsAddresses(ep *corev1.Endpoints) *endpointsAddresses {
	var readyEndpoints []istioexternalapiv1.IstioExternalServiceEndpoint
	var notReadyEndpoints []istioexternalapiv1.IstioExternalServiceEndpoint

	for _, es := range ep.Subsets {
		for _, port := range es.Ports {
			for _, readyAddr := range es.Addresses {
				readyEndpoints = append(readyEndpoints, buildEndpoint(readyAddr.IP, port.Port))
			}

			for _, notReadyAddr := range es.NotReadyAddresses {
				notReadyEndpoints = append(notReadyEndpoints, buildEndpoint(notReadyAddr.IP, port.Port))
			}
		}
	}

	return &endpointsAddresses{
		readyEndpoints:    readyEndpoints,
		notReadyEndpoints: notReadyEndpoints,
	}
}
