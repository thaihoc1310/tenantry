package httpapi

import (
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
)

// Kubernetes quantities exist only at the API's edge: inside, CPU is stored
// in millicores and memory and storage in bytes (IMPLEMENTATION-PLAN §3.7).

// Upper bounds that keep every value far inside int64.
var (
	maxCPU   = resource.MustParse("1M")  // a million cores
	maxBytes = resource.MustParse("1Ei") // an exbibyte
)

func parseQuantity(field string, v oapi.Quantity, upTo resource.Quantity) (resource.Quantity, error) {
	q, err := resource.ParseQuantity(string(v))
	if err != nil {
		return q, apperr.Invalid(field, "is not a Kubernetes quantity (e.g. 500m, 2, 4Gi)")
	}
	if q.Sign() < 0 {
		return q, apperr.Invalid(field, "must not be negative")
	}
	if q.Cmp(upTo) > 0 {
		return q, apperr.Invalid(field, "is too large")
	}
	return q, nil
}

// parseCPU returns millicores, rounding a fraction of one up.
func parseCPU(field string, v oapi.Quantity) (int64, error) {
	q, err := parseQuantity(field, v, maxCPU)
	return q.MilliValue(), err
}

// parseBytes returns bytes, rounding a fraction of one up.
func parseBytes(field string, v oapi.Quantity) (int64, error) {
	q, err := parseQuantity(field, v, maxBytes)
	return q.Value(), err
}

func formatCPU(milli int64) oapi.Quantity {
	return oapi.Quantity(resource.NewMilliQuantity(milli, resource.DecimalSI).String())
}

func formatBytes(b int64) oapi.Quantity {
	return oapi.Quantity(resource.NewQuantity(b, resource.BinarySI).String())
}

func toGrantSpec(in oapi.ClusterGrantInput) (reservation.GrantSpec, error) {
	g := reservation.GrantSpec{
		ClusterID:             in.ClusterId,
		MaxWorkspaces:         int32(in.MaxWorkspaces),
		MaxNamespacesTotal:    int32(in.MaxNamespacesTotal),
		SharedPoolIDs:         poolIDs(in.SharedPoolIds),
		NetworkPoolIDs:        poolIDs(in.NetworkPoolIds),
		AllowedStorageClasses: in.AllowedStorageClasses,
	}
	r, res := in.Resources, &g.Resources
	res.Pods = int32(r.Pods)
	var err error
	for _, p := range []struct {
		field string
		v     oapi.Quantity
		out   *int64
		parse func(string, oapi.Quantity) (int64, error)
	}{
		{"requestsCpu", r.RequestsCpu, &res.RequestsCPUMilli, parseCPU},
		{"limitsCpu", r.LimitsCpu, &res.LimitsCPUMilli, parseCPU},
		{"requestsMemory", r.RequestsMemory, &res.RequestsMemoryBytes, parseBytes},
		{"limitsMemory", r.LimitsMemory, &res.LimitsMemoryBytes, parseBytes},
		{"requestsStorage", r.RequestsStorage, &res.RequestsStorageBytes, parseBytes},
	} {
		if *p.out, err = p.parse(p.field, p.v); err != nil {
			return g, err
		}
	}
	return g, nil
}

func poolIDs(ids []oapi.PoolId) []string {
	out := make([]string, len(ids))
	for i, v := range ids {
		out[i] = string(v)
	}
	return out
}

func toClusterGrant(g reservation.Grant) oapi.ClusterGrant {
	r := g.Resources
	pools := func(ids []string) []string {
		if ids == nil {
			return []string{}
		}
		return ids
	}
	return oapi.ClusterGrant{
		Id: g.ID, IsDefault: g.IsDefault, ClusterId: g.ClusterID,
		MaxWorkspaces: int(g.MaxWorkspaces), MaxNamespacesTotal: int(g.MaxNamespacesTotal),
		Resources: oapi.GrantResources{
			RequestsCpu: formatCPU(r.RequestsCPUMilli), LimitsCpu: formatCPU(r.LimitsCPUMilli),
			RequestsMemory: formatBytes(r.RequestsMemoryBytes), LimitsMemory: formatBytes(r.LimitsMemoryBytes),
			Pods: int(r.Pods), RequestsStorage: formatBytes(r.RequestsStorageBytes),
		},
		SharedPoolIds: pools(g.SharedPoolIDs), NetworkPoolIds: pools(g.NetworkPoolIDs),
		AllowedStorageClasses: pools(g.AllowedStorageClasses),
	}
}

func toEntitlement(e oapi.Entitlement) org.Entitlement {
	return org.Entitlement{MaxMembers: int32(e.MaxMembers), MaxGroups: int32(e.MaxGroups), MaxWorkspacesTotal: int32(e.MaxWorkspacesTotal)}
}

func fromEntitlement(e org.Entitlement) oapi.Entitlement {
	return oapi.Entitlement{MaxMembers: int(e.MaxMembers), MaxGroups: int(e.MaxGroups), MaxWorkspacesTotal: int(e.MaxWorkspacesTotal)}
}
