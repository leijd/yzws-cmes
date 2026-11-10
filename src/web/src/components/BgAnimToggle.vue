<script setup lang="ts">
import { bgAnimEnabled, toggleBgAnim } from '../bgAnim'
</script>

<template>
  <button
    type="button"
    class="bg-anim-toggle"
    :class="{ on: bgAnimEnabled }"
    :aria-pressed="bgAnimEnabled"
    :title="bgAnimEnabled ? '点击关闭背景动效' : '点击开启背景动效'"
    @click="toggleBgAnim"
  >
    <span class="bg-anim-dot" aria-hidden="true"></span>
    <span>{{ bgAnimEnabled ? '背景动效：开' : '背景动效：关' }}</span>
  </button>
</template>

<style scoped>
.bg-anim-toggle {
  position: fixed; right: 18px; bottom: 18px; z-index: 90;
  display: inline-flex; align-items: center; gap: 8px;
  padding: 9px 15px; border-radius: 999px; cursor: pointer;
  font-family: Inter, "PingFang SC", "Microsoft YaHei", sans-serif;
  font-size: 13px; font-weight: 600; color: #1d4ed8;
  background: rgba(255, 255, 255, .82); backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px);
  border: 1px solid rgba(29, 78, 216, .22);
  box-shadow: 0 8px 22px rgba(15, 42, 110, .16);
  transition: background .18s, color .18s, border-color .18s, box-shadow .18s;
}
.bg-anim-toggle:hover { border-color: rgba(29, 78, 216, .5); box-shadow: 0 10px 26px rgba(15, 42, 110, .22); }
.bg-anim-toggle.on { color: #fff; border-color: transparent; background: linear-gradient(90deg, #2563eb, #06b6d4); }
.bg-anim-dot { width: 8px; height: 8px; border-radius: 50%; background: #94a3c4; flex-shrink: 0; }
.bg-anim-toggle.on .bg-anim-dot { background: #fff; box-shadow: 0 0 0 4px rgba(255, 255, 255, .35); animation: bg-anim-pulse 1.6s ease-in-out infinite; }
@keyframes bg-anim-pulse {
  0%, 100% { box-shadow: 0 0 0 3px rgba(255, 255, 255, .22); }
  50% { box-shadow: 0 0 0 6px rgba(255, 255, 255, .45); }
}
@media (max-width: 600px) {
  .bg-anim-toggle { right: 12px; bottom: 12px; padding: 8px 12px; font-size: 12px; }
}
</style>
