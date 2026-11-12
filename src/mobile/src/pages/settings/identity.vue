<template>
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">用户角色权限</view>
      <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新建角色</button>
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无角色</view>

    <view v-for="r in items" :key="r.id" class="card">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title" style="font-size: 28rpx">{{ r.name }}</text>
            <text v-if="r.is_system" class="tag tag-purple">内置</text>
            <text v-if="r.source === 'template'" class="tag tag-blue">平台模板</text>
          </view>
          <text class="muted">{{ r.code }} · {{ r.user_count }} 人使用{{ r.description ? " · " + r.description : "" }}</text>
        </view>
      </view>
      <view class="actions">
        <button class="btn btn-outline btn-sm" @click="openPerms(r)">配置权限</button>
        <button v-if="!r.is_system && r.user_count === 0" class="btn btn-danger btn-sm" @click="remove(r)">删除</button>
      </view>
    </view>

    <!-- 新建角色 -->
    <view v-if="createSheet" class="sheet-mask" @click="createSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建角色</view>
        <view class="field">
          <text class="field-label required">角色编码</text>
          <input class="input" v-model.trim="form.code" placeholder="如 group_leader" />
        </view>
        <view class="field">
          <text class="field-label required">角色名称</text>
          <input class="input" v-model.trim="form.name" placeholder="如 组长" />
        </view>
        <view class="field">
          <text class="field-label">描述</text>
          <input class="input" v-model.trim="form.description" placeholder="可选" />
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveCreate">{{ saving ? "创建中…" : "创建角色" }}</button>
      </view>
    </view>

    <!-- 权限配置 -->
    <view v-if="permSheet" class="sheet-mask">
      <view class="sheet" @click.stop>
        <view class="row between" style="margin-bottom: 24rpx">
          <text class="title">配置权限 · {{ target?.name }}</text>
          <button class="btn btn-ghost btn-sm" @click="permSheet = false">关闭</button>
        </view>
        <view v-if="permLoading" class="empty">加载中…</view>
        <template v-else>
          <view v-for="g in permGroups" :key="g.module" class="perm-group">
            <text class="perm-module">{{ g.module }}</text>
            <view class="chips">
              <view
                v-for="p in g.items"
                :key="p.code"
                class="chip"
                :class="{ on: selectedCodes.includes(p.code) }"
                @click="togglePerm(p.code)"
              >{{ p.name }}</view>
            </view>
          </view>
          <view v-if="formError" class="error-text">{{ formError }}</view>
          <button class="btn btn-solid btn-block" :disabled="saving || target?.is_system" @click="savePerms">
            {{ target?.is_system ? "内置角色不可修改" : saving ? "保存中…" : "保存权限配置" }}
          </button>
        </template>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface RoleItem {
  id: string; code: string; name: string; description?: string;
  is_system: boolean; source: string; user_count: number; version: number;
}
interface PermItem { code: string; module: string; name: string; kind: string; admin_only: boolean }

const items = ref<RoleItem[]>([]);
const loading = ref(false);
const saving = ref(false);

const createSheet = ref(false);
const formError = ref("");
const form = ref({ code: "", name: "", description: "" });

const permSheet = ref(false);
const permLoading = ref(false);
const target = ref<RoleItem | null>(null);
const allPerms = ref<PermItem[]>([]);
const selectedCodes = ref<string[]>([]);

const permGroups = computed(() => {
  const groups = new Map<string, PermItem[]>();
  for (const p of allPerms.value) {
    const list = groups.get(p.module) || [];
    list.push(p);
    groups.set(p.module, list);
  }
  return [...groups.entries()].map(([module, items]) => ({ module, items }));
});

async function load() {
  loading.value = true;
  try {
    const res = await request<{ items: RoleItem[] }>("/roles", { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  form.value = { code: "", name: "", description: "" };
  formError.value = "";
  createSheet.value = true;
}

async function saveCreate() {
  formError.value = "";
  if (!form.value.code || !form.value.name) { formError.value = "角色编码与名称必填"; return; }
  saving.value = true;
  try {
    await request("/roles", { method: "POST", data: { ...form.value }, idempotency: true });
    uni.showToast({ title: "角色已创建", icon: "none" });
    createSheet.value = false;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

async function openPerms(r: RoleItem) {
  target.value = r;
  permSheet.value = true;
  permLoading.value = true;
  formError.value = "";
  try {
    const [all, mine] = await Promise.all([
      request<{ items: PermItem[] }>("/permissions", { silent: true }),
      request<{ permission_codes: string[] }>(`/roles/${r.id}/permissions`, { silent: true }),
    ]);
    allPerms.value = (all.items || []).filter((p) => !p.admin_only);
    selectedCodes.value = mine.permission_codes || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    permLoading.value = false;
  }
}

function togglePerm(code: string) {
  const idx = selectedCodes.value.indexOf(code);
  if (idx >= 0) selectedCodes.value.splice(idx, 1);
  else selectedCodes.value.push(code);
}

async function savePerms() {
  const r = target.value;
  if (!r || r.is_system) return;
  saving.value = true;
  try {
    await request(`/roles/${r.id}/permissions`, {
      method: "PUT",
      data: { permission_codes: selectedCodes.value },
    });
    uni.showToast({ title: "权限已保存", icon: "none" });
    permSheet.value = false;
  } catch (e) {
    formError.value = (e as Error).message || "保存失败";
  } finally {
    saving.value = false;
  }
}

function remove(r: RoleItem) {
  uni.showModal({
    title: "删除角色",
    content: `确定删除角色「${r.name}」吗？`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/roles/${r.id}`, { method: "DELETE" });
        uni.showToast({ title: "已删除", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "删除失败", icon: "none" });
      }
    },
  });
}

onLoad(load);
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 90vh; overflow-y: auto; box-sizing: border-box; }
.perm-group { margin-bottom: 24rpx; }
.perm-module { display: block; font-size: 24rpx; font-weight: 700; color: #17255f; margin-bottom: 12rpx; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
