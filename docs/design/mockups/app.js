// Routes, the screen index, the component gallery and the app itself.
(function () {
  const P = T.pages;

  // Same paths as IMPLEMENTATION-PLAN §6.4.
  const ROUTES = [
    [/^\/welcome$/, P.Welcome],
    [/^\/invitations\/[^/]+$/, P.Invitation],
    [/^\/orgs\/[^/]+$/, P.Overview],
    [/^\/orgs\/[^/]+\/workspaces$/, P.Workspaces],
    [/^\/orgs\/[^/]+\/workspaces\/new$/, P.Wizard],
    [/^\/orgs\/[^/]+\/workspaces\/[^/]+$/, P.Workspace],
    [/^\/orgs\/[^/]+\/members$/, P.Members],
    [/^\/orgs\/[^/]+\/groups$/, P.Groups],
    [/^\/orgs\/[^/]+\/clusters$/, P.Clusters],
    [/^\/orgs\/[^/]+\/audit$/, P.Audit],
    [/^\/orgs\/[^/]+\/settings$/, P.Settings],
    [/^\/admin\/organizations$/, P.AdminOrgs],
    [/^\/admin\/organizations\/[^/]+\/review$/, P.AdminReview],
    [/^\/admin\/clusters$/, P.AdminClusters],
    [/^\/admin\/onboarding-policy$/, P.AdminPolicy],
    [/^\/admin\/grants$/, P.AdminGrants],
    [/^\/admin\/grants\/[^/]+\/[^/]+$/, P.AdminGrant],
    [/^\/admin\/operations$/, P.AdminOperations],
    [/^\/admin\/health$/, P.AdminHealth],
  ];

  const INDEX = [
    ['Before an Organization', [
      ['Request an Organization', '/welcome'],
      ['Request: slug already taken', '/welcome?state=taken'],
      ['Request waiting for review', '/welcome?state=pending'],
      ['Request rejected', '/welcome?state=rejected'],
      ['Accept an invitation', '/invitations/t'],
      ['Invitation: wrong account', '/invitations/t?state=mismatch'],
      ['Invitation: expired link', '/invitations/t?state=expired'],
    ]],
    ['Organization', [
      ['Overview: just approved', '/orgs/acme?state=fresh'],
      ['Overview', '/orgs/acme'],
      ['Overview: quota used up', '/orgs/acme?state=full'],
      ['Overview: cluster in maintenance', '/orgs/acme?state=maintenance'],
      ['Workspaces', '/orgs/acme/workspaces'],
      ['Workspaces: none yet', '/orgs/acme/workspaces?state=empty'],
      ['Workspaces: as a member', '/orgs/acme/workspaces?as=member'],
      ['Members and invitations', '/orgs/acme/members'],
      ['Members: invite dialog', '/orgs/acme/members?dialog=invite'],
      ['Members: invitation link', '/orgs/acme/members?dialog=link'],
      ['Groups', '/orgs/acme/groups'],
      ['Groups: create dialog', '/orgs/acme/groups?dialog=create'],
      ['Clusters', '/orgs/acme/clusters'],
      ['Audit', '/orgs/acme/audit'],
      ['Settings: owner', '/orgs/acme/settings'],
      ['Settings: owner, transfer waiting', '/orgs/acme/settings?transfer=pending'],
      ['Settings: admin', '/orgs/acme/settings?as=admin'],
      ['Settings: admin, accept ownership', '/orgs/acme/settings?as=admin&transfer=pending'],
      ['Settings: member', '/orgs/acme/settings?as=member'],
      ['Overview: ownership offered to you', '/orgs/acme?as=admin&transfer=pending'],
    ]],
    ['Workspace', [
      ['Wizard 1: name', '/orgs/acme/workspaces/new'],
      ['Wizard 2: cluster and compute', '/orgs/acme/workspaces/new?step=2'],
      ['Wizard 2: dedicated node', '/orgs/acme/workspaces/new?step=2&mode=dedicated'],
      ['Wizard 3: resources', '/orgs/acme/workspaces/new?step=3'],
      ['Wizard 3: over quota', '/orgs/acme/workspaces/new?step=3&over=1'],
      ['Wizard 4: network', '/orgs/acme/workspaces/new?step=4'],
      ['Wizard 4: network, advanced', '/orgs/acme/workspaces/new?step=4&advanced=1'],
      ['Wizard 5: namespaces', '/orgs/acme/workspaces/new?step=5'],
      ['Wizard 6: access', '/orgs/acme/workspaces/new?step=6'],
      ['Wizard 7: review', '/orgs/acme/workspaces/new?step=7'],
      ['Overview: Ready', '/orgs/acme/workspaces/solar'],
      ['Overview: Provisioning', '/orgs/acme/workspaces/orbit?state=provisioning'],
      ['Overview: Failed', '/orgs/acme/workspaces/orbit?state=failed'],
      ['Overview: Degraded', '/orgs/acme/workspaces/lunar?state=degraded'],
      ['Overview: Cordoned', '/orgs/acme/workspaces/solar?state=cordoned'],
      ['Overview: delete blocked', '/orgs/acme/workspaces/solar?state=blocked'],
      ['Resources', '/orgs/acme/workspaces/solar?tab=resources'],
      ['Compute: shared', '/orgs/acme/workspaces/solar?tab=compute'],
      ['Compute: dedicated', '/orgs/acme/workspaces/lunar?tab=compute'],
      ['Network', '/orgs/acme/workspaces/solar?tab=network'],
      ['Namespaces', '/orgs/acme/workspaces/solar?tab=namespaces'],
      ['Namespaces: as Developer', '/orgs/acme/workspaces/solar?tab=namespaces&as=developer'],
      ['Namespaces: create dialog', '/orgs/acme/workspaces/solar?tab=namespaces&dialog=namespace'],
      ['Routes', '/orgs/acme/workspaces/solar?tab=routes'],
      ['Routes: create dialog', '/orgs/acme/workspaces/solar?tab=routes&dialog=route'],
      ['Access roles', '/orgs/acme/workspaces/solar?tab=members'],
      ['Connect: kubectl and console', '/orgs/acme/workspaces/solar?tab=access'],
      ['Danger zone', '/orgs/acme/workspaces/solar?tab=danger'],
    ]],
    ['Platform Admin', [
      ['Organizations: waiting for review', '/admin/organizations'],
      ['Organizations: nothing to review', '/admin/organizations?state=empty'],
      ['Organizations: active', '/admin/organizations?tab=active'],
      ['Organizations: rejected', '/admin/organizations?tab=rejected'],
      ['Organizations: create dialog', '/admin/organizations?dialog=create'],
      ['Review a request', '/admin/organizations/globex/review'],
      ['Review: rejecting', '/admin/organizations/globex/review?reject=1'],
      ['Clusters, pools and nodes', '/admin/clusters'],
      ['Clusters: give a node', '/admin/clusters?dialog=give'],
      ['Clusters: node not clean', '/admin/clusters?dialog=dirty'],
      ['Clusters: nodes of a shared pool', '/admin/clusters?dialog=pool'],
      ['Onboarding policy', '/admin/onboarding-policy'],
      ['Grants', '/admin/grants'],
      ['Grants: none yet', '/admin/grants?state=empty'],
      ['Edit a grant', '/admin/grants/acme/cluster-a'],
      ['Edit a grant: review changes', '/admin/grants/acme/cluster-a?dialog=save'],
      ['Operations', '/admin/operations'],
      ['Operations: force dialog', '/admin/operations?dialog=force'],
      ['Platform health', '/admin/health'],
    ]],
    ['Building blocks', [['Component gallery', '/components']]],
  ];
  T.INDEX = INDEX;

  // /  — list of every screen
  const Home = {
    data: () => ({ index: INDEX }),
    template: `
      <div class="t-bare"><div class="t-page" style="max-width: 980px">
        <page-header title="Tenantry portal mockups" description="Every screen of the portal, built from Vue 3 and Element Plus with sample data. Nothing is saved and no API is called; buttons that move between screens work, the others do nothing."></page-header>
        <div class="t-cols" style="grid-template-columns: repeat(auto-fit, minmax(280px, 1fr))">
          <panel v-for="g in index" :key="g[0]" :title="g[0]" :note="g[1].length + ''" flush>
            <div class="m-links" style="padding: 8px 12px"><a v-for="l in g[1]" :key="l[1]" :href="'#' + l[1]">{{ l[0] }}</a></div>
          </panel>
        </div>
      </div></div>`,
  };

  // /components — every building block with its variants
  const Gallery = {
    data: () => ({ conds: T.conditionsFailed.slice(0, 4), states: ['Ready', 'Provisioning', 'Updating', 'Degraded', 'Cordoned', 'Failed', 'Deleting'], ops: ['Pending', 'Running', 'Succeeded', 'Failed', 'Blocked'], other: ['Active', 'Rejected', 'Synced', 'NotReady', 'Maintenance', 'Available', 'PartiallyAllocated', 'FullyAllocated', 'Overcommitted'] }),
    template: `
      <div class="t-bare"><div class="t-page" style="max-width: 1040px">
        <page-header title="Component gallery" :crumbs="[{ label: 'All screens', to: '#/' }, { label: 'Components' }]" description="The shared building blocks. Screens are assembled from these and from Element Plus controls (buttons, inputs, tables, dialogs), which keep their default look."></page-header>
        <section class="t-panel"><div class="t-panel-body" style="padding-top: 0; padding-bottom: 0">
          <div class="m-spec"><div><h3>StatusTag</h3><p class="muted small">One vocabulary for every state. Colour and a dot; busy states spin.</p></div>
            <div class="t-stack"><div class="m-spec-demo"><status-tag v-for="s in states" :key="s" :value="s" /></div><div class="m-spec-demo"><status-tag v-for="s in ops" :key="s" :value="s" /></div><div class="m-spec-demo"><status-tag v-for="s in other" :key="s" :value="s" /></div></div></div>
          <div class="m-spec"><div><h3>QuotaMeter</h3><p class="muted small">Used of total, what is left, and a bar. Turns amber at 85% and red when full. The striped part is what a pending change adds.</p></div>
            <div class="t-meters" style="flex: 1"><quota-meter label="CPU requests" :used="0" :total="2" unit="cores" /><quota-meter label="Memory requests" :used="9" :total="16" unit="Gi" /><quota-meter label="Pods" :used="70" :total="80" /><quota-meter label="Workspaces" :used="4" :total="4" /><quota-meter label="CPU after this change" :used="7" :pending="1" :total="8" unit="cores" /></div></div>
          <div class="m-spec"><div><h3>PageHeader</h3><p class="muted small">Where you are, what it is, its state, the main action.</p></div>
            <div style="flex: 1"><page-header title="Solar" :crumbs="[{ label: 'Workspaces', to: '#/components' }, { label: 'Solar' }]"><template #tags><code>acme-solar</code><status-tag value="Ready" /></template><template #actions><el-button>Kubeconfig</el-button><el-button type="primary">Open web console</el-button></template></page-header></div></div>
          <div class="m-spec"><div><h3>Panel</h3><p class="muted small">A titled block. Use one per topic; do not nest panels.</p></div>
            <div style="flex: 1"><panel title="Pod network" note="fixed after creation" foot="Footer for a caveat or a count."><template #extra><el-button size="small">Action</el-button></template><key-values :items="[['Address block', '10.240.0.0/24'], ['Pool', 'onboarding-pod-network']]"></key-values></panel></div></div>
          <div class="m-spec"><div><h3>KeyValues</h3><p class="muted small">Facts about one object.</p></div>
            <div style="flex: 1"><key-values :items="[['Cluster', 'Cluster A'], ['Compute', 'Shared on shared-general'], ['Created', '18 Nov 2026 by alice@example.com']]"></key-values></div></div>
          <div class="m-spec"><div><h3>ConditionTimeline</h3><p class="muted small">Progress by Kubernetes condition, so "where is it stuck" has an exact answer.</p></div>
            <div style="flex: 1"><condition-timeline :conditions="conds" /></div></div>
          <div class="m-spec"><div><h3>EmptyState</h3><p class="muted small">What will be here, and the next step.</p></div>
            <div style="flex: 1; border: 1px dashed var(--t-line); border-radius: 8px"><empty-state title="No Workspaces yet" description="A Workspace is an isolated part of the cluster with its own quota, network and members."><template #actions><el-button type="primary">Create your first Workspace</el-button></template></empty-state></div></div>
          <div class="m-spec"><div><h3>ErrorState</h3><p class="muted small">A load that failed: plain words, the stable code, the request id, retry.</p></div>
            <div style="flex: 1; border: 1px dashed var(--t-line); border-radius: 8px"><error-state message="The platform did not answer. Your data is safe; this is usually over in a moment." code="INTERNAL" request-id="01JD6T9A" /></div></div>
          <div class="m-spec"><div><h3>ConfirmByName</h3><p class="muted small">Destructive actions unlock after typing the name.</p></div>
            <div style="flex: 1"><confirm-by-name name="acme-solar" action="Delete Workspace" /></div></div>
          <div class="m-spec"><div><h3>NumField</h3><p class="muted small">A number with its unit inside the box.</p></div>
            <div class="m-spec-demo"><div style="width: 180px"><num-field :model-value="4" unit="cores" label="CPU" /></div><div style="width: 180px"><num-field :model-value="20" label="Pods" /></div></div></div>
          <div class="m-spec"><div><h3>ResourceGrid</h3><p class="muted small">CPU and memory: requests beside limits, with a note or an error under each.</p></div>
            <div style="flex: 1"><resource-grid idp="demo" :value="{ rcpu: 1, lcpu: 2, rmem: 2, lmem: 4 }" :notes="{ rcpu: '1 left in the grant', rmem: '2 left in the grant' }" :errors="{ lmem: 'Only 3 Gi left.' }" /></div></div>
          <div class="m-spec"><div><h3>CopyField</h3><p class="muted small">A value people paste elsewhere.</p></div>
            <div style="flex: 1; max-width: 520px"><copy-field value="https://web-acme-solar.mvp.tenantry.site" /></div></div>
          <div class="m-spec"><div><h3>Field error</h3><p class="muted small">An error that belongs to a field sits under it, with the code.</p></div>
            <div style="flex: 1; max-width: 420px"><el-form label-position="top"><el-form-item label="Slug" for="g-slug" error="This slug is already taken. Choose another one. (SLUG_TAKEN)"><el-input id="g-slug" model-value="acme" /></el-form-item></el-form></div></div>
          <div class="m-spec"><div><h3>Disabled with a reason</h3><p class="muted small">A locked control always says why, on hover and focus.</p></div>
            <div class="m-spec-demo"><el-tooltip content="This Organization has used all of its Workspaces on Cluster A."><span><el-button type="primary" disabled>Create Workspace</el-button></span></el-tooltip></div></div>
        </div></section>
      </div></div>`,
  };

  const NotFound = { template: `<div class="t-bare"><div class="t-page narrow"><panel><empty-state title="No such screen" description="This address is not part of the mockup."><template #actions><el-button type="primary" @click="$root.go('/')">All screens</el-button></template></empty-state></panel></div></div>` };

  const app = Vue.createApp({
    data: () => ({ r: T.route, me: T.me, drawer: false, index: INDEX }),
    computed: {
      view() {
        if (this.r.path === '/') return Home;
        if (this.r.path === '/components') return Gallery;
        const hit = ROUTES.find((x) => x[0].test(this.r.path));
        return hit ? hit[1] : NotFound;
      },
      here() { const q = new URLSearchParams(this.r.query).toString(); return this.r.path + (q ? '?' + q : ''); },
    },
    watch: { here() { this.drawer = false; } },
    methods: { go: T.go },
    template: `
      <component :is="view" :key="here" />
      <button type="button" class="m-fab" @click="drawer = true"><el-icon><Menu /></el-icon>Mockup screens</button>
      <el-drawer v-model="drawer" title="Mockup screens" size="340px">
        <p class="muted small">This panel is not part of the product.</p>
        <div class="m-links"><a href="#/" :aria-current="here === '/' ? 'page' : null">All screens on one page</a></div>
        <template v-for="g in index" :key="g[0]">
          <div class="m-group">{{ g[0] }}</div>
          <div class="m-links"><a v-for="l in g[1]" :key="l[1]" :href="'#' + l[1]" :aria-current="here === l[1] ? 'page' : null">{{ l[0] }}</a></div>
        </template>
      </el-drawer>`,
  });

  app.use(ElementPlus);
  for (const [name, comp] of Object.entries(ElementPlusIconsVue)) app.component(name, comp);
  for (const [name, comp] of Object.entries(T.components)) app.component(name, comp);
  app.config.errorHandler = (err) => { console.error('MOCKUP ERROR', err); };
  app.mount('#app');
})();
