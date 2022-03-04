package controllers

import (
	"context"
	istioexternalapiv1 "github.com/rodbate/istio-external-service-operator/api/v1"
	"github.com/rodbate/istio-external-service-operator/pkg/healthcheck"
	"github.com/rodbate/istio-external-service-operator/pkg/resources"
	istionetworkingapisv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IstioExternalServiceReconciler reconciles a IstioExternalService object
type IstioExternalServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=istio.rodbate.github.com,resources=istioexternalservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=istio.rodbate.github.com,resources=istioexternalservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=istio.rodbate.github.com,resources=istioexternalservices/finalizers,verbs=update

//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=serviceentries,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *IstioExternalServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("Start to reconcile loop")
	defer logger.Info("Finish to reconcile loop")
	ies := &istioexternalapiv1.IstioExternalService{}
	err := r.Get(ctx, req.NamespacedName, ies)
	if k8sapierrors.IsNotFound(err) {
		healthcheck.CleanUp(req.NamespacedName, "")
		return ctrl.Result{}, nil
	} else if err != nil {
		logger.Error(err, "Failed to get IstioExternalService")
		return ctrl.Result{}, err
	}

	if err := r.syncExternalServiceResources(ctx, ies); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *IstioExternalServiceReconciler) syncExternalServiceResources(
	ctx context.Context,
	ies *istioexternalapiv1.IstioExternalService,
) error {
	desiredServices := sets.NewString()
	for _, srv := range ies.Spec.Services {
		if desiredServices.Has(srv.Name) {
			continue
		}
		desiredServices.Insert(srv.Name)

		if err := r.syncExternalServiceEntryResources(ctx, &srv, ies); err != nil {
			return err
		}
	}

	serviceList := &corev1.ServiceList{}
	if err := r.List(
		ctx,
		serviceList,
		client.InNamespace(ies.Namespace),
		client.MatchingLabels{
			resources.ManagedByLabelName: ies.Name,
		},
	); err != nil {
		return err
	}
	for _, srv := range serviceList.Items {
		if desiredServices.Has(srv.Name) {
			continue
		}

		if err := r.Client.Delete(ctx, &srv); err != nil {
			return err
		}

		healthcheck.CleanUp(client.ObjectKeyFromObject(ies), srv.Name)
	}

	endpointsList := &corev1.EndpointsList{}
	if err := r.List(
		ctx,
		endpointsList,
		client.InNamespace(ies.Namespace),
		client.MatchingLabels{
			resources.ManagedByLabelName: ies.Name,
		},
	); err != nil {
		return err
	}
	for _, ep := range endpointsList.Items {
		if desiredServices.Has(ep.Name) {
			continue
		}

		if err := r.Client.Delete(ctx, &ep); err != nil {
			return err
		}
	}

	serviceEntryList := &istionetworkingapisv1beta1.ServiceEntryList{}
	if err := r.List(
		ctx,
		serviceEntryList,
		client.InNamespace(ies.Namespace),
		client.MatchingLabels{
			resources.ManagedByLabelName: ies.Name,
		},
	); err != nil {
		return err
	}
	for _, ie := range serviceEntryList.Items {
		if desiredServices.Has(ie.Name) {
			continue
		}

		if err := r.Client.Delete(ctx, &ie); err != nil {
			return err
		}
	}

	return nil
}

func (r *IstioExternalServiceReconciler) syncExternalServiceEntryResources(
	ctx context.Context,
	serviceEntry *istioexternalapiv1.IstioExternalServiceEntry,
	ies *istioexternalapiv1.IstioExternalService,
) error {
	//sync Service
	if err := resources.SyncExternalService(ctx, ies, serviceEntry, r.Client, r.Scheme); err != nil {
		return err
	}

	//sync Endpoints
	if err := resources.SyncExternalServiceEndpoints(ctx, ies, serviceEntry, r.Client, r.Scheme); err != nil {
		return err
	}

	//sync ServiceEntry
	if err := resources.SyncExternalIstioServiceEntry(ctx, ies, serviceEntry, r.Client, r.Scheme); err != nil {
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *IstioExternalServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	resources.SyncEndpointsStatus(r.Client)
	return ctrl.NewControllerManagedBy(mgr).
		For(&istioexternalapiv1.IstioExternalService{}).
		Complete(r)
}
