<script setup lang="ts">
// Where a Workspace is, by Kubernetes condition (mockups: condition-timeline):
// done, failed, in progress or waiting, with the controller's own words.
defineProps<{ conditions: { type: string; status: string; reason?: string; message?: string; when?: string }[] }>()

const tone = (s: string) => (s === 'True' ? 'ok' : s === 'False' ? 'bad' : s === 'Running' ? 'run' : 'wait')
const icon = (s: string) =>
  s === 'True' ? 'CircleCheckFilled' : s === 'False' ? 'CircleCloseFilled' : s === 'Running' ? 'Loading' : 'Clock'
const word = (s: string) =>
  s === 'True' ? 'done' : s === 'False' ? 'failed' : s === 'Running' ? 'in progress' : 'waiting'
</script>

<template>
  <ol class="t-conds">
    <li v-for="c in conditions" :key="c.type" class="t-cond">
      <el-icon
        class="t-cond-ic"
        :class="[tone(c.status), { 'is-loading': c.status === 'Running' }]"
        :aria-label="word(c.status)"
        ><component :is="icon(c.status)"
      /></el-icon>
      <span class="t-cond-name"
        >{{ c.type
        }}<template v-if="c.status !== 'True'">
          = {{ c.status === 'False' ? 'False' : 'Unknown'
          }}<span v-if="c.reason" class="muted"> ({{ c.reason }})</span></template
        ></span
      >
      <span class="t-cond-when">{{ c.when }}</span>
      <span class="t-cond-msg">{{ c.message }}</span>
    </li>
  </ol>
</template>
