package reservation

import (
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
)

func TestGrantFitsAndNotBelow(t *testing.T) {
	g := GrantSpec{ClusterID: "cluster-a", MaxWorkspaces: 2, MaxNamespacesTotal: 6, Resources: Resources{
		RequestsCPUMilli: 2000, RequestsMemoryBytes: 4 << 30, LimitsCPUMilli: 4000, LimitsMemoryBytes: 8 << 30, Pods: 20, RequestsStorageBytes: 20 << 30,
	}}
	ws := Resources{RequestsCPUMilli: 1500, RequestsMemoryBytes: 1 << 30, LimitsCPUMilli: 1500, LimitsMemoryBytes: 1 << 30, Pods: 5, RequestsStorageBytes: 5 << 30}
	holds := Share(true, 2, ws)
	if err := GrantFits(g, Allocation{}, holds); err != nil {
		t.Fatalf("first fits: %v", err)
	}
	err := GrantFits(g, holds, Share(true, 2, ws))
	e, ok := apperr.As(err)
	if !ok || e.Code != apperr.ClusterGrantQuotaExceeded || e.Details["resource"] != "requestsCpu" ||
		e.Details["limit"] != "2" || e.Details["allocated"] != "1500m" || e.Details["requested"] != "1500m" {
		t.Fatalf("second Shared: %v %+v", err, e)
	}
	// A Dedicated Workspace takes no CPU of the grant, but a Workspace and storage.
	if err := GrantFits(g, holds, Share(false, 2, ws)); err != nil {
		t.Fatalf("Dedicated: %v", err)
	}
	if e, _ := apperr.As(GrantFits(g, Allocation{Workspaces: 2}, Share(false, 1, ws))); e == nil || e.Details["resource"] != "workspaces" {
		t.Fatalf("a third Workspace: %+v", e)
	}

	low := g
	low.Resources.RequestsCPUMilli = 1000
	if e, _ := apperr.As(NotBelow(low, holds)); e == nil || e.Code != apperr.ClusterGrantBelowAllocation || e.Details["minimum"] != "1500m" {
		t.Fatalf("below: %+v", e)
	}
	if err := NotBelow(g, holds); err != nil {
		t.Fatalf("at or above: %v", err)
	}
}
