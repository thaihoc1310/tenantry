package httpapi

import (
	"bytes"
	"context"
	"net/url"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/workspace"
)

// kubectlClient is the Keycloak client kubelogin uses: public, PKCE, a
// loopback redirect (deploy/keycloak/realm-tenantry.json).
const kubectlClient = "kubectl"

// kubeconfig is a kubeconfig with no credential in it (PRODUCT-PLAN §11.2):
// the server is the cluster's Capsule Proxy, behind a publicly trusted
// certificate, so it carries no CA data; kubectl asks kubelogin for the
// user's token.
func kubeconfig(c workspace.Connection, issuer string) ([]byte, error) {
	name := c.Workspace.K8sName
	user := "tenantry-" + c.Cluster.ID
	context := map[string]any{"cluster": c.Cluster.ID, "user": user}
	if c.Namespace != "" {
		context["namespace"] = c.Namespace
	}
	return yaml.Marshal(map[string]any{
		"apiVersion":      "v1",
		"kind":            "Config",
		"clusters":        []any{map[string]any{"name": c.Cluster.ID, "cluster": map[string]any{"server": c.Cluster.ProxyEndpoint}}},
		"contexts":        []any{map[string]any{"name": name, "context": context}},
		"current-context": name,
		"users": []any{map[string]any{"name": user, "user": map[string]any{"exec": map[string]any{
			"apiVersion": "client.authentication.k8s.io/v1",
			"command":    "kubectl",
			"args": []string{
				"oidc-login", "get-token",
				"--oidc-issuer-url=" + issuer,
				"--oidc-client-id=" + kubectlClient,
			},
			"interactiveMode":    "IfAvailable",
			"provideClusterInfo": false,
		}}}},
	})
}

func (s server) GetWorkspaceKubeconfig(ctx context.Context, req oapi.GetWorkspaceKubeconfigRequestObject) (oapi.GetWorkspaceKubeconfigResponseObject, error) {
	c, err := s.workspaces.Kubeconfig(ctx, userID(ctx), req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	b, err := kubeconfig(c, s.issuer)
	if err != nil {
		return nil, err
	}
	return oapi.GetWorkspaceKubeconfig200ApplicationyamlResponse{
		Body:          bytes.NewReader(b),
		ContentLength: int64(len(b)),
		Headers: oapi.GetWorkspaceKubeconfig200ResponseHeaders{
			ContentDisposition: `attachment; filename="` + c.Workspace.K8sName + `.kubeconfig"`,
		},
	}, nil
}

// consoleURL opens Headlamp (in-cluster, its cluster is "main") in the
// Workspace's first Namespace, or on its home page.
func consoleURL(c workspace.Connection) string {
	base := strings.TrimSuffix(c.Cluster.ConsoleUrl, "/")
	if c.Namespace == "" {
		return base
	}
	return base + "/c/main/namespaces/" + url.PathEscape(c.Namespace)
}

func (s server) GetWorkspaceConsoleLink(ctx context.Context, req oapi.GetWorkspaceConsoleLinkRequestObject) (oapi.GetWorkspaceConsoleLinkResponseObject, error) {
	c, err := s.workspaces.Console(ctx, userID(ctx), req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return oapi.GetWorkspaceConsoleLink200JSONResponse{Url: consoleURL(c)}, nil
}
