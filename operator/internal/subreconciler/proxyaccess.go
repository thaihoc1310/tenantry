package subreconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
	"github.com/thaihoc1310/tenantry/operator/internal/metrics"
	"github.com/thaihoc1310/tenantry/operator/internal/render"
)

// Reasons of ProxyAccessReady.
const (
	// ReasonNoNamespace: the Workspace has no Namespace, nothing to show.
	ReasonNoNamespace = "NoNamespace"
	// ReasonProxyUnreachable: Capsule Proxy did not answer the probe.
	ReasonProxyUnreachable = "ProxyUnreachable"
	// ReasonProxyNotFiltering: the probe saw a Namespace that belongs to no
	// Workspace: a system one, or another Tenant's.
	ReasonProxyNotFiltering = "ProxyNotFiltering"
)

// Probe intervals: a passing check is repeated at most every probeEvery,
// a failing one sooner, never in a tight loop.
const (
	probeEvery      = 5 * time.Minute
	probeRetry      = 30 * time.Second
	tokenSeconds    = 600
	tokenRenewAhead = 2 * time.Minute
)

// ProxyProber runs the synthetic verification of sub-reconciler proxyaccess
// (PRODUCT-PLAN §17.1 step 14): it lists Namespaces through Capsule Proxy as
// the probe ServiceAccount, a subject of every Workspace's ProxySetting
// next to its Developer and Viewer groups, so it sees what they see. The
// probe has no RBAC right at all; its token comes from TokenRequest, lives
// in memory only and is reused until shortly before it expires.
type ProxyProber struct {
	Client client.Client
	// URL of Capsule Proxy inside the cluster.
	URL string
	// Namespace and Name of the probe ServiceAccount.
	Namespace, Name string
	HTTP            *http.Client

	mu       sync.Mutex
	token    string
	tokenEnd time.Time
	last     map[string]probeMemo
}

type probeMemo struct {
	key    string
	at     time.Time
	result Result
}

// Username of the probe as Kubernetes and Capsule Proxy see it.
func (p *ProxyProber) Username() string {
	return "system:serviceaccount:" + p.Namespace + ":" + p.Name
}

// ProxyAccess applies the Workspace's ProxySetting and reports
// ProxyAccessReady from the probe; retry asks for another pass. namespaces
// are the Tenant's live Namespaces; cordoned, Capsule refuses the
// operator's writes in them, so the ProxySetting is left as it is.
func (p *ProxyProber) ProxyAccess(ctx context.Context, scheme *runtime.Scheme, ws *v1.PlatformWorkspace,
	namespaces []corev1.Namespace, cordoned bool) (res Result, retry time.Duration) {
	const t = v1.ConditionProxyAccessReady
	var names []string
	for _, ns := range namespaces {
		if ns.DeletionTimestamp == nil {
			names = append(names, ns.Name)
		}
	}
	slices.Sort(names)
	if len(names) == 0 {
		r := ok(t)
		r.Condition.Reason, r.Condition.Message = ReasonNoNamespace, "the Workspace has no Namespace yet"
		return r, 0
	}
	if !cordoned {
		if err := p.applySetting(ctx, scheme, ws, names[0]); err != nil {
			return failure(t, err), 0
		}
	}

	key := fmt.Sprintf("%d/%s", ws.Generation, strings.Join(names, ","))
	p.mu.Lock()
	memo, seen := p.last[ws.Name]
	p.mu.Unlock()
	if seen && memo.key == key {
		wait := probeEvery
		if memo.result.Condition.Status != metav1.ConditionTrue {
			wait = probeRetry
		}
		if left := wait - time.Since(memo.at); left > 0 {
			return memo.result, left
		}
	}
	res = p.probe(ctx, ws, names)
	metrics.ProxyProbes.WithLabelValues(res.Condition.Reason).Inc()
	p.mu.Lock()
	if p.last == nil {
		p.last = map[string]probeMemo{}
	}
	p.last[ws.Name] = probeMemo{key: key, at: time.Now(), result: res}
	p.mu.Unlock()
	if res.Condition.Status != metav1.ConditionTrue {
		return res, probeRetry
	}
	return res, probeEvery
}

// Forget drops what is remembered of a deleted Workspace.
func (p *ProxyProber) Forget(name string) {
	p.mu.Lock()
	delete(p.last, name)
	p.mu.Unlock()
}

// applySetting puts the ProxySetting in the first Namespace of the Tenant
// and removes it from the others: Capsule Proxy shows a subject every
// Namespace of the Tenant holding a ProxySetting that names it.
func (p *ProxyProber) applySetting(ctx context.Context, scheme *runtime.Scheme, ws *v1.PlatformWorkspace, namespace string) error {
	setting := render.ProxySetting(ws, namespace, p.Username())
	if err := controllerutil.SetControllerReference(ws, setting, scheme); err != nil {
		return err
	}
	if err := Apply(ctx, p.Client, setting); err != nil {
		return err
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(render.ProxySettingGVK.GroupVersion().WithKind(render.ProxySettingGVK.Kind + "List"))
	if err := p.Client.List(ctx, list, client.MatchingLabels{v1.LabelManaged: "true", v1.LabelWorkspaceID: ws.Spec.WorkspaceID}); err != nil {
		return err
	}
	for i := range list.Items {
		if s := &list.Items[i]; s.GetNamespace() != namespace && s.GetDeletionTimestamp() == nil {
			if err := p.Client.Delete(ctx, s); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

// probe lists Namespaces through the proxy: every Namespace of the
// Workspace must be there, and nothing outside the Workspaces. The probe
// is a subject of every Workspace's ProxySetting, so it rightly sees them
// all; that a Developer of one does not see another's follows from the
// ProxySetting naming only that Workspace's groups.
func (p *ProxyProber) probe(ctx context.Context, ws *v1.PlatformWorkspace, want []string) Result {
	const t = v1.ConditionProxyAccessReady
	token, err := p.probeToken(ctx)
	if err != nil {
		return failure(t, fmt.Errorf("token of the probe %s: %w", p.Username(), err))
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(p.URL, "/")+"/api/v1/namespaces", nil)
	if err != nil {
		return failed(t, ReasonProxyUnreachable, err.Error())
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return failed(t, ReasonProxyUnreachable, "Capsule Proxy: "+err.Error())
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return failed(t, ReasonProxyUnreachable, fmt.Sprintf("Capsule Proxy answered %s to the probe", resp.Status))
	}
	var list corev1.NamespaceList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return failed(t, ReasonProxyUnreachable, "Capsule Proxy: "+err.Error())
	}
	var all v1.PlatformWorkspaceList
	if err := p.Client.List(ctx, &all); err != nil {
		return failure(t, err)
	}
	workspaces := map[string]bool{}
	for _, w := range all.Items {
		workspaces[w.Name] = true
	}
	seen := map[string]bool{}
	var foreign []string
	for _, ns := range list.Items {
		seen[ns.Name] = true
		if !workspaces[ns.Labels[render.LabelCapsuleTenant]] {
			foreign = append(foreign, ns.Name)
		}
	}
	if len(foreign) > 0 {
		slices.Sort(foreign)
		return failed(t, ReasonProxyNotFiltering, "the probe sees Namespaces outside the Workspaces: "+strings.Join(foreign, ", "))
	}
	var missing []string
	for _, name := range want {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return waiting(t, "waiting for Capsule Proxy to show "+strings.Join(missing, ", "))
	}
	return ok(t)
}

func (p *ProxyProber) probeToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && time.Until(p.tokenEnd) > tokenRenewAhead {
		return p.token, nil
	}
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: p.Namespace, Name: p.Name}}
	seconds := int64(tokenSeconds)
	tr := &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &seconds}}
	if err := p.Client.SubResource("token").Create(ctx, sa, tr); err != nil {
		return "", err
	}
	p.token, p.tokenEnd = tr.Status.Token, tr.Status.ExpirationTimestamp.Time
	return p.token, nil
}
