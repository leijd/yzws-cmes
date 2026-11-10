<script setup lang="ts">
import { toasts } from '../toast'
</script>

<template>
  <Teleport to="body">
    <div class="toast-host" role="alert">
      <div v-for="item in toasts" :key="item.id" class="toast-item">{{ item.message }}</div>
    </div>
  </Teleport>
</template>

<style scoped>
.toast-host {
  position: fixed;
  top: 24px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 10000;
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: center;
  pointer-events: none;
}
.toast-item {
  max-width: min(560px, 90vw);
  background: #b42318;
  color: #fff;
  padding: 10px 18px;
  border-radius: 8px;
  font-size: 14px;
  line-height: 1.5;
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.25);
  animation: toast-in 0.18s ease-out;
}
@keyframes toast-in {
  from { opacity: 0; transform: translateY(-8px); }
  to { opacity: 1; transform: translateY(0); }
}
</style>
