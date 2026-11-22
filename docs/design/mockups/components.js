// Shared building blocks. Every screen is assembled from these plus Element
// Plus controls; the real web/ keeps the same names (IMPLEMENTATION-PLAN §6.2).
(function () {
  const { reactive, computed, ref } = Vue;

  // ---- tiny hash router: #/path?key=value ----
  const route = reactive({ path: '/', query: {} });
  function parse() {
    const raw = (location.hash || '#/').slice(1);
    const [path, qs] = raw.split('?');
    route.path = path || '/';
    route.query = Object.fromEntries(new URLSearchParams(qs || ''));
    window.scrollTo(0, 0);
  }
  window.addEventListener('hashchange', parse);
  parse();
  T.route = route;
  T.href = (path, query) => {
    const qs = query ? new URLSearchParams(query).toString() : '';
    return '#' + path + (qs ? '?' + qs : '');
  };
  T.go = (path, query) => { location.hash = T.href(path, query); };

  // One vocabulary for every state shown in the product: tone + words.
  const STATUS = {
    // Workspace
    Draft: 'idle', Validating: 'busy', Reserving: 'busy', Provisioning: 'busy', Updating: 'busy', Deleting: 'busy',
    Ready: 'ok', Cordoned: 'warn', Degraded: 'warn', Failed: 'bad', Deleted: 'idle',
    // Operation
    Pending: 'warn', Running: 'busy', Succeeded: 'ok', Blocked: 'bad',
    // Organization, records
    Active: 'ok', Rejected: 'bad', Creating: 'busy', Desired: 'idle',
    // Sync, nodes, capacity, cluster
    Synced: 'ok', Error: 'bad', NotReady: 'bad', Maintenance: 'warn', Unavailable: 'bad', Registered: 'idle',
    Available: 'ok', PartiallyAllocated: 'info', FullyAllocated: 'warn', Overcommitted: 'bad',
    Accepted: 'ok', Expired: 'idle',
  };
  const WORDS = { PartiallyAllocated: 'Partially allocated', FullyAllocated: 'Fully allocated', NotReady: 'Not ready' };

  const components = {
    // <status-tag value="Ready" /> — state as a pill; busy states spin.
    StatusTag: {
      props: { value: String, label: String, tone: String },
      computed: {
        cls() { const t = this.tone || STATUS[this.value] || 'idle'; return t === 'busy' ? 'info busy' : t; },
        text() { return this.label || WORDS[this.value] || this.value; },
      },
      template: `<span class="t-status" :class="cls">{{ text }}</span>`,
    },

    // <quota-meter label used total unit pending /> — one number with its bar.
    QuotaMeter: {
      props: { label: String, used: { type: Number, default: 0 }, total: { type: Number, default: 0 }, unit: { type: String, default: '' }, pending: { type: Number, default: 0 } },
      computed: {
        left() { return Math.round((this.total - this.used - this.pending) * 100) / 100; },
        pct() { return this.total ? Math.min(100, (this.used / this.total) * 100) : 0; },
        pctPending() { return this.total ? Math.min(100 - this.pct, (this.pending / this.total) * 100) : 0; },
        tone() { const p = this.total ? (this.used + this.pending) / this.total : 0; return p > 1 ? 'bad' : p >= 0.85 ? 'warn' : ''; },
        leftText() { return this.left < 0 ? 'over by ' + (-this.left) : this.left + (this.unit === 'Gi' ? ' Gi' : '') + ' left'; },
      },
      template: `
        <div class="t-meter">
          <div class="t-meter-top"><span>{{ label }}</span><span class="t-meter-left" :class="tone">{{ leftText }}</span></div>
          <div class="t-meter-val">{{ used }}{{ pending ? ' + ' + pending : '' }} <span>of {{ total }}{{ unit ? ' ' + unit : '' }}</span></div>
          <div class="t-track" role="img" :aria-label="label + ': ' + used + ' of ' + total + ' ' + unit + ' reserved'">
            <i :class="tone" :style="{ width: pct + '%' }"></i><i class="pending" :style="{ width: pctPending + '%' }"></i>
          </div>
        </div>`,
    },

    // <page-header title crumbs description> with #tags and #actions.
    PageHeader: {
      props: { title: String, crumbs: { type: Array, default: () => [] }, description: String },
      template: `
        <header class="t-head">
          <div class="t-head-main">
            <nav v-if="crumbs.length" class="t-crumbs" aria-label="Breadcrumb">
              <template v-for="(c, i) in crumbs" :key="i">
                <a v-if="c.to" :href="c.to">{{ c.label }}</a><span v-else>{{ c.label }}</span>
                <span v-if="i < crumbs.length - 1" aria-hidden="true">/</span>
              </template>
            </nav>
            <div class="t-head-title"><h1>{{ title }}</h1><slot name="tags"></slot></div>
            <p v-if="description" class="t-head-desc">{{ description }}</p>
          </div>
          <div class="t-head-actions"><slot name="actions"></slot></div>
        </header>`,
    },

    // <panel title note> — a titled block of content; #extra sits right of the title.
    Panel: {
      props: { title: String, note: String, flush: Boolean, danger: Boolean, foot: String },
      template: `
        <section class="t-panel" :class="{ 't-danger': danger }">
          <div v-if="title || $slots.extra" class="t-panel-head">
            <div class="t-row"><h2>{{ title }}</h2><span v-if="note" class="muted">{{ note }}</span></div>
            <div class="t-row"><slot name="extra"></slot></div>
          </div>
          <div class="t-panel-body" :class="{ flush }"><slot></slot></div>
          <div v-if="foot" class="t-panel-foot">{{ foot }}</div>
        </section>`,
    },

    // <condition-timeline :conditions> — where a Workspace is, by Kubernetes condition.
    ConditionTimeline: {
      props: { conditions: Array },
      methods: {
        tone(c) { return c.status === 'True' ? 'ok' : c.status === 'False' ? 'bad' : c.status === 'Running' ? 'run' : 'wait'; },
        icon(c) { return c.status === 'True' ? 'CircleCheckFilled' : c.status === 'False' ? 'CircleCloseFilled' : c.status === 'Running' ? 'Loading' : 'Clock'; },
        word(c) { return c.status === 'True' ? 'done' : c.status === 'False' ? 'failed' : c.status === 'Running' ? 'in progress' : 'waiting'; },
      },
      template: `
        <ol class="t-conds">
          <li v-for="c in conditions" :key="c.type" class="t-cond">
            <el-icon class="t-cond-ic" :class="[tone(c), { 'is-loading': c.status === 'Running' }]" :aria-label="word(c)"><component :is="icon(c)" /></el-icon>
            <span class="t-cond-name">{{ c.type }}<template v-if="c.status === 'False' || c.status === 'Running'"> = {{ c.status === 'False' ? 'False' : 'Unknown' }}<span v-if="c.reason" class="muted"> ({{ c.reason }})</span></template></span>
            <span class="t-cond-when">{{ c.when }}</span>
            <span class="t-cond-msg">{{ c.message }}</span>
          </li>
        </ol>`,
    },

    // <empty-state title description> with #actions — says what will be here and the next step.
    EmptyState: {
      props: { title: String, description: String, center: Boolean },
      template: `
        <div class="t-empty" :class="{ center }">
          <h2>{{ title }}</h2>
          <p v-if="description">{{ description }}</p>
          <div v-if="$slots.actions" class="t-empty-actions"><slot name="actions"></slot></div>
        </div>`,
    },

    // <error-state code message request-id> — failed load: what, the stable code, retry.
    ErrorState: {
      props: { title: { type: String, default: 'This could not be loaded' }, code: String, message: String, requestId: String },
      template: `
        <div class="t-empty">
          <h2>{{ title }}</h2>
          <p>{{ message }}</p>
          <p class="t-errcode">{{ code }}<template v-if="requestId"> · request {{ requestId }}</template></p>
          <div class="t-empty-actions"><el-button type="primary" plain>Try again</el-button></div>
        </div>`,
    },

    // <confirm-by-name name action> — destructive actions unlock after typing the name.
    ConfirmByName: {
      props: { name: String, action: String, disabled: Boolean },
      data: () => ({ typed: '' }),
      computed: { id() { return 'confirm-' + this.name; } },
      template: `
        <div class="t-stack" style="gap: 10px; max-width: 420px">
          <label :for="id" class="small">Type <code>{{ name }}</code> to confirm</label>
          <el-input :id="id" v-model="typed" :disabled="disabled" autocomplete="off" />
          <div><el-button type="danger" :disabled="disabled || typed !== name">{{ action }}</el-button></div>
        </div>`,
    },

    // <copy-field value /> — a value people paste somewhere else.
    CopyField: {
      props: { value: String, label: { type: String, default: 'Copy' } },
      data: () => ({ done: false }),
      methods: { copy() { this.done = true; setTimeout(() => { this.done = false; }, 1500); } },
      template: `
        <div class="t-copy">
          <span>{{ value }}</span>
          <el-button size="small" @click="copy">{{ done ? 'Copied' : label }}</el-button>
        </div>`,
    },

    // <num-field v-model="n" unit="cores" /> — a number with its unit inside the box, no stepper arrows.
    NumField: {
      props: { modelValue: Number, unit: String, min: { type: Number, default: 0 }, max: Number, step: { type: Number, default: 1 }, disabled: Boolean, id: String, label: String },
      emits: ['update:modelValue'],
      template: `
        <el-input-number class="t-num" :id="id" :aria-label="label" :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" :min="min" :max="max" :step="step" :disabled="disabled" :controls="false">
          <template v-if="unit" #suffix>{{ unit }}</template>
        </el-input-number>`,
    },

    // <resource-grid :value="{ rcpu, lcpu, rmem, lmem }" :notes :errors lock-limits /> — CPU and memory, requests beside limits.
    ResourceGrid: {
      props: { value: Object, notes: { type: Object, default: () => ({}) }, errors: { type: Object, default: () => ({}) }, lockLimits: Boolean, idp: { type: String, default: 'r' } },
      data: () => ({ rows: [['CPU', 'cores', 0.5, 'rcpu', 'lcpu'], ['Memory', 'Gi', 1, 'rmem', 'lmem']] }),
      methods: {
        locked(k) { return this.lockLimits && k[0] === 'l'; },
        val(k) { return this.locked(k) ? this.value['r' + k.slice(1)] : this.value[k]; },
      },
      template: `
        <div class="t-res" role="group" aria-label="CPU and memory">
          <span></span>
          <div class="t-res-head"><strong>Requests</strong>guaranteed to Pods</div>
          <div class="t-res-head"><strong>Limits</strong>the most Pods may use</div>
          <template v-for="r in rows" :key="r[0]">
            <div class="t-res-label">{{ r[0] }}</div>
            <div v-for="k in [r[3], r[4]]" :key="k">
              <num-field :id="idp + '-' + k" :label="r[0] + (k[0] === 'r' ? ' requests' : ' limits')" :model-value="val(k)" @update:model-value="value[k] = $event" :unit="r[1]" :step="r[2]" :disabled="locked(k)" />
              <div v-if="errors[k]" class="t-field-note t-field-err">{{ errors[k] }}</div>
              <div v-else-if="locked(k)" class="t-field-note">Same as requests on a dedicated node.</div>
              <div v-else-if="notes[k]" class="t-field-note num">{{ notes[k] }}</div>
            </div>
          </template>
        </div>`,
    },

    // <key-values :items="[[label, value], …]" /> — facts about one object.
    KeyValues: {
      props: { items: Array, compact: Boolean },
      template: `
        <dl class="t-kv" :class="{ compact }">
          <template v-for="(it, i) in items" :key="i"><dt>{{ it[0] }}</dt><dd><slot :name="'v' + i" :value="it[1]">{{ it[1] }}</slot></dd></template>
        </dl>`,
    },

    // <app-shell area="org|admin|bare" active="overview"> — top bar, menu, content.
    AppShell: {
      props: { area: { type: String, default: 'org' }, active: String, narrow: Boolean, limited: Boolean },
      data: () => ({ org: T.org }),
      computed: {
        who() {
          if (this.area === 'admin') return T.admin;
          const q = T.route.query;
          if (T.route.path.startsWith('/invitations')) return q.state === 'mismatch' ? T.mallory : T.joe;
          return q.as === 'admin' ? T.joe : q.as === 'member' ? T.bob : T.me;
        },
        base() { return '#/orgs/' + T.org.slug; },
        orgNav() {
          const all = [
            ['overview', 'Overview', '', 'Odometer'],
            ['workspaces', 'Workspaces', '/workspaces', 'Box'],
            ['members', 'Members', '/members', 'User'],
            ['groups', 'Groups', '/groups', 'Collection'],
            ['clusters', 'Clusters', '/clusters', 'Cpu'],
            ['audit', 'Audit', '/audit', 'Tickets'],
            ['settings', 'Settings', '/settings', 'Setting'],
          ];
          return this.limited ? all.filter((n) => ['overview', 'workspaces', 'settings'].includes(n[0])) : all;
        },
        adminNav() {
          return [
            ['organizations', 'Organizations', '/admin/organizations', 'OfficeBuilding', T.requests.length],
            ['clusters', 'Clusters', '/admin/clusters', 'Cpu'],
            ['policy', 'Onboarding policy', '/admin/onboarding-policy', 'DocumentChecked'],
            ['grants', 'Grants', '/admin/grants', 'Coin'],
            ['operations', 'Operations', '/admin/operations', 'Operation'],
            ['health', 'Platform health', '/admin/health', 'Monitor'],
          ];
        },
      },
      template: `
        <div class="t-shell">
          <header class="t-top">
            <a class="t-brand" :href="area === 'admin' ? '#/admin/organizations' : base"><span class="t-brand-mark" aria-hidden="true"><i></i><i></i><i></i><i></i></span>Tenantry</a>
            <template v-if="area === 'org'">
              <span class="t-top-sep" aria-hidden="true"></span>
              <el-dropdown trigger="click">
                <button class="t-switch" type="button" :aria-label="'Switch Organization, current: ' + org.name">{{ org.name }} <el-icon><ArrowDown /></el-icon></button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item><strong>{{ org.name }}</strong>&nbsp;<span class="muted">OrgOwner</span></el-dropdown-item>
                    <el-dropdown-item>Umbrella&nbsp;<span class="muted">OrgMember</span></el-dropdown-item>
                    <el-dropdown-item divided><a href="#/welcome">Request another Organization</a></el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </template>
            <el-tag v-if="area === 'admin'" type="primary" effect="plain" round>Platform Admin</el-tag>
            <span class="t-top-spacer"></span>
            <a v-if="area === 'admin'" :href="base" class="small">Back to portal</a>
            <el-dropdown trigger="click">
              <button class="t-user" type="button" aria-label="Account menu"><span class="t-avatar" aria-hidden="true">{{ who.initials }}</span><span class="t-user-name">{{ who.email }}</span><el-icon><ArrowDown /></el-icon></button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item disabled>{{ who.name }}</el-dropdown-item>
                  <el-dropdown-item v-if="area !== 'admin'"><a href="#/admin/organizations">Platform Admin area</a></el-dropdown-item>
                  <el-dropdown-item divided>Log out everywhere</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </header>

          <div v-if="area === 'bare'" class="t-bare"><div class="t-page" :class="{ narrow }"><slot></slot></div></div>
          <div v-else class="t-body">
            <aside class="t-side"><div class="t-side-in">
              <div class="t-side-label">{{ area === 'admin' ? 'Platform' : 'Organization' }}</div>
              <nav class="t-nav" :aria-label="area === 'admin' ? 'Platform Admin' : 'Organization'">
                <template v-if="area === 'org'">
                  <a v-for="n in orgNav" :key="n[0]" :href="base + n[2]" :aria-current="active === n[0] ? 'page' : null"><el-icon><component :is="n[3]" /></el-icon>{{ n[1] }}</a>
                </template>
                <template v-else>
                  <a v-for="n in adminNav" :key="n[0]" :href="'#' + n[2]" :aria-current="active === n[0] ? 'page' : null"><el-icon><component :is="n[3]" /></el-icon>{{ n[1] }}<span v-if="n[4]" class="t-nav-count" :aria-label="n[4] + ' waiting'">{{ n[4] }}</span></a>
                </template>
              </nav>
            </div></aside>
            <main class="t-main"><div class="t-page" :class="{ narrow }"><slot></slot></div></main>
          </div>
        </div>`,
    },
  };

  T.components = components;
  T.STATUS = STATUS;
})();
