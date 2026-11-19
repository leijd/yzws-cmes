<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  sceneType?: string
  sceneSub?: string
  outsource?: boolean
  party?: string
  compact?: boolean
}>(), { sceneType: '', sceneSub: '', outsource: false, party: '', compact: false })

const labels: Record<string, string> = { A: '接服贸客商单', B: '接外包单', C: '接外包工序' }
const subLabels: Record<string, string> = {
  A1: '自己生产', A2: '外包部分业务', A3: '外包部分工序',
  B1: '完全自己生产', B2: '外包部分工序', C1: '完全自己生产',
}
const color = computed(() => ({ A: '#2F6BFF', B: '#F59E0B', C: '#8B5CF6' }[props.sceneType] || '#8A9099'))
const label = computed(() => labels[props.sceneType] || props.sceneType || '本厂')
const marker = computed(() => props.outsource || props.party === '外协' || props.sceneType === 'B' || props.sceneType === 'C')
</script>

<template>
  <span class="scene-tags" :class="{ compact }">
    <span v-if="sceneType" class="scene-badge" :style="{ '--scene-color': color }">
      <b>{{ sceneType }}</b><span v-if="!compact">{{ label }}</span><span v-if="sceneSub && !compact" class="scene-sub">{{ subLabels[sceneSub] || sceneSub }}</span>
    </span>
    <span v-if="marker" class="outsource-tag" title="外协单据">⇄ 外协</span>
  </span>
</template>

<style scoped>
.scene-tags { display: inline-flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.scene-badge { display: inline-flex; align-items: center; gap: 5px; padding: 2px 8px; border-radius: 999px; font-size: 12px; line-height: 18px;
  color: var(--scene-color); background: color-mix(in srgb, var(--scene-color) 10%, #fff); border: 1px solid color-mix(in srgb, var(--scene-color) 32%, #fff); white-space: nowrap; }
.scene-badge b { font-weight: 700; font-size: 11px; width: 16px; height: 16px; line-height: 16px; text-align: center; border-radius: 50%; color: #fff; background: var(--scene-color); }
.scene-sub { color: #4E5562; border-left: 1px solid color-mix(in srgb, var(--scene-color) 25%, #fff); padding-left: 6px; }
.outsource-tag { padding: 2px 7px; border-radius: 999px; font-size: 11px; line-height: 18px; color: #B45309; background: #FEF3C7; border: 1px solid #FCD34D; white-space: nowrap; }
.compact .scene-badge { padding: 1px 5px; }
</style>
