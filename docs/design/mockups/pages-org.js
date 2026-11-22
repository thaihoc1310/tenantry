// Organization-level screens: /orgs/:org and its menu entries.
(function () {
  T.pages = T.pages || {};
  const base = '#/orgs/acme';

  // /orgs/acme?state=fresh|maintenance|full
  T.pages.Overview = {
    data: () => ({ r: T.route, org: T.org, cluster: T.cluster, ent: T.entitlement, base }),
    computed: {
      state() { return this.r.query.state || 'normal'; },
      fresh() { return this.state === 'fresh'; },
      grant() {
        if (this.fresh) return T.freshGrant;
        if (this.state === 'full') return T.grant.map((g) => (g.key === 'workspaces' ? { ...g, used: 4 } : g.key === 'rcpu' ? { ...g, used: g.total } : g));
        return T.grant;
      },
      workspaces() { return this.fresh ? [] : T.workspaces; },
      canCreate() { return this.state === 'normal' || this.fresh; },
      offered() { return this.r.query.transfer === 'pending'; },
      why() {
        if (this.state === 'maintenance') return 'Cluster A is in maintenance. Existing Workspaces keep running; creating and resizing are paused.';
        if (this.state === 'full') return 'This Organization has used all of its Workspaces and CPU on Cluster A.';
        return '';
      },
    },
    template: `
      <app-shell active="overview">
        <page-header :title="org.name">
          <template #tags><code>{{ org.slug }}</code><status-tag value="Active" /><el-tag type="info" effect="plain" round>{{ offered ? 'You are OrgAdmin' : 'You are OrgOwner' }}</el-tag></template>
          <template #actions>
            <el-tooltip :content="why" :disabled="canCreate" placement="bottom-end">
              <span><el-button type="primary" :disabled="!canCreate" @click="$root.go('/orgs/acme/workspaces/new')"><el-icon><Plus /></el-icon>&nbsp;Create Workspace</el-button></span>
            </el-tooltip>
          </template>
        </page-header>

        <el-alert v-if="offered" type="warning" :closable="false" show-icon title="Alice Nguyen offers you ownership of Acme" style="margin-bottom: 16px">
          <div>The offer expires in 6 days. Until you accept, Alice stays the owner.</div>
          <div style="margin-top: 8px"><el-button size="small" type="primary" @click="$root.go('/orgs/acme/settings', { as: 'admin', transfer: 'pending' })">Review in Settings</el-button></div>
        </el-alert>

        <el-alert v-if="fresh" type="success" show-icon title="Your Organization was approved" description="You start with a small shared quota on Cluster A: 2 Workspaces, 2 CPU cores and 4 Gi of memory. Create your first Workspace to use it." />
        <el-alert v-if="state === 'maintenance'" type="warning" :closable="false" show-icon title="Cluster A is in maintenance" description="Existing Workspaces keep running and stay reachable. Creating and resizing are paused until maintenance ends." />
        <el-alert v-if="state === 'full'" type="warning" :closable="false" show-icon title="No quota left for a new Workspace" description="Free quota by deleting or shrinking a Workspace, or ask a Platform Admin for a larger grant." />

        <panel title="Quota on Cluster A" note="reserved by your Workspaces">
          <template #extra><status-tag :value="state === 'maintenance' ? 'Maintenance' : 'Ready'" :label="state === 'maintenance' ? 'Cluster in maintenance' : 'Cluster ready'" /><a :href="base + '/clusters'" class="small">Grant details</a></template>
          <div class="t-meters">
            <quota-meter v-for="g in grant" :key="g.key" :label="g.label" :used="g.used" :total="g.total" :unit="g.unit" />
          </div>
        </panel>

        <panel v-if="workspaces.length" title="Workspaces" flush>
          <template #extra><a :href="base + '/workspaces'" class="small">All Workspaces</a></template>
          <div class="t-tablewrap">
            <el-table :data="workspaces" style="width: 100%">
              <el-table-column label="Workspace" min-width="150">
                <template #default="{ row }"><a :href="base + '/workspaces/' + row.slug + (row.state === 'Provisioning' ? '?state=provisioning' : '')"><strong>{{ row.name }}</strong></a><div class="muted mono">acme-{{ row.slug }}</div></template>
              </el-table-column>
              <el-table-column label="State" width="150"><template #default="{ row }"><status-tag :value="row.state" /></template></el-table-column>
              <el-table-column label="Compute" min-width="230"><template #default="{ row }">{{ row.mode }} <span class="muted">on</span> <code>{{ row.place }}</code></template></el-table-column>
              <el-table-column label="CPU req." width="100" align="right"><template #default="{ row }"><span class="num">{{ row.rcpu }}</span></template></el-table-column>
              <el-table-column label="Memory req." width="120" align="right"><template #default="{ row }"><span class="num">{{ row.rmem }} Gi</span></template></el-table-column>
              <el-table-column label="Namespaces" width="120" align="right"><template #default="{ row }"><span class="num">{{ row.namespaces }}</span></template></el-table-column>
            </el-table>
          </div>
        </panel>
        <panel v-else>
          <empty-state title="No Workspaces yet" description="A Workspace is an isolated part of the cluster with its own quota, network and members. Creating one takes about a minute and gives you a kubeconfig and a web console link.">
            <template #actions><el-button type="primary" @click="$root.go('/orgs/acme/workspaces/new')">Create your first Workspace</el-button></template>
          </empty-state>
        </panel>

        <div class="t-cols">
          <panel title="Organization limits">
            <key-values :items="[['Members', (fresh ? 1 : ent.members[0]) + ' of ' + ent.members[1]], ['Groups', (fresh ? 3 : ent.groups[0]) + ' of ' + ent.groups[1]], ['Workspaces, all clusters', (fresh ? 0 : ent.workspaces[0]) + ' of ' + ent.workspaces[1]]]"></key-values>
          </panel>
          <panel title="People">
            <div class="t-stack" style="gap: 10px">
              <p class="muted">Invite teammates and put them in groups; groups get roles in each Workspace.</p>
              <div class="t-row"><el-button @click="$root.go('/orgs/acme/members')">Invite a member</el-button><el-button @click="$root.go('/orgs/acme/groups')">Manage groups</el-button></div>
            </div>
          </panel>
        </div>
      </app-shell>`,
  };

  // /orgs/acme/workspaces?state=empty  &as=member
  T.pages.Workspaces = {
    data: () => ({ r: T.route, base, q: '' }),
    computed: {
      member() { return this.r.query.as === 'member'; },
      rows() {
        if (this.r.query.state === 'empty') return [];
        const all = this.member ? T.workspaces.filter((w) => w.slug === 'solar') : T.workspaces;
        return all.filter((w) => w.name.toLowerCase().includes(this.q.toLowerCase()));
      },
    },
    template: `
      <app-shell active="workspaces" :limited="member">
        <page-header title="Workspaces" :description="member ? 'Workspaces your groups can reach.' : ''">
          <template #actions><el-button v-if="!member" type="primary" @click="$root.go('/orgs/acme/workspaces/new')"><el-icon><Plus /></el-icon>&nbsp;Create Workspace</el-button></template>
        </page-header>

        <panel v-if="rows.length || q" flush>
          <div style="padding: 12px 20px; border-bottom: 1px solid var(--t-line-soft)">
            <el-input v-model="q" placeholder="Filter by name" clearable style="max-width: 280px" aria-label="Filter Workspaces by name"><template #prefix><el-icon><Search /></el-icon></template></el-input>
          </div>
          <div class="t-tablewrap">
            <el-table :data="rows" style="width: 100%" empty-text="No Workspace matches this filter.">
              <el-table-column label="Workspace" min-width="160">
                <template #default="{ row }"><a :href="base + '/workspaces/' + row.slug + (row.state === 'Provisioning' ? '?state=provisioning' : '')"><strong>{{ row.name }}</strong></a><div class="muted mono">acme-{{ row.slug }}</div></template>
              </el-table-column>
              <el-table-column label="State" width="150"><template #default="{ row }"><status-tag :value="row.state" /></template></el-table-column>
              <el-table-column label="Compute" min-width="230"><template #default="{ row }">{{ row.mode }} <span class="muted">on</span> <code>{{ row.place }}</code></template></el-table-column>
              <el-table-column label="Pod network" width="150"><template #default="{ row }"><span class="mono">{{ row.cidr }}</span></template></el-table-column>
              <el-table-column label="CPU req. / limit" width="140" align="right"><template #default="{ row }"><span class="num">{{ row.rcpu }} / {{ row.lcpu }}</span></template></el-table-column>
              <el-table-column label="Memory req. / limit" width="160" align="right"><template #default="{ row }"><span class="num">{{ row.rmem }} / {{ row.lmem }} Gi</span></template></el-table-column>
              <el-table-column label="Created" width="120"><template #default="{ row }"><span class="num">{{ row.created }}</span></template></el-table-column>
            </el-table>
          </div>
        </panel>
        <panel v-else>
          <empty-state v-if="member" title="No Workspace is shared with you yet" description="You see a Workspace once one of your groups gets a role in it. Ask an owner or admin of Acme."></empty-state>
          <empty-state v-else title="No Workspaces yet" description="A Workspace is an isolated part of the cluster with its own quota, network and members.">
            <template #actions><el-button type="primary" @click="$root.go('/orgs/acme/workspaces/new')">Create your first Workspace</el-button></template>
          </empty-state>
        </panel>
      </app-shell>`,
  };

  // /orgs/acme/members
  T.pages.Members = {
    data: () => ({ members: T.members, invitations: T.invitations, ent: T.entitlement, dialog: !!T.route.query.dialog, created: T.route.query.dialog === 'link', form: { email: T.route.query.dialog ? 'carol@example.com' : '', role: 'OrgMember', group: 'developers' } }),
    methods: { open() { this.created = false; this.form.email = ''; this.dialog = true; } },
    template: `
      <app-shell active="members">
        <page-header title="Members" :description="members.length + ' of ' + ent.members[1] + ' members. What a member can do inside a Workspace comes from their groups.'">
          <template #actions><el-button type="primary" @click="open"><el-icon><Plus /></el-icon>&nbsp;Invite member</el-button></template>
        </page-header>

        <panel flush>
          <div class="t-tablewrap">
            <el-table :data="members" style="width: 100%">
              <el-table-column label="Member" min-width="220"><template #default="{ row }"><strong>{{ row.name }}</strong> <el-tag v-if="row.you" size="small" type="info" effect="plain" round>You</el-tag><div class="muted">{{ row.email }}</div></template></el-table-column>
              <el-table-column label="Organization role" width="200">
                <template #default="{ row }">
                  <span v-if="row.role === 'OrgOwner'">OrgOwner</span>
                  <el-select v-else :model-value="row.role" size="small" style="width: 150px" :aria-label="'Role of ' + row.name"><el-option value="OrgAdmin" label="OrgAdmin" /><el-option value="OrgMember" label="OrgMember" /></el-select>
                </template>
              </el-table-column>
              <el-table-column label="Groups" min-width="200"><template #default="{ row }"><div class="t-row" style="gap: 6px"><el-tag v-for="g in row.groups" :key="g" type="info" effect="plain">{{ g }}</el-tag></div></template></el-table-column>
              <el-table-column label="Joined" width="130"><template #default="{ row }"><span class="num">{{ row.joined }}</span></template></el-table-column>
              <el-table-column width="110" align="right">
                <template #default="{ row }">
                  <el-tooltip v-if="row.role === 'OrgOwner'" content="The owner cannot be removed. Transfer ownership first, in Settings."><span><el-button link disabled>Remove</el-button></span></el-tooltip>
                  <el-button v-else link type="danger">Remove</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </panel>

        <panel title="Open invitations" flush>
          <div class="t-tablewrap">
            <el-table :data="invitations" style="width: 100%" empty-text="No open invitations.">
              <el-table-column label="Email" min-width="220" prop="email" />
              <el-table-column label="Role" width="140" prop="role" />
              <el-table-column label="Group" width="150"><template #default="{ row }"><el-tag type="info" effect="plain">{{ row.group }}</el-tag></template></el-table-column>
              <el-table-column label="Invited by" min-width="150" prop="by" />
              <el-table-column label="Expires" width="120" prop="expires" />
              <el-table-column width="110" align="right"><template #default><el-button link type="danger">Revoke</el-button></template></el-table-column>
            </el-table>
          </div>
          <div class="t-panel-foot">The link is shown once, when the invitation is created. To send it again, revoke and invite again.</div>
        </panel>

        <el-dialog v-model="dialog" :title="created ? 'Send this link to ' + (form.email || 'carol@example.com') : 'Invite a member'" width="520px">
          <template v-if="!created">
            <el-form label-position="top" @submit.prevent>
              <el-form-item label="Email address" for="inv-email"><el-input id="inv-email" v-model="form.email" placeholder="name@company.com" /><div class="t-field-note">They must log in with exactly this address, and it must be verified.</div></el-form-item>
              <el-form-item label="Organization role" for="inv-role">
                <el-radio-group id="inv-role" v-model="form.role"><el-radio value="OrgMember">OrgMember</el-radio><el-radio value="OrgAdmin">OrgAdmin</el-radio></el-radio-group>
                <div class="t-field-note">OrgAdmin can manage members, groups and Workspaces. OrgMember only sees what their groups can reach.</div>
              </el-form-item>
              <el-form-item label="Add to group (optional)" for="inv-group"><el-select id="inv-group" v-model="form.group" clearable style="width: 100%"><el-option v-for="g in ['admins', 'developers', 'viewers', 'backend']" :key="g" :value="g" :label="g" /></el-select></el-form-item>
            </el-form>
          </template>
          <template v-else>
            <div class="t-stack">
              <copy-field value="https://portal.tenantry.site/invitations/3fK9x…Qm2" label="Copy link" />
              <el-alert type="warning" :closable="false" show-icon title="Copy the link now" description="Tenantry does not send email. This link is shown only once, works once, and expires in 7 days." />
            </div>
          </template>
          <template #footer>
            <template v-if="!created"><el-button @click="dialog = false">Cancel</el-button><el-button type="primary" @click="created = true">Create invitation link</el-button></template>
            <el-button v-else type="primary" @click="dialog = false">Done</el-button>
          </template>
        </el-dialog>
      </app-shell>`,
  };

  // /orgs/acme/groups
  T.pages.Groups = {
    data: () => ({ groups: T.groups, ent: T.entitlement, dialog: !!T.route.query.dialog, name: '' }),
    template: `
      <app-shell active="groups">
        <page-header title="Groups" :description="groups.length + ' of ' + ent.groups[1] + ' groups. A group gets a role in a Workspace; its members get that role in Kubernetes.'">
          <template #actions><el-button type="primary" @click="dialog = true"><el-icon><Plus /></el-icon>&nbsp;Create group</el-button></template>
        </page-header>

        <el-alert type="info" :closable="false" show-icon title="Changes reach Kubernetes within 5 minutes" description="People gain or lose a group the next time kubectl or the web console refreshes their token. Nobody has to log in again." />

        <panel flush>
          <div class="t-tablewrap">
            <el-table :data="groups" style="width: 100%">
              <el-table-column label="Group" min-width="170"><template #default="{ row }"><strong>{{ row.name }}</strong></template></el-table-column>
              <el-table-column label="Members" width="110" align="right"><template #default="{ row }"><span class="num">{{ row.members }}</span></template></el-table-column>
              <el-table-column label="Roles in Workspaces" min-width="260">
                <template #default="{ row }">
                  <div v-if="row.bindings.length" class="t-row" style="gap: 6px"><el-tag v-for="b in row.bindings" :key="b" type="info" effect="plain">{{ b }}</el-tag></div>
                  <span v-else class="muted">Not used in any Workspace</span>
                </template>
              </el-table-column>
              <el-table-column label="Sync to login system" width="190">
                <template #default="{ row }">
                  <status-tag v-if="row.sync === 'Synced'" value="Synced" />
                  <el-tooltip v-else content="Waiting for Keycloak for 4 minutes. Tokens do not contain this group yet, so it cannot be used in a Workspace."><span><status-tag tone="warn" label="Sync pending" /></span></el-tooltip>
                </template>
              </el-table-column>
              <el-table-column width="170" align="right">
                <template #default="{ row }">
                  <el-button link type="primary">Members</el-button>
                  <el-tooltip v-if="row.bindings.length" content="This group has a role in a Workspace. Remove those roles first."><span><el-button link disabled>Delete</el-button></span></el-tooltip>
                  <el-button v-else link type="danger">Delete</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </panel>

        <el-dialog v-model="dialog" title="Create group" width="460px">
          <el-form label-position="top" @submit.prevent>
            <el-form-item label="Name" for="grp-name"><el-input id="grp-name" v-model="name" placeholder="qa" /><div class="t-field-note">Lowercase letters, digits and hyphens. You can rename it later: Kubernetes sees a fixed id, so renaming never changes access.</div></el-form-item>
          </el-form>
          <template #footer><el-button @click="dialog = false">Cancel</el-button><el-button type="primary" @click="dialog = false">Create group</el-button></template>
        </el-dialog>
      </app-shell>`,
  };

  // /orgs/acme/clusters
  T.pages.Clusters = {
    data: () => ({ cluster: T.cluster, grant: T.grant, pool: T.pool, nodes: T.nodes, net: T.netPool }),
    template: `
      <app-shell active="clusters">
        <page-header title="Clusters" description="What a Platform Admin has granted this Organization. Only an Admin can change these numbers."></page-header>

        <panel :title="cluster.name" note="default cluster">
          <template #extra><status-tag value="Ready" label="Cluster ready" /></template>
          <div class="t-stack" style="gap: 24px">
            <div class="t-meters"><quota-meter v-for="g in grant" :key="g.key" :label="g.label" :used="g.used" :total="g.total" :unit="g.unit" /></div>
            <key-values :items="[['Storage class', 'standard'], ['kubectl endpoint', cluster.proxy], ['Web console', cluster.console], ['App domain', '*.' + cluster.appsDomain]]">
              <template #v0><code>standard</code></template>
              <template #v1><span class="mono">{{ cluster.proxy }}</span></template>
              <template #v2><span class="mono">{{ cluster.console }}</span></template>
              <template #v3><span class="mono">*.{{ cluster.appsDomain }}</span></template>
            </key-values>
          </div>
        </panel>

        <panel title="Shared pool" note="nodes shared with other Organizations">
          <div class="t-row t-between" style="margin-bottom: 16px"><div class="t-row"><code>{{ pool.id }}</code><status-tag :value="pool.status" /></div><span class="muted small">{{ pool.nodes }} nodes · overcommit ratio {{ pool.ratio }}×</span></div>
          <div class="t-meters two">
            <quota-meter label="CPU reserved, all tenants" :used="pool.cpu.reserved" :total="pool.cpu.reservable" unit="cores" />
            <quota-meter label="Memory reserved, all tenants" :used="pool.mem.reserved" :total="pool.mem.reservable" unit="Gi" />
          </div>
        </panel>

        <panel title="Dedicated nodes" note="only your Organization runs Pods here" flush>
          <div class="t-tablewrap">
            <el-table :data="nodes" style="width: 100%">
              <el-table-column label="Node" width="110"><template #default="{ row }"><code>{{ row.name }}</code></template></el-table-column>
              <el-table-column label="Health" width="130"><template #default="{ row }"><status-tag :value="row.health" /></template></el-table-column>
              <el-table-column label="Capacity" width="180"><template #default="{ row }"><status-tag :value="row.capacity" /></template></el-table-column>
              <el-table-column label="CPU left" width="130" align="right"><template #default="{ row }"><span class="num">{{ row.cpu.reservable - row.cpu.reserved }} of {{ row.cpu.reservable }}</span></template></el-table-column>
              <el-table-column label="Memory left" width="150" align="right"><template #default="{ row }"><span class="num">{{ row.mem.reservable - row.mem.reserved }} of {{ row.mem.reservable }} Gi</span></template></el-table-column>
              <el-table-column label="Used by" min-width="140"><template #default="{ row }"><span v-if="row.workspaces.length">{{ row.workspaces.join(', ') }}</span><span v-else class="muted">No Workspace</span></template></el-table-column>
            </el-table>
          </div>
        </panel>

        <panel title="Pod network pool">
          <key-values :items="[['Pool', net.id], ['Address range', net.cidr], ['Each Workspace gets', 'a /' + net.prefix + ' block (254 Pod addresses)'], ['Blocks free', net.free + ' of ' + net.total]]">
            <template #v0><code>{{ net.id }}</code></template>
            <template #v1><span class="mono">{{ net.cidr }}</span></template>
          </key-values>
        </panel>
      </app-shell>`,
  };

  // /orgs/acme/audit
  T.pages.Audit = {
    data: () => ({ rows: T.audit, actor: '', action: '' }),
    template: `
      <app-shell active="audit">
        <page-header title="Audit" description="Every change made to this Organization through the portal, newest first."></page-header>
        <panel flush>
          <div class="t-row" style="padding: 12px 20px; border-bottom: 1px solid var(--t-line-soft)">
            <el-select v-model="actor" placeholder="Anyone" clearable style="width: 220px" aria-label="Filter by person"><el-option v-for="a in ['alice@example.com', 'joe@example.com', 'admin@example.com']" :key="a" :value="a" :label="a" /></el-select>
            <el-select v-model="action" placeholder="Any action" clearable style="width: 220px" aria-label="Filter by action"><el-option v-for="a in ['workspace', 'route', 'group', 'invitation', 'cluster-grant', 'organization']" :key="a" :value="a" :label="a + '.*'" /></el-select>
          </div>
          <div class="t-tablewrap">
            <el-table :data="rows.filter((x) => (!actor || x.actor === actor) && (!action || x.action.startsWith(action)))" style="width: 100%" empty-text="No change matches these filters.">
              <el-table-column label="When" width="180"><template #default="{ row }"><span class="num">{{ row.when }}</span></template></el-table-column>
              <el-table-column label="Who" min-width="190" prop="actor" />
              <el-table-column label="Action" min-width="190"><template #default="{ row }"><span class="mono">{{ row.action }}</span></template></el-table-column>
              <el-table-column label="On" min-width="200" prop="target" />
              <el-table-column label="Result" width="130"><template #default="{ row }"><status-tag :value="row.result" /></template></el-table-column>
            </el-table>
          </div>
          <div class="t-panel-foot">Showing 6 of 6. Changes made with kubectl are recorded by the cluster, not here.</div>
        </panel>
      </app-shell>`,
  };

  // /orgs/acme/settings?as=admin|member&transfer=pending
  T.pages.Settings = {
    data: () => ({ r: T.route, name: 'Acme', desc: T.org.description, to: 'joe@example.com', ok: false }),
    computed: {
      role() { return this.r.query.as || 'owner'; },
      owner() { return this.role === 'owner'; },
      member() { return this.role === 'member'; },
      pending() { return this.r.query.transfer === 'pending'; },
    },
    template: `
      <app-shell active="settings" :limited="member">
        <page-header title="Settings"></page-header>

        <panel v-if="role === 'admin' && pending" title="Become the owner of Acme">
          <div class="t-stack" style="max-width: 640px">
            <key-values :items="[['Offered by', 'Alice Nguyen (alice@example.com), OrgOwner'], ['You', 'Joe Tran (joe@example.com), OrgAdmin'], ['Offer expires', 'in 6 days']]"></key-values>
            <el-alert type="warning" :closable="false" show-icon title="What changes when you accept">
              <div>You become <strong>OrgOwner</strong>: only you can delete the Organization or hand it over again.</div>
              <div>Alice becomes <strong>OrgAdmin</strong>. Workspaces, groups and quota stay as they are.</div>
            </el-alert>
            <el-checkbox v-model="ok" id="transfer-ok">I understand that I become responsible for this Organization.</el-checkbox>
            <div class="t-row" style="justify-content: flex-end">
              <el-button>Decline</el-button>
              <el-button type="primary" :disabled="!ok">Accept ownership</el-button>
            </div>
          </div>
        </panel>

        <panel v-if="!member" title="Details">
          <el-form label-position="top" style="max-width: 520px" @submit.prevent>
            <el-form-item label="Display name" for="set-name"><el-input id="set-name" v-model="name" /></el-form-item>
            <el-form-item label="Slug" for="set-slug"><el-input id="set-slug" model-value="acme" disabled /><div class="t-field-note">The slug is part of every Kubernetes name and cannot be changed.</div></el-form-item>
            <el-form-item label="Description" for="set-desc"><el-input id="set-desc" v-model="desc" type="textarea" :rows="2" /></el-form-item>
            <el-button type="primary">Save changes</el-button>
          </el-form>
        </panel>

        <panel v-if="owner && pending" title="Transfer ownership">
          <div class="t-stack" style="max-width: 560px">
            <key-values :items="[['Offered to', 'Joe Tran (joe@example.com), OrgAdmin'], ['Offer expires', 'in 6 days']]"></key-values>
            <p class="muted">You stay the owner until Joe accepts. If Joe stops being an OrgAdmin or leaves, the offer is cancelled.</p>
            <div><el-button>Cancel transfer</el-button></div>
          </div>
        </panel>

        <panel v-else-if="owner" title="Transfer ownership">
          <div class="t-stack" style="max-width: 560px">
            <p class="muted">Hand this Organization to one of its OrgAdmins. They must accept within 7 days; until then you stay the owner. Afterwards you become OrgAdmin.</p>
            <el-form label-position="top" @submit.prevent>
              <el-form-item label="New owner" for="set-owner"><el-select id="set-owner" v-model="to" style="width: 100%"><el-option value="joe@example.com" label="Joe Tran (joe@example.com), OrgAdmin" /></el-select><div class="t-field-note">Only OrgAdmins are listed. Change a member's role on the Members page first.</div></el-form-item>
              <el-button>Start transfer</el-button>
            </el-form>
          </div>
        </panel>

        <panel v-else title="Leave this Organization">
          <div class="t-stack" style="max-width: 560px">
            <p class="muted">You lose access to Acme and its Workspaces in the portal now, and in Kubernetes within 5 minutes.</p>
            <div><el-button type="danger" plain>Leave Acme</el-button></div>
          </div>
        </panel>

        <panel v-if="owner" title="Delete this Organization" danger>
          <div class="t-stack">
            <el-alert type="error" :closable="false" show-icon title="Acme still has 3 Workspaces" description="Delete every Workspace first. Deleting the Organization then removes its members, groups and grant, and frees the slug." />
            <confirm-by-name name="acme" action="Delete Organization" disabled />
          </div>
        </panel>
      </app-shell>`,
  };
})();
