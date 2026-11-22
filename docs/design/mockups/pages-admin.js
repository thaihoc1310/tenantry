// Platform Admin area: /admin/*
(function () {
  T.pages = T.pages || {};

  // The grant fields, shared by review, create and edit.
  const GrantForm = {
    props: { current: Object, idp: { type: String, default: 'g' } },
    data: () => ({ g: { ws: 2, ns: 6, rcpu: 2, rmem: 4, lcpu: 4, lmem: 8, pods: 20, storage: 20, pool: 'shared-general', net: 'onboarding-pod-network', sc: 'standard' }, used: { ws: 3, ns: 5, rcpu: 3, rmem: 6, lcpu: 5, lmem: 10, pods: 30, storage: 45 } }),
    created() { if (this.current) Object.assign(this.g, { ws: 4, ns: 12, rcpu: 4, rmem: 8, lcpu: 8, lmem: 16, pods: 60, storage: 100 }); },
    computed: {
      all() { return [['ws', 'Workspaces', ''], ['ns', 'Namespaces', ''], ['storage', 'Storage', 'Gi']]; },
      notes() { const n = {}; if (this.current) for (const k in this.used) n[k] = 'Reserved now: ' + this.used[k]; return n; },
      errors() { const e = {}; if (this.current) for (const k in this.used) if (this.g[k] < this.used[k]) e[k] = 'Workspaces already reserve ' + this.used[k] + '. (CLUSTER_GRANT_BELOW_ALLOCATION)'; return e; },
    },
    template: `
      <el-form label-position="top" class="t-stack" style="gap: 24px" @submit.prevent>
        <section class="t-section">
          <h3>Every Workspace, shared or dedicated</h3>
          <div class="t-fields">
            <el-form-item v-for="f in all" :key="f[0]" :label="f[1]" :for="idp + '-' + f[0]">
              <num-field :id="idp + '-' + f[0]" v-model="g[f[0]]" :unit="f[2]" />
              <div v-if="errors[f[0]]" class="t-field-note t-field-err">{{ errors[f[0]] }}</div>
              <div v-else-if="notes[f[0]]" class="t-field-note num">{{ notes[f[0]] }}</div>
            </el-form-item>
          </div>
        </section>
        <section class="t-section">
          <h3>Workspaces on a shared pool</h3>
          <div class="t-fields" style="grid-template-columns: minmax(0, 300px)">
            <el-form-item label="Shared pool" :for="idp + '-pool'">
              <el-select :id="idp + '-pool'" v-model="g.pool" clearable :disabled="!!current" placeholder="None: dedicated nodes only" style="width: 100%"><el-option value="shared-general" label="shared-general" /></el-select>
              <div v-if="current" class="t-field-note">2 Workspaces of Acme run on this pool, so it cannot be taken away.</div>
            </el-form-item>
          </div>
          <template v-if="g.pool">
            <p class="t-field-note">Shared quota: the total that this Organization's Workspaces may reserve on the pool.</p>
            <resource-grid :idp="idp" :value="g" :notes="notes" :errors="errors" />
            <div class="t-fields">
              <el-form-item label="Pods" :for="idp + '-pods'">
                <num-field :id="idp + '-pods'" v-model="g.pods" />
                <div v-if="errors.pods" class="t-field-note t-field-err">{{ errors.pods }}</div>
                <div v-else-if="notes.pods" class="t-field-note num">{{ notes.pods }}</div>
              </el-form-item>
            </div>
          </template>
        </section>
        <section class="t-section">
          <h3>Workspaces on dedicated nodes</h3>
          <p class="t-explain">No quota to set. A dedicated node belongs to the Organization, so its Workspaces are limited by the size of the node only.
            <template v-if="current"> Acme has 2 nodes: <code>wk3</code> <code>wk4</code> </template>
            <template v-else> This Organization can use dedicated nodes as soon as you give it one. </template>
            <a href="#/admin/clusters">Give or take back nodes</a>
          </p>
        </section>
        <section class="t-section">
          <h3>Network and storage</h3>
          <div class="t-fields" style="grid-template-columns: repeat(auto-fill, minmax(220px, 300px))">
            <el-form-item label="Pod network pool" :for="idp + '-net'"><el-select :id="idp + '-net'" v-model="g.net" style="width: 100%"><el-option value="onboarding-pod-network" label="onboarding-pod-network (shared)" /></el-select></el-form-item>
            <el-form-item label="Storage class" :for="idp + '-sc'"><el-select :id="idp + '-sc'" v-model="g.sc" style="width: 100%"><el-option value="standard" label="standard" /></el-select></el-form-item>
          </div>
        </section>
      </el-form>`,
  };

  // /admin/organizations?tab=pending|active|rejected  &state=empty
  T.pages.AdminOrgs = {
    components: { GrantForm },
    data: () => ({ r: T.route, requests: T.requests, orgs: T.adminOrgs, dialog: !!T.route.query.dialog, n: { name: '', slug: '', owner: '' } }),
    computed: {
      tab() { return this.r.query.tab || 'pending'; },
      pending() { return this.r.query.state === 'empty' ? [] : this.requests; },
    },
    template: `
      <app-shell area="admin" active="organizations">
        <page-header title="Organizations">
          <template #actions><el-button @click="dialog = true"><el-icon><Plus /></el-icon>&nbsp;Create Organization</el-button></template>
        </page-header>
        <nav class="t-tabs" aria-label="Organizations by status">
          <a href="#/admin/organizations" :aria-current="tab === 'pending' ? 'page' : null">Waiting for review ({{ pending.length }})</a>
          <a href="#/admin/organizations?tab=active" :aria-current="tab === 'active' ? 'page' : null">Active</a>
          <a href="#/admin/organizations?tab=rejected" :aria-current="tab === 'rejected' ? 'page' : null">Rejected</a>
        </nav>

        <panel v-if="tab === 'pending'" flush>
          <div v-if="pending.length" class="t-tablewrap">
            <el-table :data="pending" style="width: 100%">
              <el-table-column label="Organization" min-width="160"><template #default="{ row }"><strong>{{ row.name }}</strong><div class="mono muted">{{ row.slug }}</div></template></el-table-column>
              <el-table-column label="Requested by" min-width="180" prop="by" />
              <el-table-column label="Purpose" min-width="280" prop="purpose" />
              <el-table-column label="Waiting since" width="170"><template #default="{ row }"><span class="num">{{ row.at }}</span></template></el-table-column>
              <el-table-column width="110" align="right"><template #default="{ row }"><el-button type="primary" size="small" @click="$root.go('/admin/organizations/' + row.slug + '/review')">Review</el-button></template></el-table-column>
            </el-table>
          </div>
          <empty-state v-else center title="Nothing to review" description="New requests for an Organization appear here."></empty-state>
        </panel>

        <panel v-else-if="tab === 'active'" flush>
          <div class="t-tablewrap">
            <el-table :data="orgs" style="width: 100%">
              <el-table-column label="Organization" min-width="160"><template #default="{ row }"><strong>{{ row.name }}</strong><div class="mono muted">{{ row.slug }}</div></template></el-table-column>
              <el-table-column label="Owner" min-width="190" prop="owner" />
              <el-table-column label="Workspaces" width="130" align="right"><template #default="{ row }"><span class="num">{{ row.workspaces }}</span></template></el-table-column>
              <el-table-column label="CPU requests" width="140" align="right"><template #default="{ row }"><span class="num">{{ row.cpu }}</span></template></el-table-column>
              <el-table-column label="Status" width="120"><template #default="{ row }"><status-tag :value="row.status" /></template></el-table-column>
              <el-table-column width="120" align="right"><template #default="{ row }"><el-button link type="primary" @click="$root.go('/admin/grants/' + row.slug + '/cluster-a')">Edit grant</el-button></template></el-table-column>
            </el-table>
          </div>
        </panel>

        <panel v-else flush>
          <div class="t-tablewrap">
            <el-table :data="[{ name: 'Acme', slug: 'acme', by: 'alice@example.com', note: 'The purpose is too short to decide a quota.', at: '21 Nov 2026, 15:40' }]" style="width: 100%">
              <el-table-column label="Organization" min-width="150"><template #default="{ row }"><strong>{{ row.name }}</strong><div class="mono muted">{{ row.slug }} (released)</div></template></el-table-column>
              <el-table-column label="Requested by" min-width="180" prop="by" />
              <el-table-column label="Reason given" min-width="300" prop="note" />
              <el-table-column label="Rejected" width="170"><template #default="{ row }"><span class="num">{{ row.at }}</span></template></el-table-column>
            </el-table>
          </div>
        </panel>

        <el-dialog v-model="dialog" title="Create Organization" width="900px">
          <p class="muted" style="margin-bottom: 14px">For a customer you set up yourself. The Organization is active right away, with no request to review.</p>
          <el-form label-position="top" @submit.prevent>
            <div class="t-grid-form">
              <el-form-item label="Display name" for="co-name"><el-input id="co-name" v-model="n.name" /></el-form-item>
              <el-form-item label="Slug" for="co-slug"><el-input id="co-slug" v-model="n.slug" /></el-form-item>
              <el-form-item label="Owner" for="co-owner"><el-select id="co-owner" v-model="n.owner" filterable placeholder="Search by email" style="width: 100%"><el-option value="hank@example.com" label="hank@example.com" /><el-option value="peter@example.com" label="peter@example.com" /></el-select><div class="t-field-note">Only people who have logged in at least once are listed.</div></el-form-item>
            </div>
          </el-form>
          <h3 style="margin: 4px 0 12px">Starting grant on Cluster A</h3>
          <grant-form idp="co" />
          <template #footer><el-button @click="dialog = false">Cancel</el-button><el-button type="primary" @click="dialog = false">Create Organization</el-button></template>
        </el-dialog>
      </app-shell>`,
  };

  // /admin/organizations/:slug/review
  T.pages.AdminReview = {
    components: { GrantForm },
    data: () => ({ r: T.route, reason: '', rejecting: !!T.route.query.reject }),
    computed: { req() { const slug = this.r.path.split('/')[3]; return T.requests.find((x) => x.slug === slug) || T.requests[0]; } },
    template: `
      <app-shell area="admin" active="organizations">
        <page-header :title="req.name" :crumbs="[{ label: 'Organizations', to: '#/admin/organizations' }, { label: 'Review request' }]">
          <template #tags><code>{{ req.slug }}</code><status-tag value="Pending" label="Waiting for review" /></template>
        </page-header>

        <panel title="Request">
          <key-values :items="[['Requested by', req.by], ['Purpose', req.purpose], ['Waiting since', req.at], ['Other Organizations of this person', 'None']]">
            <template #v0>{{ req.by }} <status-tag tone="ok" label="Email verified" /> <span class="muted small">becomes OrgOwner</span></template>
          </key-values>
        </panel>

        <panel title="Starting grant on Cluster A" note="filled in from the onboarding policy">
          <div class="t-stack">
            <grant-form idp="rv" />
            <p class="t-explain">Approving creates the Organization, this grant and the groups <code>admins</code>, <code>developers</code> and <code>viewers</code>, with {{ req.by }} in <code>admins</code>. Later changes to the onboarding policy do not change this grant.</p>
            <div class="t-row t-between">
              <el-button type="danger" plain @click="rejecting = !rejecting">Reject…</el-button>
              <el-button type="primary" @click="$root.go('/admin/organizations', { tab: 'active' })">Approve with this grant</el-button>
            </div>
          </div>
        </panel>

        <panel v-if="rejecting" title="Reject this request" danger>
          <el-form label-position="top" style="max-width: 640px" @submit.prevent>
            <el-form-item label="Reason, shown to the requester" for="rv-reason"><el-input id="rv-reason" v-model="reason" type="textarea" :rows="3" /><div class="t-field-note">Say what they should change. Rejecting releases the slug <code>{{ req.slug }}</code>, and they can send a new request.</div></el-form-item>
            <el-button type="danger" :disabled="reason.trim().length < 10" @click="$root.go('/admin/organizations', { tab: 'rejected' })">Reject request</el-button>
            <span v-if="reason.trim().length < 10" class="t-field-note" style="margin-left: 10px">Write a reason first.</span>
          </el-form>
        </panel>
      </app-shell>`,
  };

  // /admin/clusters
  T.pages.AdminClusters = {
    data: () => ({ c: T.cluster, pool: T.pool, nodes: T.nodes, all: T.clusterNodes, net: T.netPool, grantDialog: ['give', 'dirty'].includes(T.route.query.dialog), dirty: T.route.query.dialog === 'dirty', poolDialog: T.route.query.dialog === 'pool', sel: T.clusterNodes.filter((n) => n.pool).map((n) => n.name) }),
    computed: {
      free() { return this.all.filter((n) => !n.kind && !n.pool && !n.org); },
      removed() { return this.all.filter((n) => n.pool && !this.sel.includes(n.name)).map((n) => n.name); },
      after() { return this.sel.length * 7 - 2; },
      tooSmall() { return this.after * this.pool.ratio < this.pool.cpu.reserved; },
    },
    methods: {
      why(n) { return n.kind ? n.kind + ' node' : n.org ? 'Dedicated to ' + n.org : n.pool ? 'In this pool' : 'Free'; },
    },
    template: `
      <app-shell area="admin" active="clusters">
        <page-header title="Clusters" description="Clusters registered with the platform, and the pools and nodes Organizations can be given on them.">
          <template #actions><el-button><el-icon><Plus /></el-icon>&nbsp;Register cluster</el-button></template>
        </page-header>

        <panel :title="c.name" note="cluster-a">
          <template #extra>
            <status-tag value="Ready" />
            <el-select model-value="Ready" size="small" style="width: 150px" aria-label="Change cluster status"><el-option value="Ready" label="Ready" /><el-option value="Maintenance" label="Maintenance" /><el-option value="Unavailable" label="Unavailable" /></el-select>
          </template>
          <key-values :items="[['Kubernetes', c.version], ['Last health check', c.lastCheck], ['kubectl endpoint', c.proxy], ['Web console', c.console], ['App domain', '*.' + c.appsDomain]]">
            <template #v2><span class="mono">{{ c.proxy }}</span></template><template #v3><span class="mono">{{ c.console }}</span></template><template #v4><span class="mono">*.{{ c.appsDomain }}</span></template>
          </key-values>
          <p class="t-field-note" style="margin-top: 14px">In Maintenance, existing Workspaces keep running and stay reachable; creating and resizing are paused for everyone.</p>
        </panel>

        <panel title="Shared pools" flush>
          <template #extra><el-button size="small">Add pool</el-button></template>
          <div class="t-tablewrap">
            <el-table :data="[pool]" style="width: 100%">
              <el-table-column label="Pool" min-width="160"><template #default="{ row }"><code>{{ row.id }}</code></template></el-table-column>
              <el-table-column label="Nodes" min-width="160"><template #default><code>wk2</code> <code>wk5</code></template></el-table-column>
              <el-table-column label="Overcommit ratio" width="120" align="right"><template #default="{ row }"><span class="num">{{ row.ratio }}×</span></template></el-table-column>
              <el-table-column label="CPU reserved" width="150" align="right"><template #default="{ row }"><span class="num">{{ row.cpu.reserved }} of {{ row.cpu.reservable }}</span></template></el-table-column>
              <el-table-column label="Memory reserved" width="170" align="right"><template #default="{ row }"><span class="num">{{ row.mem.reserved }} of {{ row.mem.reservable }} Gi</span></template></el-table-column>
              <el-table-column label="Capacity" width="140"><template #default="{ row }"><status-tag :value="row.status" /></template></el-table-column>
              <el-table-column width="130" align="right"><template #default><el-button link type="primary" @click="poolDialog = true">Change nodes</el-button></template></el-table-column>
            </el-table>
          </div>
        </panel>

        <panel title="Dedicated nodes" flush>
          <template #extra><el-button size="small" @click="dirty = false; grantDialog = true">Give a node to an Organization</el-button></template>
          <div class="t-tablewrap">
            <el-table :data="nodes" style="width: 100%">
              <el-table-column label="Node" width="100"><template #default="{ row }"><code>{{ row.name }}</code></template></el-table-column>
              <el-table-column label="Organization" width="140"><template #default>acme</template></el-table-column>
              <el-table-column label="Health" width="130"><template #default="{ row }"><status-tag :value="row.health" /></template></el-table-column>
              <el-table-column label="Capacity" width="180"><template #default="{ row }"><status-tag :value="row.capacity" /></template></el-table-column>
              <el-table-column label="CPU reserved" width="140" align="right"><template #default="{ row }"><span class="num">{{ row.cpu.reserved }} of {{ row.cpu.reservable }}</span></template></el-table-column>
              <el-table-column label="Memory reserved" width="160" align="right"><template #default="{ row }"><span class="num">{{ row.mem.reserved }} of {{ row.mem.reservable }} Gi</span></template></el-table-column>
              <el-table-column min-width="120" align="right"><template #default="{ row }"><el-tooltip v-if="row.workspaces.length" content="A Workspace still reserves capacity on this node."><span><el-button link disabled>Take back</el-button></span></el-tooltip><el-button v-else link type="danger">Take back</el-button></template></el-table-column>
            </el-table>
          </div>
        </panel>

        <panel title="Pod network pools" flush>
          <template #extra><el-button size="small">Add pool</el-button></template>
          <div class="t-tablewrap">
            <el-table :data="[net]" style="width: 100%">
              <el-table-column label="Pool" min-width="220"><template #default="{ row }"><code>{{ row.id }}</code></template></el-table-column>
              <el-table-column label="Range" width="160"><template #default="{ row }"><span class="mono">{{ row.cidr }}</span></template></el-table-column>
              <el-table-column label="Belongs to" width="180"><template #default>Every Organization</template></el-table-column>
              <el-table-column label="Block sizes" width="150"><template #default><span class="mono">/20 to /24</span></template></el-table-column>
              <el-table-column label="/24 blocks free" width="160" align="right"><template #default="{ row }"><span class="num">{{ row.free }} of {{ row.total }}</span></template></el-table-column>
            </el-table>
          </div>
        </panel>

        <el-dialog v-model="grantDialog" title="Give a node to an Organization" width="560px">
          <el-form label-position="top" @submit.prevent>
            <div class="t-grid-form">
              <el-form-item label="Node" for="ng-node"><el-select id="ng-node" :model-value="dirty ? 'wk7' : 'wk6'" style="width: 100%" @change="dirty = $event === 'wk7'"><el-option v-for="n in free" :key="n.name" :value="n.name" :label="n.name" /></el-select><div class="t-field-note">Only free nodes are listed. To use a node of a shared pool, take it out of the pool first.</div></el-form-item>
              <el-form-item label="Organization" for="ng-org"><el-select id="ng-org" model-value="acme" style="width: 100%"><el-option value="acme" label="Acme" /><el-option value="umbrella" label="Umbrella" /></el-select></el-form-item>
            </div>
          </el-form>
          <el-alert v-if="dirty" type="error" :closable="false" show-icon title="wk7 still runs Pods of other tenants">
            <div class="mono" style="margin: 4px 0 8px">umbrella-lab-web / web-7c9d4-x2k8p<br>umbrella-lab-web / web-7c9d4-m4q1z</div>
            <div>wk7 was taken out of a shared pool but not drained. Move these Pods first, then try again. The platform does not drain nodes for you:</div>
            <div class="mono">kubectl drain wk7 --ignore-daemonsets --delete-emptydir-data</div>
            <div class="t-errcode" style="margin-top: 6px">DEDICATED_NODE_NOT_CLEAN</div>
          </el-alert>
          <p v-else class="t-explain">The node is checked for Pods of other tenants. If it is clean, it gets a label and a taint so that only Acme's Pods run on it. Acme can use it once the node reports Ready.</p>
          <template #footer><el-button @click="grantDialog = false">Cancel</el-button><el-button type="primary" :disabled="dirty" @click="grantDialog = false">Give node</el-button></template>
        </el-dialog>

        <el-dialog v-model="poolDialog" title="Nodes of shared-general" width="560px">
          <el-checkbox-group v-model="sel" aria-label="Nodes of this pool">
            <ul class="t-list t-picklist">
              <li v-for="n in all" :key="n.name">
                <el-checkbox :value="n.name" :disabled="!!n.kind || !!n.org"><code>{{ n.name }}</code></el-checkbox>
                <span class="muted small num">{{ why(n) }}<template v-if="n.cpu"> · {{ n.cpu }} cores, {{ n.mem }} Gi</template></span>
              </li>
            </ul>
          </el-checkbox-group>
          <el-alert v-if="tooSmall" type="error" :closable="false" show-icon title="The pool would be too small" :description="'Workspaces already reserve ' + pool.cpu.reserved + ' cores on this pool. Keep more nodes, or ask Organizations to shrink their Workspaces. (COMPUTE_POOL_CAPACITY_EXCEEDED)'" style="margin-top: 14px" />
          <el-alert v-else-if="removed.length" type="warning" :closable="false" show-icon :title="'Pods on ' + removed.join(', ') + ' keep running'" style="margin-top: 14px">
            <div>Taking a node out only stops new Pods from landing on it. To empty the node, drain it yourself:</div>
            <div class="mono">kubectl drain {{ removed[0] }} --ignore-daemonsets --delete-emptydir-data</div>
          </el-alert>
          <p class="t-field-note" style="margin-top: 12px">After this change: {{ sel.length }} nodes, {{ after }} cores to reserve against, {{ pool.cpu.reserved }} reserved now.</p>
          <template #footer><el-button @click="poolDialog = false">Cancel</el-button><el-button type="primary" :disabled="tooSmall || !sel.length" @click="poolDialog = false">Save nodes</el-button></template>
        </el-dialog>
      </app-shell>`,
  };

  // /admin/onboarding-policy
  T.pages.AdminPolicy = {
    components: { GrantForm },
    template: `
      <app-shell area="admin" active="policy">
        <page-header title="Onboarding policy" description="The grant that is filled in when you review a request for a new Organization. You can still change it for each request."></page-header>
        <panel title="Default grant" note="on Cluster A">
          <div class="t-stack">
            <grant-form idp="op" />
            <p class="t-explain">Saving changes the starting point for <strong>future</strong> reviews only. Grants that were already given stay as they are.</p>
            <div class="t-row" style="justify-content: flex-end"><el-button>Discard changes</el-button><el-button type="primary">Save policy</el-button></div>
          </div>
        </panel>
        <panel title="Default Organization limits">
          <el-form label-position="top" @submit.prevent>
            <div class="t-fields">
              <el-form-item label="Members" for="op-m"><num-field id="op-m" :model-value="20" /></el-form-item>
              <el-form-item label="Groups" for="op-gr"><num-field id="op-gr" :model-value="10" /></el-form-item>
              <el-form-item label="Workspaces, all clusters" for="op-w"><num-field id="op-w" :model-value="8" /></el-form-item>
            </div>
          </el-form>
        </panel>
      </app-shell>`,
  };

  // /admin/grants  &state=empty
  T.pages.AdminGrants = {
    data: () => ({ rows: T.route.query.state === 'empty' ? [] : T.grants, q: '' }),
    methods: { open(row) { T.go('/admin/grants/' + row.slug + '/' + row.clusterId); } },
    template: `
      <app-shell area="admin" active="grants">
        <page-header title="Grants" description="What each Organization may reserve on each cluster. A grant is created when an Organization is approved."></page-header>
        <panel flush>
          <div class="t-row" style="padding: 12px 20px; border-bottom: 1px solid var(--t-line-soft)">
            <el-input v-model="q" placeholder="Find an Organization" clearable style="width: 260px" aria-label="Find an Organization" />
          </div>
          <div class="t-tablewrap">
            <el-table :data="rows.filter((r) => r.org.toLowerCase().includes(q.toLowerCase()))" style="width: 100%" row-class-name="t-clickable" empty-text="No grant yet. Approve an Organization to create its first grant." @row-click="open">
              <el-table-column label="Organization" min-width="150"><template #default="{ row }"><a :href="'#/admin/grants/' + row.slug + '/' + row.clusterId" @click.stop><strong>{{ row.org }}</strong></a></template></el-table-column>
              <el-table-column label="Cluster" min-width="120" prop="cluster" />
              <el-table-column label="Workspaces" min-width="110"><template #default="{ row }"><span class="num">{{ row.ws }}</span></template></el-table-column>
              <el-table-column label="Shared CPU" min-width="130"><template #default="{ row }"><span class="num">{{ row.cpu }}</span></template></el-table-column>
              <el-table-column label="Shared memory" min-width="140"><template #default="{ row }"><span class="num">{{ row.mem }}</span></template></el-table-column>
              <el-table-column label="Compute" min-width="200"><template #default="{ row }"><code>{{ row.pool }}</code><span v-if="row.nodes" class="muted"> + {{ row.nodes }} dedicated nodes</span></template></el-table-column>
              <el-table-column width="90" align="right"><template #default="{ row }"><el-button link type="primary" @click.stop="open(row)">Edit</el-button></template></el-table-column>
            </el-table>
          </div>
        </panel>
      </app-shell>`,
  };

  // /admin/grants/:orgSlug/:clusterId
  T.pages.AdminGrant = {
    components: { GrantForm },
    data: () => ({ confirm: T.route.query.dialog === 'save' }),
    template: `
      <app-shell area="admin" active="grants">
        <page-header title="Grant of Acme on Cluster A" :crumbs="[{ label: 'Grants', to: '#/admin/grants' }, { label: 'Acme on Cluster A' }]" description="A grant is a ceiling for what Acme may reserve from now on. Changing it never touches Workspaces that already run."></page-header>
        <panel>
          <div class="t-stack" style="gap: 20px">
            <grant-form idp="gr" :current="{}" />
            <div class="t-row t-between">
              <el-tooltip content="Acme still has 3 Workspaces on Cluster A."><span><el-button type="danger" plain disabled>Remove grant</el-button></span></el-tooltip>
              <div class="t-row"><el-button>Discard changes</el-button><el-button type="primary" @click="confirm = true">Review changes</el-button></div>
            </div>
          </div>
        </panel>
        <panel title="Dedicated nodes of Acme" flush>
          <template #extra><a href="#/admin/clusters" class="small">Manage nodes</a></template>
          <ul class="t-list">
            <li><span><code>wk3</code> &nbsp;<status-tag value="Ready" /> <status-tag value="PartiallyAllocated" /></span><span class="num muted">4 of 7 cores, 8 of 14 Gi reserved</span></li>
            <li><span><code>wk4</code> &nbsp;<status-tag value="NotReady" /></span><span class="num muted">0 of 7 cores reserved</span></li>
          </ul>
        </panel>

        <el-dialog v-model="confirm" title="Save these changes to Acme's grant?" width="520px">
          <el-table :data="[['Workspaces', '4', '5'], ['Shared CPU requests', '4 cores', '6 cores'], ['Shared memory limits', '16 Gi', '12 Gi']]" style="width: 100%">
            <el-table-column label="Field" min-width="180"><template #default="{ row }">{{ row[0] }}</template></el-table-column>
            <el-table-column label="Now" width="110"><template #default="{ row }"><span class="num muted">{{ row[1] }}</span></template></el-table-column>
            <el-table-column label="After" width="110"><template #default="{ row }"><strong class="num">{{ row[2] }}</strong></template></el-table-column>
          </el-table>
          <p class="t-explain" style="margin-top: 14px">Running Workspaces keep everything they have reserved. Acme will be able to reserve less memory limit than before: 2 Gi instead of 6 Gi.</p>
          <template #footer><el-button @click="confirm = false">Back</el-button><el-button type="primary" @click="confirm = false">Save grant</el-button></template>
        </el-dialog>
      </app-shell>`,
  };

  // /admin/operations
  T.pages.AdminOperations = {
    data: () => ({ ops: T.operations, state: 'needs', force: T.route.query.dialog ? T.operations[0] : null, typed: '' }),
    computed: { rows() { return this.state === 'needs' ? this.ops.filter((o) => o.state === 'Blocked' || o.state === 'Failed') : this.state ? this.ops.filter((o) => o.state === this.state) : this.ops; } },
    template: `
      <app-shell area="admin" active="operations">
        <page-header title="Operations" description="Every change the platform is applying to a cluster. Blocked and failed ones need a decision."></page-header>
        <panel flush>
          <div class="t-row" style="padding: 12px 20px; border-bottom: 1px solid var(--t-line-soft)">
            <el-radio-group v-model="state" aria-label="Filter operations">
              <el-radio-button value="needs">Needs attention (2)</el-radio-button>
              <el-radio-button value="Running">Running</el-radio-button>
              <el-radio-button value="Succeeded">Succeeded</el-radio-button>
              <el-radio-button value="">All</el-radio-button>
            </el-radio-group>
          </div>
          <div class="t-tablewrap">
            <el-table :data="rows" style="width: 100%" empty-text="No operation in this state.">
              <el-table-column label="Operation" min-width="170"><template #default="{ row }"><span class="mono">{{ row.type }}</span><div class="mono muted">{{ row.id }}</div></template></el-table-column>
              <el-table-column label="On" min-width="150"><template #default="{ row }">{{ row.org }} / {{ row.target }}</template></el-table-column>
              <el-table-column label="State" width="130"><template #default="{ row }"><status-tag :value="row.state" /></template></el-table-column>
              <el-table-column label="Why" min-width="280" prop="detail" />
              <el-table-column label="Age" width="90"><template #default="{ row }"><span class="num">{{ row.age }}</span></template></el-table-column>
              <el-table-column width="160" align="right">
                <template #default="{ row }">
                  <el-button v-if="row.state === 'Failed' || row.state === 'Blocked'" link type="primary">Retry</el-button>
                  <el-button v-if="row.state === 'Blocked'" link type="danger" @click="force = row; typed = ''">Force…</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </panel>

        <el-dialog :model-value="!!force" title="Force this deletion?" width="520px" @close="force = null">
          <div v-if="force" class="t-stack">
            <el-alert type="error" :closable="false" show-icon title="Forcing removes the finalizers of 2 volumes" description="Kubernetes then deletes them without waiting for the storage to confirm. Data that was not released can be left behind on the NFS server. This is written to the audit log with your name." />
            <div class="mono">umbrella-lab-db / data-postgres-0<br>umbrella-lab-db / backups</div>
            <confirm-by-name :name="'umbrella-' + force.target" action="Force deletion" />
          </div>
          <template #footer><el-button @click="force = null">Cancel</el-button></template>
        </el-dialog>
      </app-shell>`,
  };

  // /admin/health
  T.pages.AdminHealth = {
    data: () => ({ rows: T.health }),
    template: `
      <app-shell area="admin" active="health">
        <page-header title="Platform health" description="The parts the platform itself needs to work.">
          <template #tags><status-tag tone="warn" label="1 warning" /></template>
        </page-header>
        <panel flush>
          <ul class="t-list">
            <li v-for="h in rows" :key="h.name">
              <span class="t-row"><status-tag :tone="h.state" :label="h.state === 'ok' ? 'Healthy' : 'Warning'" /><strong>{{ h.name }}</strong></span>
              <span class="muted">{{ h.detail }}</span>
            </li>
          </ul>
        </panel>
        <div class="t-cols">
          <panel title="Work in progress"><key-values :items="[['Operations running', '1'], ['Operations needing attention', '2'], ['Groups waiting for sync', '1'], ['Requests waiting for review', '2']]"><template #v1><a href="#/admin/operations">2</a></template><template #v3><a href="#/admin/organizations">2</a></template></key-values></panel>
          <panel title="Capacity on Cluster A"><div class="t-meters"><quota-meter label="Shared pool CPU" :used="5.5" :total="12" unit="cores" /><quota-meter label="Pod network /24 blocks" :used="3" :total="256" /></div></panel>
        </div>
      </app-shell>`,
  };
})();
