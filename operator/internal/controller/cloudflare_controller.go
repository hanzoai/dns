package controller

import (
	"context"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dnsv1alpha1 "github.com/hanzoai/dns-operator/api/v1alpha1"
	"github.com/hanzoai/dns-operator/internal/cloudflare"
)

const cloudflareFinalizer = "dns.hanzo.ai/cloudflare-cleanup"

// CloudflareReconciler syncs DnsRecords with syncToCloudflare=true to Cloudflare DNS.
// It owns the record's lifecycle at the edge: create, update, and (via finalizer) delete.
type CloudflareReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	CF     *cloudflare.Client
}

// +kubebuilder:rbac:groups=dns.hanzo.ai,resources=dnsrecords,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=dns.hanzo.ai,resources=dnsrecords/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dns.hanzo.ai,resources=dnszones,verbs=get;list;watch

func (r *CloudflareReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var record dnsv1alpha1.DnsRecord
	if err := r.Get(ctx, req.NamespacedName, &record); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	zoneID := r.zoneID(ctx, record.Namespace, record.Spec.ZoneRef)

	// Deletion: drop the edge record, then release the finalizer.
	if !record.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&record, cloudflareFinalizer) {
			if zoneID != "" && record.Status.CloudflareRecordID != "" && r.CF != nil {
				if err := r.CF.DeleteRecord(ctx, zoneID, record.Status.CloudflareRecordID); err != nil {
					return ctrl.Result{}, err
				}
			}
			controllerutil.RemoveFinalizer(&record, cloudflareFinalizer)
			if err := r.Update(ctx, &record); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Opted out: clean up any edge record we created, release the finalizer.
	if !record.Spec.SyncToCloudflare {
		if controllerutil.ContainsFinalizer(&record, cloudflareFinalizer) {
			if zoneID != "" && record.Status.CloudflareRecordID != "" && r.CF != nil {
				if err := r.CF.DeleteRecord(ctx, zoneID, record.Status.CloudflareRecordID); err != nil {
					return ctrl.Result{}, err
				}
			}
			controllerutil.RemoveFinalizer(&record, cloudflareFinalizer)
			if err := r.Update(ctx, &record); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if r.CF == nil {
		logger.Info("cloudflare sync disabled: no API token configured")
		return ctrl.Result{}, nil
	}

	if zoneID == "" {
		logger.Info("skipping CF sync: parent zone missing cloudflareZoneId", "zoneRef", record.Spec.ZoneRef)
		return r.markError(ctx, &record)
	}

	if controllerutil.AddFinalizer(&record, cloudflareFinalizer) {
		if err := r.Update(ctx, &record); err != nil {
			return ctrl.Result{}, err
		}
	}

	desired := cloudflare.Record{
		Type:     record.Spec.Type,
		Name:     fqdn(record.Spec.Name, r.zoneName(ctx, record.Namespace, record.Spec.ZoneRef)),
		Content:  record.Spec.Content,
		TTL:      ttlOrDefault(record.Spec.TTL),
		Proxied:  record.Spec.Proxied,
		Priority: record.Spec.Priority,
	}

	id := record.Status.CloudflareRecordID
	if id == "" {
		// Adopt an existing edge record of the same name+type if present, else create.
		existing, err := r.CF.FindRecord(ctx, zoneID, desired.Name, desired.Type)
		if err != nil {
			return ctrl.Result{}, err
		}
		if existing != nil {
			id = existing.ID
			if err := r.CF.UpdateRecord(ctx, zoneID, id, desired); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			if id, err = r.CF.CreateRecord(ctx, zoneID, desired); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else if err := r.CF.UpdateRecord(ctx, zoneID, id, desired); err != nil {
		return ctrl.Result{}, err
	}

	now := metav1.Now()
	record.Status.Phase = "Active"
	record.Status.CloudflareRecordID = id
	record.Status.LastSyncTime = &now
	if err := r.Status().Update(ctx, &record); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// zoneID resolves the Cloudflare zone ID for a record's ZoneRef.
func (r *CloudflareReconciler) zoneID(ctx context.Context, ns, zoneRef string) string {
	if z := r.zone(ctx, ns, zoneRef); z != nil {
		return z.Spec.CloudflareZoneID
	}
	return ""
}

func (r *CloudflareReconciler) zoneName(ctx context.Context, ns, zoneRef string) string {
	if z := r.zone(ctx, ns, zoneRef); z != nil {
		return z.Spec.Zone
	}
	return ""
}

func (r *CloudflareReconciler) zone(ctx context.Context, ns, zoneRef string) *dnsv1alpha1.DnsZone {
	var zones dnsv1alpha1.DnsZoneList
	if err := r.List(ctx, &zones, client.InNamespace(ns)); err != nil {
		return nil
	}
	for i := range zones.Items {
		if zones.Items[i].Name == zoneRef {
			return &zones.Items[i]
		}
	}
	return nil
}

func (r *CloudflareReconciler) markError(ctx context.Context, record *dnsv1alpha1.DnsRecord) (ctrl.Result, error) {
	record.Status.Phase = "Error"
	if err := r.Status().Update(ctx, record); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// fqdn renders a record name into the full Cloudflare name. "@" → apex, an
// already-qualified name is left alone, otherwise the zone is appended.
func fqdn(name, zone string) string {
	if zone == "" || name == zone {
		return name
	}
	if name == "@" || name == "" {
		return zone
	}
	if strings.HasSuffix(name, "."+zone) {
		return name
	}
	return name + "." + zone
}

func ttlOrDefault(ttl int) int {
	if ttl <= 0 {
		return 1 // Cloudflare: 1 = "automatic"
	}
	return ttl
}

func (r *CloudflareReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dnsv1alpha1.DnsRecord{}).
		Complete(r)
}
