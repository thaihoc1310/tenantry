// Workspace screens: the create wizard and the detail page with its tabs.
(function () {
  T.pages = T.pages || {};
  const base = '#/orgs/acme';

  // What is left of the grant before this Workspace (T.grant: total - used).
  const LEFT = { rcpu: 1, rmem: 2, lcpu: 3, lmem: 6, pods: 30, storage: 55, namespaces: 7 };

  // /orgs/acme/workspaces/new
  T.pages.Wizard = {
    data: () => ({
      step: 0,
      visited: [true, false, false, false, false, false, false],
      steps: ['Name', 'Cluster and compute', 'Resources', 'Network', 'Namespaces', 'Access', 'Review'],
      f: {
        name: 'Orbit', slug: 'orbit', slugEdited: false, desc: '',
        mode: 'Shared', node: 'wk3',
        rcpu: 1, rmem: 2, lcpu: 2, lmem: 4, pods: 10, storage: 5,
        netAuto: true, prefix: 24, cidr: '',
        maxNs: 3, ns: ['web'],
        owners: ['admins'], devs: ['developers'], viewers: [],
      },
      pool: T.pool, nodes: T.nodes, net: T.netPool, groups: T.groups, LEFT,
    }),
    computed: {
      dedicated() { return this.f.mode === 'Dedicated'; },
      tenant() { return 'acme-' + (this.f.slug || 'slug'); },
      slugError() { return /^[a-z0-9]([a-z0-9-]{1,18}[a-z0-9])?$/.test(this.f.slug) ? '' : 'Use 3 to 20 lowercase letters, digits or hyphens.'; },
      limCpu() { return this.dedicated ? this.f.rcpu : this.f.lcpu; },
      limMem() { return this.dedicated ? this.f.rmem : this.f.lmem; },
      errs() {
        const e = {}; const f = this.f;
        if (this.dedicated) { if (f.rcpu > 3) e.rcpu = 'Node wk3 has only 3 cores left. (DEDICATED_NODE_INSUFFICIENT_CAPACITY)'; if (f.rmem > 6) e.rmem = 'Node wk3 has only 6 Gi left. (DEDICATED_NODE_INSUFFICIENT_CAPACITY)'; return e; }
        if (f.rcpu > LEFT.rcpu) e.rcpu = 'Only ' + LEFT.rcpu + ' core is left in the shared quota. Lower this, or ask a Platform Admin for more. (CLUSTER_GRANT_CPU_QUOTA_EXCEEDED)';
        if (f.rmem > LEFT.rmem) e.rmem = 'Only ' + LEFT.rmem + ' Gi is left in the shared quota. (CLUSTER_GRANT_MEMORY_QUOTA_EXCEEDED)';
        if (!this.dedicated && f.lcpu < f.rcpu) e.lcpu = 'A limit cannot be lower than its request.';
        if (!this.dedicated && f.lcpu > f.rcpu * this.pool.ratio) e.lcpu = 'On this pool limits may be at most ' + this.pool.ratio + '× the request (' + f.rcpu * this.pool.ratio + ' cores).';
        if (!this.dedicated && f.lmem < f.rmem) e.lmem = 'A limit cannot be lower than its request.';
        if (!this.dedicated && f.lmem > f.rmem * this.pool.ratio) e.lmem = 'On this pool limits may be at most ' + this.pool.ratio + '× the request (' + f.rmem * this.pool.ratio + ' Gi).';
        if (this.limCpu > LEFT.lcpu) e.lcpu = 'Only ' + LEFT.lcpu + ' cores of CPU limit are left in the shared quota.';
        if (this.limMem > LEFT.lmem) e.lmem = 'Only ' + LEFT.lmem + ' Gi of memory limit is left in the shared quota.';
        return e;
      },
      stepErr() {
        return [!!this.slugError || !this.f.name, false, Object.keys(this.errs).length > 0, false, this.f.maxNs < this.f.ns.length, this.f.owners.length === 0, false];
      },
      blocked() { return this.stepErr.some(Boolean); },
      addresses() { return Math.pow(2, 32 - this.f.prefix) - 2; },
    },
    created() {
      const q = T.route.query;
      if (q.mode === 'dedicated') this.f.mode = 'Dedicated';
      if (q.over) this.f.rcpu = 2;
      if (q.advanced) this.f.netAuto = false;
      const s = Number(q.step);
      if (s >= 1 && s <= 7) { this.step = s - 1; for (let i = 0; i < s; i++) this.visited[i] = true; }
    },
    watch: { 'f.name'(v) { if (!this.f.slugEdited) this.f.slug = v.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, 20); } },
    methods: {
      cores(n) { return n + (n === 1 ? ' core' : ' cores'); },
      to(i) { this.step = i; this.visited[i] = true; window.scrollTo(0, 0); },
      cls(i) { return { done: this.visited[i] && i !== this.step && !this.stepErr[i], err: this.visited[i] && i !== this.step && this.stepErr[i] }; },
      mark(i) { return this.visited[i] && i !== this.step ? (this.stepErr[i] ? '!' : '✓') : i + 1; },
      groupOff(g) { return g.sync !== 'Synced'; },
    },
    template: `
      <app-shell active="workspaces">
        <page-header title="Create Workspace" :crumbs="[{ label: 'Workspaces', to: '#/orgs/acme/workspaces' }, { label: 'Create' }]"></page-header>

        <div class="t-wizard">
          <ol class="t-wsteps" aria-label="Steps">
            <li v-for="(s, i) in steps" :key="s"><button type="button" class="t-wstep" :class="cls(i)" :aria-current="i === step ? 'step' : null" @click="to(i)"><span class="t-wstep-n">{{ mark(i) }}</span>{{ s }}</button></li>
          </ol>

          <section class="t-panel">
            <div class="t-panel-head"><div class="t-row"><h2>{{ steps[step] }}</h2><span class="muted">Step {{ step + 1 }} of {{ steps.length }}</span></div></div>

            <!-- 1 Name -->
            <div v-if="step === 0" class="t-panel-body">
              <el-form label-position="top" style="max-width: 560px" @submit.prevent>
                <el-form-item label="Name" for="w-name"><el-input id="w-name" v-model="f.name" maxlength="40" /></el-form-item>
                <el-form-item label="Slug" for="w-slug" :error="slugError"><el-input id="w-slug" v-model="f.slug" maxlength="20" @input="f.slugEdited = true" /></el-form-item>
                <el-alert type="warning" :closable="false" show-icon title="The slug cannot be changed later" style="margin-bottom: 18px">
                  <div>Name in Kubernetes: <code>{{ tenant }}</code></div>
                  <div>Namespaces: <code>{{ tenant }}-&lt;namespace&gt;</code></div>
                  <div>App addresses: <code>&lt;name&gt;-{{ tenant }}.mvp.tenantry.site</code></div>
                </el-alert>
                <el-form-item label="Description (optional)" for="w-desc"><el-input id="w-desc" v-model="f.desc" type="textarea" :rows="2" /></el-form-item>
              </el-form>
            </div>

            <!-- 2 Cluster and compute -->
            <div v-else-if="step === 1" class="t-panel-body t-stack" style="gap: 24px">
              <div class="t-stack" style="gap: 10px">
                <h3>Cluster</h3>
                <div class="t-row t-between" style="padding: 12px 14px; border: 1px solid var(--t-line); border-radius: 8px">
                  <div class="t-row"><strong>Cluster A</strong><status-tag value="Ready" label="Cluster ready" /></div>
                  <span class="muted small">Chosen for you: it is the only cluster granted to Acme.</span>
                </div>
                <p class="t-field-note">A Workspace lives on one cluster and cannot be moved to another later.</p>
              </div>

              <div class="t-stack" style="gap: 10px">
                <h3 id="mode-label">Where should the Pods run?</h3>
                <div class="t-options" role="group" aria-labelledby="mode-label">
                  <button type="button" class="t-option" :aria-pressed="f.mode === 'Shared'" @click="f.mode = 'Shared'">
                    <span class="t-option-title">Shared nodes</span>
                    <span class="muted small">Nodes shared with other Organizations. Limits can be higher than requests, so Pods may burst when the nodes have room.</span>
                  </button>
                  <button type="button" class="t-option" :aria-pressed="f.mode === 'Dedicated'" @click="f.mode = 'Dedicated'">
                    <span class="t-option-title">Dedicated node</span>
                    <span class="muted small">One node that only Acme uses. The CPU and memory you ask for are reserved on it, and limits equal requests.</span>
                  </button>
                </div>
                <p class="t-field-note">Both modes give the same rights inside the Workspace. They only differ in which machines run your Pods.</p>
              </div>

              <div v-if="!dedicated" class="t-stack" style="gap: 10px">
                <h3>Shared pool</h3>
                <div class="t-row t-between"><div class="t-row"><code>{{ pool.id }}</code><status-tag :value="pool.status" /></div><span class="muted small">Chosen for you: the only pool in your grant.</span></div>
                <div class="t-meters two">
                  <quota-meter label="Pool CPU reserved" :used="pool.cpu.reserved" :total="pool.cpu.reservable" unit="cores" />
                  <quota-meter label="Pool memory reserved" :used="pool.mem.reserved" :total="pool.mem.reservable" unit="Gi" />
                </div>
              </div>

              <div v-else class="t-stack" style="gap: 10px">
                <h3 id="node-label">Node</h3>
                <div class="t-options" role="group" aria-labelledby="node-label">
                  <button v-for="n in nodes" :key="n.name" type="button" class="t-option" :disabled="n.health !== 'Ready'" :aria-pressed="f.node === n.name && n.health === 'Ready'" @click="f.node = n.name">
                    <span class="t-option-title"><code>{{ n.name }}</code><status-tag :value="n.health" /></span>
                    <span class="num">{{ n.cpu.reservable - n.cpu.reserved }} of {{ n.cpu.reservable }} cores left · {{ n.mem.reservable - n.mem.reserved }} of {{ n.mem.reservable }} Gi left</span>
                    <span v-if="n.why" class="t-option-why">{{ n.why }}</span>
                    <span v-else class="muted small">Also used by: {{ n.workspaces.join(', ') }}</span>
                  </button>
                </div>
                <p class="t-field-note">The node belongs to Acme, and several of Acme's Workspaces can share its capacity.</p>
              </div>
            </div>

            <!-- 3 Resources -->
            <div v-else-if="step === 2" class="t-panel-body t-stack" style="gap: 20px">
              <p v-if="dedicated" class="t-explain">These numbers are reserved on node <code>{{ f.node }}</code> as soon as the Workspace is created. The node belongs to Acme, so they are limited by what is left on the node and <strong>do not count against Acme's shared quota</strong>. Limits equal requests, which gives Pods Guaranteed quality of service.</p>
              <p v-else class="t-explain">These numbers are taken out of Acme's shared quota on Cluster A as soon as the Workspace is created, whether or not Pods use them.</p>
              <el-form label-position="top" @submit.prevent>
                <div class="t-stack" style="gap: 18px">
                  <resource-grid idp="w" :value="f" :errors="errs" :lock-limits="dedicated" :notes="dedicated ? { rcpu: '3 left on ' + f.node, rmem: '6 left on ' + f.node } : { rcpu: LEFT.rcpu + ' left in the shared quota', rmem: LEFT.rmem + ' left in the shared quota', lcpu: LEFT.lcpu + ' left in the shared quota', lmem: LEFT.lmem + ' left in the shared quota' }" />
                  <div class="t-fields">
                    <el-form-item label="Pods" for="w-pods"><num-field id="w-pods" v-model="f.pods" :min="1" :max="dedicated ? 80 : LEFT.pods" /><div class="t-field-note num">{{ dedicated ? '80 left on ' + f.node : LEFT.pods + ' left in the shared quota' }}</div></el-form-item>
                    <el-form-item label="Storage" for="w-sto"><num-field id="w-sto" v-model="f.storage" :max="LEFT.storage" unit="Gi" /><div class="t-field-note num">{{ LEFT.storage }} left in the grant</div></el-form-item>
                  </div>
                </div>
              </el-form>
              <div class="t-stack" style="gap: 12px">
                <h3>{{ dedicated ? 'Node ' + f.node + ' after this Workspace' : "Acme's shared quota after this Workspace" }}</h3>
                <div v-if="dedicated" class="t-meters two">
                  <quota-meter label="CPU reserved" :used="4" :pending="f.rcpu" :total="7" unit="cores" />
                  <quota-meter label="Memory reserved" :used="8" :pending="f.rmem" :total="14" unit="Gi" />
                </div>
                <div v-else class="t-meters tight">
                  <quota-meter label="CPU requests" :used="3" :pending="f.rcpu" :total="4" unit="cores" />
                  <quota-meter label="Memory requests" :used="6" :pending="f.rmem" :total="8" unit="Gi" />
                  <quota-meter label="CPU limits" :used="5" :pending="limCpu" :total="8" unit="cores" />
                  <quota-meter label="Memory limits" :used="10" :pending="limMem" :total="16" unit="Gi" />
                </div>
                <p class="t-field-note">Striped part: what this Workspace adds.</p>
              </div>
            </div>

            <!-- 4 Network -->
            <div v-else-if="step === 3" class="t-panel-body t-stack" style="gap: 18px">
              <el-radio-group v-model="f.netAuto" aria-label="How to pick the Pod network">
                <el-radio :value="true">Pick an address block for me</el-radio>
                <el-radio :value="false">Advanced: choose the block</el-radio>
              </el-radio-group>
              <key-values v-if="f.netAuto" :items="[['Pool', net.id], ['Block size', '/24, 254 Pod addresses'], ['Block', 'chosen when the Workspace is created']]"><template #v0><code>{{ net.id }}</code></template></key-values>
              <el-form v-else label-position="top" style="max-width: 560px" @submit.prevent>
                <el-form-item label="Pool" for="w-pool"><el-select id="w-pool" :model-value="net.id" style="width: 100%"><el-option :value="net.id" :label="net.id + '  (' + net.cidr + ')'" /></el-select></el-form-item>
                <el-form-item label="Block size" for="w-prefix"><el-select id="w-prefix" v-model="f.prefix" style="width: 100%"><el-option v-for="p in [24, 23, 22, 21, 20]" :key="p" :value="p" :label="'/' + p + '  (' + (Math.pow(2, 32 - p) - 2) + ' Pod addresses)'" /></el-select></el-form-item>
                <el-form-item label="Exact block (optional)" for="w-cidr"><el-input id="w-cidr" v-model="f.cidr" placeholder="10.240.8.0/24" /><div class="t-field-note">Leave empty to get the next free block of this size. A block that overlaps another Workspace is refused.</div></el-form-item>
              </el-form>
              <el-alert type="warning" :closable="false" show-icon title="The address block cannot be changed once Pods are running" :description="'This Workspace gets ' + (f.netAuto ? 254 : addresses) + ' Pod addresses. Pods of other Workspaces cannot reach them unless you publish a Route.'" />
            </div>

            <!-- 5 Namespaces -->
            <div v-else-if="step === 4" class="t-panel-body">
              <el-form label-position="top" style="max-width: 560px" @submit.prevent>
                <el-form-item label="Namespace limit" for="w-maxns" :error="f.maxNs < f.ns.length ? 'Lower than the number of namespaces you are creating below.' : ''">
                  <num-field id="w-maxns" v-model="f.maxNs" :min="1" :max="LEFT.namespaces" style="width: 180px" />
                  <div class="t-field-note">How many namespaces this Workspace may hold. Acme has {{ LEFT.namespaces }} left in its grant.</div>
                </el-form-item>
                <el-form-item label="Namespaces to create now (optional)" for="w-ns">
                  <el-select id="w-ns" v-model="f.ns" multiple filterable allow-create default-first-option :reserve-keyword="false" placeholder="Type a name and press Enter" style="width: 100%"></el-select>
                  <div class="t-field-note">Created as <span v-for="(n, i) in f.ns" :key="n"><code>{{ tenant }}-{{ n }}</code><template v-if="i < f.ns.length - 1">, </template></span><span v-if="!f.ns.length">nothing yet. You can add namespaces later.</span></div>
                </el-form-item>
              </el-form>
              <p class="t-explain">Members with the <strong>TenantOwner</strong> role can create more namespaces later, from the portal or with kubectl. Developers and Viewers cannot.</p>
            </div>

            <!-- 6 Access -->
            <div v-else-if="step === 5" class="t-panel-body t-stack">
              <el-form label-position="top" style="max-width: 560px" @submit.prevent>
                <el-form-item label="Owner groups" for="w-own" :error="f.owners.length ? '' : 'Choose at least one owner group. (LAST_OWNER_BINDING)'">
                  <el-select id="w-own" v-model="f.owners" multiple style="width: 100%"><el-option v-for="g in groups" :key="g.name" :value="g.name" :label="g.name + (groupOff(g) ? '  (sync pending)' : '')" :disabled="groupOff(g)" /></el-select>
                  <div class="t-field-note">TenantOwner: create namespaces, manage workloads and access inside the Workspace.</div>
                </el-form-item>
                <el-form-item label="Developer groups (optional)" for="w-dev">
                  <el-select id="w-dev" v-model="f.devs" multiple style="width: 100%"><el-option v-for="g in groups" :key="g.name" :value="g.name" :label="g.name + (groupOff(g) ? '  (sync pending)' : '')" :disabled="groupOff(g)" /></el-select>
                  <div class="t-field-note">Developer: deploy and change workloads in existing namespaces.</div>
                </el-form-item>
                <el-form-item label="Viewer groups (optional)" for="w-view">
                  <el-select id="w-view" v-model="f.viewers" multiple style="width: 100%"><el-option v-for="g in groups" :key="g.name" :value="g.name" :label="g.name + (groupOff(g) ? '  (sync pending)' : '')" :disabled="groupOff(g)" /></el-select>
                  <div class="t-field-note">Viewer: read only.</div>
                </el-form-item>
              </el-form>
              <el-alert type="info" :closable="false" show-icon title="The group backend cannot be chosen yet" description="It is still being synced to the login system. Tokens do not contain it, so its members would get no access." />
              <p class="t-explain">Nobody in a Workspace can create cluster-wide resources such as nodes, storage classes or custom resource definitions.</p>
            </div>

            <!-- 7 Review -->
            <div v-else class="t-panel-body t-stack" style="gap: 20px">
              <el-alert v-if="blocked" type="error" :closable="false" show-icon title="Fix the steps marked with ! before creating the Workspace" />
              <key-values :items="[
                ['Name', f.name],
                ['Cluster', 'Cluster A (cannot be changed later)'],
                ['Compute', dedicated ? 'Dedicated node ' + f.node : 'Shared pool ' + pool.id],
                ['CPU', cores(f.rcpu) + ' requested, ' + cores(limCpu) + ' limit'],
                ['Memory', f.rmem + ' Gi requested, ' + limMem + ' Gi limit'],
                ['Pods and storage', f.pods + ' Pods, ' + f.storage + ' Gi'],
                ['Pod network', f.netAuto ? 'A free /24 from ' + net.id : (f.cidr || 'A free /' + f.prefix) + ' from ' + net.id],
                ['Namespaces', 'Up to ' + f.maxNs + (f.ns.length ? '; creating ' + f.ns.join(', ') : '')],
                ['Owners', f.owners.join(', ') || 'none'],
                ['Developers', f.devs.join(', ') || 'none'],
                ['Viewers', f.viewers.join(', ') || 'none'],
              ]"><template #v0>{{ f.name }} <code>{{ tenant }}</code></template></key-values>
              <p class="t-explain">Creating takes about a minute. You can follow each step on the next page and leave it at any time.</p>
            </div>

            <div class="t-wfoot">
              <el-button :disabled="step === 0" @click="to(step - 1)">Back</el-button>
              <div class="t-row">
                <el-button @click="$root.go('/orgs/acme/workspaces')">Cancel</el-button>
                <el-button v-if="step < steps.length - 1" type="primary" @click="to(step + 1)">Continue</el-button>
                <el-button v-else type="primary" :disabled="blocked" @click="$root.go('/orgs/acme/workspaces/orbit', { state: 'provisioning' })">Create Workspace</el-button>
              </div>
            </div>
          </section>
        </div>
      </app-shell>`,
  };

  // /orgs/acme/workspaces/:slug?tab=…&state=ready|provisioning|failed|degraded|cordoned|blocked  &as=developer
  T.pages.Workspace = {
    data: () => ({
      r: T.route, base, cluster: T.cluster, bindings: T.bindings, namespaces: T.namespaces, routes: T.routes,
      nsDialog: T.route.query.dialog === 'namespace', nsName: '', routeDialog: T.route.query.dialog === 'route', route: { ns: 'frontend', name: 'docs', service: 'docs', port: 80 }, bindDialog: false,
      tabs: [['overview', 'Overview'], ['resources', 'Resources'], ['compute', 'Compute'], ['network', 'Network'], ['namespaces', 'Namespaces'], ['routes', 'Routes'], ['members', 'Access roles'], ['access', 'Connect'], ['danger', 'Danger zone']],
    }),
    computed: {
      slug() { return this.r.path.split('/').pop(); },
      w() { return T.workspaces.find((x) => x.slug === this.slug) || T.workspaces[0]; },
      tab() { return this.r.query.tab || 'overview'; },
      st() { return this.r.query.state || (this.w.state === 'Provisioning' ? 'provisioning' : 'ready'); },
      state() { return { ready: 'Ready', provisioning: 'Provisioning', failed: 'Failed', degraded: 'Degraded', cordoned: 'Cordoned', blocked: 'Deleting' }[this.st]; },
      dev() { return this.r.query.as === 'developer'; },
      usable() { return ['ready', 'degraded', 'cordoned'].includes(this.st); },
      conds() {
        if (this.st === 'provisioning') return T.conditionsProvisioning;
        if (this.st === 'failed') return T.conditionsFailed;
        const w = this.w; const where = w.mode === 'Shared' ? 'pool ' + w.place : 'node ' + w.place;
        const msg = { ComputeReady: 'Pods are placed on ' + where + '.', NetworkReady: 'IP pool ' + w.cidr + ' is ready.', CapsuleTenantReady: 'Tenant acme-' + w.slug + ' exists.' };
        return T.conditionsReady.map((c) => {
          if (this.st === 'degraded' && c.type === 'ComputeReady') return { type: c.type, status: 'False', reason: 'NodeOvercommitted', message: 'Node ' + w.place + ' has 8 cores reserved but can hold 7.', when: 'since 09:14' };
          return msg[c.type] ? { ...c, message: msg[c.type] } : c;
        });
      },
      building() { return this.st === 'provisioning' || this.st === 'failed'; },
      path() { return '/orgs/acme/workspaces/' + this.slug; },
      whyNot() { return this.st === 'blocked' ? 'This Workspace is being deleted.' : 'Available when the Workspace is Ready.'; },
    },
    methods: {
      tabHref(t) { const q = { tab: t }; if (this.r.query.state) q.state = this.r.query.state; if (this.r.query.as) q.as = this.r.query.as; return T.href(this.path, q); },
    },
    template: `
      <app-shell active="workspaces" :limited="dev">
        <page-header :title="w.name" :crumbs="[{ label: 'Workspaces', to: '#/orgs/acme/workspaces' }, { label: w.name }]">
          <template #tags><code>acme-{{ w.slug }}</code><status-tag :value="state" /><el-tag v-if="dev" type="info" effect="plain" round>You are Developer</el-tag></template>
          <template #actions>
            <el-tooltip :content="whyNot" :disabled="usable"><span><el-button :disabled="!usable"><el-icon><Download /></el-icon>&nbsp;Kubeconfig</el-button></span></el-tooltip>
            <el-tooltip :content="whyNot" :disabled="usable"><span><el-button type="primary" :disabled="!usable"><el-icon><Monitor /></el-icon>&nbsp;Open web console</el-button></span></el-tooltip>
          </template>
        </page-header>

        <nav class="t-tabs" aria-label="Workspace sections"><a v-for="t in tabs" :key="t[0]" :href="tabHref(t[0])" :aria-current="tab === t[0] ? 'page' : null">{{ t[1] }}</a></nav>

        <!-- ================= Overview ================= -->
        <template v-if="tab === 'overview'">
          <el-alert v-if="st === 'provisioning'" type="info" :closable="false" show-icon title="Creating this Workspace is taking longer than usual" description="It has been 2 minutes. The step in progress is shown below; nothing is needed from you. You can leave this page." />
          <el-alert v-if="st === 'failed'" type="error" :closable="false" show-icon title="The Workspace could not be created">
            <div>The Pod network was not ready after 5 minutes. Your quota and address block stay reserved, so retrying continues from the failed step.</div>
            <div class="t-errcode" style="margin: 6px 0 10px">NetworkReady = False (IPPoolNotReady) · operation op-01jd3z</div>
            <el-button type="danger" size="small">Retry</el-button> <el-button size="small" @click="$root.go(path, { tab: 'danger', state: 'failed' })">Delete instead</el-button>
          </el-alert>
          <el-alert v-if="st === 'degraded'" type="warning" :closable="false" show-icon title="Node wk3 has more reserved than it can hold" description="Your Pods keep running. A Platform Admin has been notified and is fixing the node's capacity. Resizing this Workspace is paused until then." />
          <el-alert v-if="st === 'cordoned'" type="warning" :closable="false" show-icon title="This Workspace is cordoned: read-only">
            <div>Running Pods keep running, but nobody can change anything with kubectl or the web console.</div>
            <div style="margin-top: 8px"><el-button size="small">Uncordon</el-button></div>
          </el-alert>
          <el-alert v-if="st === 'blocked'" type="error" :closable="false" show-icon title="Deleting is blocked by 2 volumes">
            <div>These PersistentVolumeClaims have not been released after 18 minutes. Wait, or ask a Platform Admin to force the deletion.</div>
            <div class="mono" style="margin-top: 6px">acme-solar-backend / data-postgres-0<br>acme-solar-backend / uploads</div>
            <div class="t-errcode" style="margin-top: 6px">Operation Blocked · op-01jd4f</div>
          </el-alert>

          <div class="t-split">
            <panel :title="building ? 'Progress' : 'Health'" :note="st === 'provisioning' ? '2 of 7 done' : st === 'failed' ? 'stopped at step 3 of 7' : st === 'degraded' ? '6 of 7 conditions are True' : 'all 7 conditions are True'">
              <condition-timeline :conditions="conds" />
            </panel>
            <div class="t-stack" style="gap: 20px">
              <panel title="Summary">
                <key-values compact :items="[['Cluster', 'Cluster A'], ['Compute', w.mode + ' on ' + w.place], ['Pod network', w.cidr], ['Namespaces', w.namespaces + ' of 6'], ['Created', w.created + ' by alice@example.com']]"><template #v2><span class="mono">{{ w.cidr }}</span></template></key-values>
              </panel>
              <panel title="Recent changes" flush>
                <ul class="t-list">
                  <template v-if="w.slug === 'solar'">
                    <li v-if="st === 'blocked'"><span class="mono">workspace.delete</span><status-tag value="Blocked" /></li>
                    <li><span><span class="mono">route.create</span> api</span><status-tag value="Running" /></li>
                    <li><span><span class="mono">namespace.create</span> backend</span><status-tag value="Succeeded" /></li>
                  </template>
                  <li><span class="mono">workspace.create</span><status-tag :value="st === 'provisioning' ? 'Running' : st === 'failed' ? 'Failed' : 'Succeeded'" /></li>
                </ul>
              </panel>
            </div>
          </div>
        </template>

        <!-- ================= Resources ================= -->
        <template v-else-if="tab === 'resources'">
          <panel title="Quota of this Workspace" note="used by running Pods, out of what the Workspace reserved">
            <template #extra><el-tooltip content="Only owners and admins of Acme can resize." :disabled="!dev"><span><el-button :disabled="dev || st === 'degraded'">Resize</el-button></span></el-tooltip></template>
            <div class="t-meters">
              <quota-meter label="CPU requests" :used="1.8" :total="w.rcpu" unit="cores" />
              <quota-meter label="Memory requests" :used="3.5" :total="w.rmem" unit="Gi" />
              <quota-meter label="CPU limits" :used="2.5" :total="w.lcpu" unit="cores" />
              <quota-meter label="Memory limits" :used="5" :total="w.lmem" unit="Gi" />
              <quota-meter label="Pods" :used="10" :total="w.pods" />
              <quota-meter label="Storage" :used="12" :total="w.storage" unit="Gi" />
            </div>
          </panel>
          <el-alert type="warning" :closable="false" show-icon title="CPU requests are almost used up" description="New Pods that ask for more than 0.2 cores will not start. Resize the Workspace, or lower the requests of existing Pods." />
          <panel title="By namespace" flush>
            <div class="t-tablewrap">
              <el-table :data="namespaces" style="width: 100%">
                <el-table-column label="Namespace" min-width="220"><template #default="{ row }"><span class="mono">{{ row.name }}</span></template></el-table-column>
                <el-table-column label="Pods" width="100" align="right"><template #default="{ row }"><span class="num">{{ row.pods }}</span></template></el-table-column>
                <el-table-column label="CPU requests" width="140" align="right"><template #default="{ row }"><span class="num">{{ row.rcpu }}</span></template></el-table-column>
                <el-table-column label="Memory requests" width="160" align="right"><template #default="{ row }"><span class="num">{{ row.rmem }}</span></template></el-table-column>
              </el-table>
            </div>
          </panel>
        </template>

        <!-- ================= Compute ================= -->
        <template v-else-if="tab === 'compute'">
          <panel title="Where the Pods run">
            <div class="t-stack" style="gap: 20px">
              <key-values :items="[['Mode', w.mode === 'Shared' ? 'Shared nodes' : 'Dedicated node'], [w.mode === 'Shared' ? 'Pool' : 'Node', w.place], ['Reserved', w.rcpu + ' cores, ' + w.rmem + ' Gi'], ['Quality of service', w.mode === 'Shared' ? 'Burstable: limits may exceed requests' : 'Guaranteed: limits equal requests']]"><template #v1><code>{{ w.place }}</code> <status-tag value="Ready" /></template></key-values>
              <div class="t-meters two">
                <quota-meter :label="'CPU reserved on ' + w.place" :used="w.mode === 'Shared' ? 5.5 : 4" :total="w.mode === 'Shared' ? 12 : 7" unit="cores" />
                <quota-meter :label="'Memory reserved on ' + w.place" :used="w.mode === 'Shared' ? 11 : 8" :total="w.mode === 'Shared' ? 24 : 14" unit="Gi" />
              </div>
            </div>
          </panel>
          <p class="t-explain">Pods of this Workspace can only be scheduled on <code>{{ w.place }}</code>. A Pod that asks for another node is refused. The mode cannot be changed after creation.</p>
        </template>

        <!-- ================= Network ================= -->
        <template v-else-if="tab === 'network'">
          <panel title="Pod network">
            <div class="t-stack" style="gap: 20px">
              <key-values :items="[['Address block', w.cidr], ['Pool', 'onboarding-pod-network'], ['Isolation', 'Pods of other Workspaces cannot connect to these Pods']]"><template #v0><span class="mono">{{ w.cidr }}</span></template><template #v1><code>onboarding-pod-network</code></template></key-values>
              <div class="t-meters"><quota-meter label="Pod addresses in use" :used="10" :total="254" /></div>
            </div>
          </panel>
          <p class="t-explain">The address block cannot be changed while Pods are running. To put an app on the internet, create a <a :href="tabHref('routes')">Route</a>.</p>
        </template>

        <!-- ================= Namespaces ================= -->
        <template v-else-if="tab === 'namespaces'">
          <panel title="Namespaces" :note="namespaces.length + ' of 6'" flush>
            <template #extra><el-tooltip content="Only members with the TenantOwner role can create namespaces." :disabled="!dev"><span><el-button type="primary" :disabled="dev || !usable || st === 'cordoned'" @click="nsDialog = true"><el-icon><Plus /></el-icon>&nbsp;Create namespace</el-button></span></el-tooltip></template>
            <div class="t-tablewrap">
              <el-table :data="namespaces" style="width: 100%">
                <el-table-column label="Namespace" min-width="230"><template #default="{ row }"><span class="mono">{{ row.name }}</span></template></el-table-column>
                <el-table-column label="State" width="120"><template #default="{ row }"><status-tag :value="row.state" /></template></el-table-column>
                <el-table-column label="Created with" width="140" prop="source" />
                <el-table-column label="Pods" width="90" align="right"><template #default="{ row }"><span class="num">{{ row.pods }}</span></template></el-table-column>
                <el-table-column label="CPU req." width="110" align="right"><template #default="{ row }"><span class="num">{{ row.rcpu }}</span></template></el-table-column>
                <el-table-column label="Memory req." width="130" align="right"><template #default="{ row }"><span class="num">{{ row.rmem }}</span></template></el-table-column>
                <el-table-column width="100" align="right"><template #default><el-button link type="danger" :disabled="dev">Delete</el-button></template></el-table-column>
              </el-table>
            </div>
            <div class="t-panel-foot">Namespaces created with kubectl appear here too. Deleting a namespace deletes everything in it, including its Routes and volumes.</div>
          </panel>
          <el-dialog v-model="nsDialog" title="Create namespace" width="460px">
            <el-form label-position="top" @submit.prevent>
              <el-form-item label="Name" for="ns-name"><el-input id="ns-name" v-model="nsName" placeholder="staging"><template #prepend>acme-{{ w.slug }}-</template></el-input><div class="t-field-note">Lowercase letters, digits and hyphens. {{ 6 - namespaces.length }} of 6 namespaces left.</div></el-form-item>
            </el-form>
            <template #footer><el-button @click="nsDialog = false">Cancel</el-button><el-button type="primary" @click="nsDialog = false">Create namespace</el-button></template>
          </el-dialog>
        </template>

        <!-- ================= Routes ================= -->
        <template v-else-if="tab === 'routes'">
          <panel title="Routes" :note="routes.length + ' of 20'" flush>
            <template #extra><el-tooltip content="Only members with the TenantOwner role can create Routes." :disabled="!dev"><span><el-button type="primary" :disabled="dev || !usable || st === 'cordoned'" @click="routeDialog = true"><el-icon><Plus /></el-icon>&nbsp;Create Route</el-button></span></el-tooltip></template>
            <div class="t-tablewrap">
              <el-table :data="routes" style="width: 100%">
                <el-table-column label="Address" min-width="320"><template #default="{ row }"><a v-if="row.state === 'Active'" :href="'https://' + row.host" target="_blank" rel="noopener" class="mono">https://{{ row.host }}</a><span v-else class="mono muted">https://{{ row.host }}</span></template></el-table-column>
                <el-table-column label="State" width="130"><template #default="{ row }"><status-tag :value="row.state" /></template></el-table-column>
                <el-table-column label="Sends traffic to" min-width="230"><template #default="{ row }">Service <code>{{ row.service }}</code>:{{ row.port }} <span class="muted">in</span> {{ row.namespace }}</template></el-table-column>
                <el-table-column width="100" align="right"><template #default><el-button link type="danger" :disabled="dev">Delete</el-button></template></el-table-column>
              </el-table>
            </div>
            <div class="t-panel-foot">A Route puts one Service on the internet over HTTPS; the certificate is handled for you. Ingress objects created with kubectl are refused.</div>
          </panel>
          <el-dialog v-model="routeDialog" title="Create Route" width="540px">
            <el-form label-position="top" @submit.prevent>
              <el-form-item label="Namespace" for="rt-ns"><el-select id="rt-ns" v-model="route.ns" style="width: 100%"><el-option v-for="n in namespaces" :key="n.short" :value="n.short" :label="n.name" /></el-select></el-form-item>
              <el-form-item label="Route name" for="rt-name"><el-input id="rt-name" v-model="route.name" /><div class="t-field-note">The first part of the address. It cannot be changed later.</div></el-form-item>
              <div class="t-grid-form">
                <el-form-item label="Service" for="rt-svc"><el-input id="rt-svc" v-model="route.service" /></el-form-item>
                <el-form-item label="Port" for="rt-port"><el-input-number id="rt-port" v-model="route.port" :min="1" :max="65535" controls-position="right" /></el-form-item>
              </div>
              <div class="t-stack" style="gap: 6px"><span class="small muted">Your app will be at</span><copy-field :value="'https://' + (route.name || 'name') + '-acme-' + w.slug + '.mvp.tenantry.site'" /></div>
            </el-form>
            <template #footer><el-button @click="routeDialog = false">Cancel</el-button><el-button type="primary" @click="routeDialog = false">Create Route</el-button></template>
          </el-dialog>
        </template>

        <!-- ================= Access roles ================= -->
        <template v-else-if="tab === 'members'">
          <panel title="Who can use this Workspace" note="roles are given to groups, not to people" flush>
            <template #extra><el-button type="primary" :disabled="dev" @click="bindDialog = true"><el-icon><Plus /></el-icon>&nbsp;Add group</el-button></template>
            <div class="t-tablewrap">
              <el-table :data="bindings" style="width: 100%">
                <el-table-column label="Group" min-width="170"><template #default="{ row }"><a href="#/orgs/acme/groups"><strong>{{ row.group }}</strong></a></template></el-table-column>
                <el-table-column label="Role" width="200"><template #default="{ row }"><el-select :model-value="row.role" size="small" style="width: 160px" :disabled="dev" :aria-label="'Role of group ' + row.group"><el-option v-for="x in ['TenantOwner', 'Developer', 'Viewer']" :key="x" :value="x" :label="x" /></el-select></template></el-table-column>
                <el-table-column label="Members" width="110" align="right"><template #default="{ row }"><span class="num">{{ row.members }}</span></template></el-table-column>
                <el-table-column label="Sync" width="150"><template #default="{ row }"><status-tag :value="row.sync" /></template></el-table-column>
                <el-table-column width="110" align="right">
                  <template #default="{ row }">
                    <el-tooltip v-if="row.role === 'TenantOwner'" content="A Workspace needs at least one owner group. Add another owner group first."><span><el-button link disabled>Remove</el-button></span></el-tooltip>
                    <el-button v-else link type="danger" :disabled="dev">Remove</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>
            <div class="t-panel-foot">After a group is removed, its members lose Kubernetes access within 5 minutes and must log in again.</div>
          </panel>
          <panel title="What each role can do">
            <key-values :items="[['TenantOwner', 'Create and delete namespaces and Routes, manage workloads and access inside the Workspace.'], ['Developer', 'Deploy and change workloads in existing namespaces.'], ['Viewer', 'Read only.']]"></key-values>
          </panel>
          <el-dialog v-model="bindDialog" title="Add a group to this Workspace" width="460px">
            <el-form label-position="top" @submit.prevent>
              <el-form-item label="Group" for="bd-g"><el-select id="bd-g" model-value="" placeholder="Choose a group" style="width: 100%"><el-option value="backend" label="backend  (sync pending)" disabled /></el-select><div class="t-field-note">All synced groups already have a role here. <a href="#/orgs/acme/groups">Create a group</a></div></el-form-item>
              <el-form-item label="Role" for="bd-r"><el-radio-group id="bd-r" model-value="Developer"><el-radio value="TenantOwner">TenantOwner</el-radio><el-radio value="Developer">Developer</el-radio><el-radio value="Viewer">Viewer</el-radio></el-radio-group></el-form-item>
            </el-form>
            <template #footer><el-button @click="bindDialog = false">Cancel</el-button><el-button type="primary" disabled>Add group</el-button></template>
          </el-dialog>
        </template>

        <!-- ================= Connect ================= -->
        <template v-else-if="tab === 'access'">
          <el-alert v-if="!usable" type="info" :closable="false" show-icon :title="whyNot" />
          <div class="t-cols">
            <panel title="Web console">
              <div class="t-stack">
                <p class="muted">Browse and edit workloads in the browser. You are logged in automatically with your Tenantry account.</p>
                <div><el-button type="primary" :disabled="!usable"><el-icon><Monitor /></el-icon>&nbsp;Open web console</el-button></div>
                <span class="mono muted">{{ cluster.console }}</span>
              </div>
            </panel>
            <panel title="Your access">
              <key-values :items="[['Role here', dev ? 'Developer' : 'TenantOwner'], ['Through group', dev ? 'developers' : 'admins'], ['Session', 'renewed every 5 minutes while you are logged in']]"></key-values>
            </panel>
          </div>
          <panel title="kubectl">
            <div class="t-stack">
              <p><strong>1. Install the login plugin once.</strong> <span class="muted">The kubeconfig contains no password or token; kubectl opens your browser to log in.</span></p>
              <pre class="t-term"><i># macOS or Linux, with Homebrew</i>
<b>brew install</b> kubelogin
<i># or with krew</i>
<b>kubectl krew install</b> oidc-login</pre>
              <p><strong>2. Download the kubeconfig</strong> for this Workspace.</p>
              <div><el-button :disabled="!usable"><el-icon><Download /></el-icon>&nbsp;Download kubeconfig</el-button></div>
              <p><strong>3. Use it.</strong></p>
              <pre class="t-term"><b>export</b> KUBECONFIG=~/Downloads/acme-{{ w.slug }}.kubeconfig
<b>kubectl</b> get namespaces
<i># a browser window opens once; afterwards:</i>
NAME                  STATUS   AGE
acme-{{ w.slug }}-frontend   Active   3d
acme-{{ w.slug }}-backend    Active   3d</pre>
              <p class="t-field-note">You only see namespaces of Workspaces you have a role in. Server: <span class="mono">{{ cluster.proxy }}</span></p>
            </div>
          </panel>
        </template>

        <!-- ================= Danger zone ================= -->
        <template v-else>
          <panel title="Cordon: make the Workspace read-only">
            <div class="t-row t-between">
              <p class="muted" style="max-width: 62ch">Running Pods keep running, but nobody can change anything with kubectl or the web console until you uncordon. Use it during an incident or an audit.</p>
              <el-button :disabled="dev || !usable">{{ st === 'cordoned' ? 'Uncordon' : 'Cordon Workspace' }}</el-button>
            </div>
          </panel>
          <panel title="Delete this Workspace" danger>
            <div class="t-stack">
              <el-alert type="error" :closable="false" show-icon title="Deleting cannot be undone">
                <div>This removes right away, with no grace period:</div>
                <div><strong>{{ w.namespaces }} namespaces</strong> with all their workloads, <strong>2 Routes</strong>, and <strong>3 volumes holding 12 Gi of data</strong>.</div>
                <div>The quota goes back to Acme's grant. The address block {{ w.cidr }} is kept unused for a while before it is given out again.</div>
              </el-alert>
              <confirm-by-name :name="'acme-' + w.slug" action="Delete Workspace" :disabled="dev || st === 'blocked'" />
              <p v-if="dev" class="t-field-note">Only owners and admins of Acme can delete a Workspace.</p>
            </div>
          </panel>
        </template>
      </app-shell>`,
  };
})();
