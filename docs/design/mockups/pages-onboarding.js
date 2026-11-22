// Screens a user sees before belonging to an Organization, and the
// invitation link. Ownership transfer is accepted in Settings (pages-org.js).
(function () {
  T.pages = T.pages || {};

  // /welcome?state=empty|taken|pending|rejected
  T.pages.Welcome = {
    data: () => ({ name: 'Acme', slug: 'acme', slugEdited: false, purpose: 'Staging environments for the Acme web team: 2 environments, about 6 small services each.', r: T.route }),
    computed: {
      state() { return this.r.query.state || 'empty'; },
      slugError() {
        if (this.state === 'taken') return 'This slug is already taken. Choose another one.';
        if (!/^[a-z0-9]([a-z0-9-]{1,18}[a-z0-9])?$/.test(this.slug)) return 'Use 3 to 20 lowercase letters, digits or hyphens. Start and end with a letter or digit.';
        return '';
      },
      shown() { return this.slug || 'your-slug'; },
    },
    watch: { name(v) { if (!this.slugEdited) this.slug = v.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, 20); } },
    template: `
      <app-shell area="bare" narrow>
        <template v-if="state === 'empty' || state === 'taken'">
          <page-header title="Request an Organization" description="An Organization owns your Workspaces, members and quota. A Platform Admin reviews each request and sets the starting quota."></page-header>

          <panel>
            <el-form label-position="top" @submit.prevent>
              <el-form-item label="Display name" for="org-name">
                <el-input id="org-name" v-model="name" maxlength="60" />
              </el-form-item>
              <el-form-item label="Slug" for="org-slug" :error="slugError">
                <el-input id="org-slug" v-model="slug" maxlength="20" @input="slugEdited = true" />
                <div v-if="!slugError" class="t-field-note">3 to 20 lowercase letters, digits or hyphens.</div>
              </el-form-item>
              <el-alert type="warning" :closable="false" show-icon title="The slug cannot be changed later" style="margin-bottom: 18px">
                <div>It becomes part of every name Kubernetes sees:</div>
                <div>Workspaces: <code>{{ shown }}-&lt;workspace&gt;</code></div>
                <div>App addresses: <code>&lt;name&gt;-{{ shown }}-&lt;workspace&gt;.mvp.tenantry.site</code></div>
              </el-alert>
              <el-form-item label="What will you use it for?" for="org-purpose">
                <el-input id="org-purpose" v-model="purpose" type="textarea" :rows="3" maxlength="500" show-word-limit />
                <div class="t-field-note">The Platform Admin decides your quota from this. Say how many environments you need and roughly how big they are.</div>
              </el-form-item>
              <div class="t-row t-between">
                <span v-if="state === 'taken'" class="t-errcode">SLUG_TAKEN · request 01JD3K7Q</span><span v-else></span>
                <el-button type="primary" :disabled="!!slugError && state !== 'taken'" @click="state === 'taken' ? null : $root.go('/welcome', { state: 'pending' })">Send request</el-button>
              </div>
            </el-form>
          </panel>

          <panel title="Joining an existing Organization?">
            <p class="muted">Ask its owner for an invitation link, then open the link while logged in as <strong>{{ $root.me.email }}</strong>.</p>
          </panel>
        </template>

        <template v-else-if="state === 'pending'">
          <page-header title="Your request is waiting for review">
            <template #tags><status-tag value="Pending" /></template>
          </page-header>
          <panel title="What you asked for">
            <key-values :items="[['Display name', 'Acme'], ['Slug', 'acme'], ['Purpose', purpose], ['Sent', '21 Nov 2026, 14:02']]">
              <template #v1><code>acme</code> <span class="muted small">held for you while the request is open</span></template>
            </key-values>
          </panel>
          <panel title="What happens next">
            <div class="t-stack">
              <p>A Platform Admin reads the request and decides the starting quota. When it is approved, this page becomes the Overview of your Organization. If it is rejected, the reason appears here and you can send a new request.</p>
              <div class="t-row t-between">
                <span class="muted small">You can have one open request at a time.</span>
                <el-button @click="$root.go('/orgs/acme', { state: 'fresh' })"><el-icon><Refresh /></el-icon>&nbsp;Check again</el-button>
              </div>
            </div>
          </panel>
        </template>

        <template v-else>
          <page-header title="Your request was not approved">
            <template #tags><status-tag value="Rejected" /></template>
          </page-header>
          <panel title="Reason from the Platform Admin">
            <div class="t-stack">
              <el-alert type="error" :closable="false" title="The purpose is too short to decide a quota. Tell us how many environments you need and roughly how large they are." />
              <key-values :items="[['Display name', 'Acme'], ['Slug', 'acme'], ['Reviewed', '21 Nov 2026, 15:40']]">
                <template #v1><code>acme</code> <span class="muted small">released, you can request it again</span></template>
              </key-values>
            </div>
          </panel>
          <div class="t-row" style="justify-content: flex-end">
            <el-button type="primary" @click="$root.go('/welcome')">Send a new request</el-button>
          </div>
        </template>
      </app-shell>`,
  };

  // /invitations/:token?state=ok|mismatch|expired
  T.pages.Invitation = {
    data: () => ({ r: T.route }),
    computed: { state() { return this.r.query.state || 'ok'; } },
    template: `
      <app-shell area="bare" narrow>
        <template v-if="state === 'ok'">
          <page-header title="Join Acme" description="Alice Nguyen invited you to the Organization Acme."></page-header>
          <panel>
            <div class="t-stack">
              <key-values :items="[['Invited email', 'joe@example.com'], ['You are logged in as', 'joe@example.com'], ['Your role', 'OrgMember'], ['Group', 'developers'], ['Link expires', 'in 6 days']]">
                <template #v1>joe@example.com <status-tag tone="ok" label="Verified, matches" /></template>
              </key-values>
              <p class="t-explain">As <strong>OrgMember</strong> you see the Organization and the Workspaces your groups can reach. The group <code>developers</code> decides what you can do inside those Workspaces.</p>
              <div class="t-row" style="justify-content: flex-end">
                <el-button @click="$root.go('/welcome')">Not now</el-button>
                <el-button type="primary" @click="$root.go('/orgs/acme/workspaces', { as: 'member' })">Accept invitation</el-button>
              </div>
            </div>
          </panel>
        </template>
        <template v-else-if="state === 'mismatch'">
          <page-header title="This invitation is for another account"></page-header>
          <panel>
            <div class="t-stack">
              <el-alert type="error" :closable="false" show-icon title="The invitation was sent to j•••@example.com, but you are logged in as mallory@example.com." description="Log out and log in again with the invited email address. Nothing has been changed." />
              <p class="t-errcode">INVITATION_EMAIL_MISMATCH · request 01JD5P2M</p>
              <div class="t-row" style="justify-content: flex-end"><el-button type="primary">Log out and switch account</el-button></div>
            </div>
          </panel>
        </template>
        <template v-else>
          <page-header title="This invitation link no longer works"></page-header>
          <panel>
            <empty-state title="The link expired or was already used" description="Invitation links work once and expire after 7 days. Ask the person who invited you to send a new one."></empty-state>
          </panel>
        </template>
      </app-shell>`,
  };
})();
