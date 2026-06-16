package controller

import (
	"context"
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

// PagesProjectReconciler manages a Cloudflare Pages site and wires its custom
// domain. Domain DNS is delegated to a child DnsRecord (composition over a
// second CF-DNS code path), which the CloudflareReconciler syncs to the edge.
type PagesProjectReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	CF     *cloudflare.Client
}

// +kubebuilder:rbac:groups=dns.hanzo.ai,resources=pagesprojects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dns.hanzo.ai,resources=pagesprojects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dns.hanzo.ai,resources=dnsrecords,verbs=get;list;watch;create;update;patch;delete

func (r *PagesProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var pp dnsv1alpha1.PagesProject
	if err := r.Get(ctx, req.NamespacedName, &pp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !pp.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil // child DnsRecord + CF cleanup handled by ownerRef GC + its finalizer
	}
	if r.CF == nil {
		logger.Info("pages sync disabled: no API token configured")
		return ctrl.Result{}, nil
	}

	name := pp.Spec.ProjectName
	if name == "" {
		name = pp.Name
	}

	// 1. Ensure the Pages project exists; capture its pages.dev subdomain.
	proj, err := r.CF.GetPagesProject(ctx, pp.Spec.AccountID, name)
	if err != nil {
		return r.fail(ctx, &pp, err)
	}
	if proj == nil {
		if proj, err = r.CF.CreatePagesProject(ctx, pp.Spec.AccountID,
			cloudflare.PagesProject{Name: name, ProductionBranch: pp.Spec.ProductionBranch}); err != nil {
			return r.fail(ctx, &pp, err)
		}
	}

	// 2. Attach the custom domain and emit its CNAME (delegated to the DNS layer).
	if pp.Spec.CustomDomain != "" {
		if err := r.CF.AddPagesDomain(ctx, pp.Spec.AccountID, name, pp.Spec.CustomDomain); err != nil {
			return r.fail(ctx, &pp, err)
		}
		if pp.Spec.ZoneRef != "" {
			if err := r.ensureDomainRecord(ctx, &pp, proj.Subdomain); err != nil {
				return r.fail(ctx, &pp, err)
			}
		}
	}

	pp.Status.Phase = "Ready"
	pp.Status.Subdomain = proj.Subdomain
	if err := r.Status().Update(ctx, &pp); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
}

// ensureDomainRecord creates/updates the owned CNAME DnsRecord that points the
// custom domain at the project's pages.dev host.
func (r *PagesProjectReconciler) ensureDomainRecord(ctx context.Context, pp *dnsv1alpha1.PagesProject, subdomain string) error {
	rec := &dnsv1alpha1.DnsRecord{
		ObjectMeta: metav1.ObjectMeta{Name: pp.Name + "-domain", Namespace: pp.Namespace},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, rec, func() error {
		rec.Spec = dnsv1alpha1.DnsRecordSpec{
			ZoneRef:          pp.Spec.ZoneRef,
			Name:             pp.Spec.CustomDomain,
			Type:             "CNAME",
			Content:          subdomain,
			Proxied:          true,
			SyncToCloudflare: true,
		}
		return controllerutil.SetControllerReference(pp, rec, r.Scheme)
	})
	return err
}

func (r *PagesProjectReconciler) fail(ctx context.Context, pp *dnsv1alpha1.PagesProject, cause error) (ctrl.Result, error) {
	log.FromContext(ctx).Error(cause, "pages reconcile failed", "project", pp.Name)
	pp.Status.Phase = "Error"
	if err := r.Status().Update(ctx, pp); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *PagesProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dnsv1alpha1.PagesProject{}).
		Owns(&dnsv1alpha1.DnsRecord{}).
		Complete(r)
}
