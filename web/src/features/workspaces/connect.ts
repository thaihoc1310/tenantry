// Reaching a Workspace's cluster from the portal (PRODUCT-PLAN §11, §12).
import { api, unwrap } from '@/api/client'
import { showFailure } from '@/app/failure'

// The kubeconfig is a plain GET with the session cookie: the browser saves
// it under the name the API gives (Content-Disposition).
export const kubeconfigURL = (wsId: string) => `/api/v1/workspaces/${encodeURIComponent(wsId)}/kubeconfig`

// openConsole opens the cluster's Headlamp in a new tab. The tab is opened
// at once, inside the click, so a popup blocker lets it through.
export async function openConsole(wsId: string): Promise<void> {
  const tab = window.open('', '_blank')
  try {
    const { url } = await unwrap(
      api.GET('/workspaces/{workspaceId}/console-link', { params: { path: { workspaceId: wsId } } }),
    )
    if (tab) {
      tab.opener = null
      tab.location.href = url
    } else window.location.assign(url)
  } catch (err) {
    tab?.close()
    showFailure(err)
  }
}
