-- Development and first-install data (cmd/tenantry-seed). Every insert keeps
-- an existing row as it is: running the seed again changes nothing, and
-- never undoes what an Admin changed since.

-- name: SeedCluster :execrows
INSERT INTO clusters (id, display_name, status, proxy_endpoint, console_url, apps_domain)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO NOTHING;

-- name: SeedSharedPool :execrows
INSERT INTO shared_compute_pools (id, cluster_id, name, allocation_overcommit_ratio)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO NOTHING;

-- name: SeedNetworkPool :execrows
INSERT INTO network_pools (id, cluster_id, cidrs, default_prefix_length, allowed_prefix_lengths)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO NOTHING;

-- name: SeedOnboardingPolicy :execrows
INSERT INTO onboarding_policies (id, default_cluster_id, entitlement_template, grant_template, active)
VALUES ($1, $2, $3, $4, true)
ON CONFLICT (id) DO NOTHING;
