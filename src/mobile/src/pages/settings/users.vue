<template>
  <BrandNavBar title="人员与组织" />
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">人员与组织</view>
      <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新建账号</button>
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无账号</view>

    <view v-for="u in items" :key="u.id" class="card">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title" style="font-size: 28rpx">{{ u.name }}</text>
            <text class="tag" :class="u.status === 'active' ? 'tag-green' : 'tag-gray'">{{ u.status === "active" ? "启用" : "停用" }}</text>
          </view>
          <text class="muted">登录名 {{ u.login_name }}</text>
        </view>
      </view>
      <view class="row gap8 wrap" style="margin-top: 14rpx">
        <text v-for="r in u.roles" :key="r" class="tag tag-purple">{{ r }}</text>
        <text v-if="!u.roles.length" class="muted">未分配角色</text>
      </view>
      <view class="actions">
        <button class="btn btn-outline btn-sm" @click="openRoles(u)">角色/状态</button>
        <button class="btn btn-danger btn-sm" @click="remove(u)">删除</button>
      </view>
    </view>

    <!-- 新建账号 -->
    <view v-if="createSheet" class="sheet-mask" @click="createSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建员工账号</view>
        <view class="field">
          <text class="field-label required">工号（登录名同工号）</text>
          <input class="input" v-model.trim="form.employee_no" placeholder="如 YJZY2-000003" />
        </view>
        <view class="field">
          <text class="field-label required">姓名</text>
          <input class="input" v-model.trim="form.name" placeholder="员工姓名" />
        </view>
        <view class="field">
          <text class="field-label">手机号</text>
          <input class="input" v-model.trim="form.phone" type="number" maxlength="11" placeholder="可选" />
        </view>
        <view class="field">
          <text class="field-label required">初始密码（至少 12 位）</text>
          <input class="input" v-model="form.password" password placeholder="至少 12 位" />
        </view>
        <view class="field">
          <text class="field-label">角色（可多选）</text>
          <view class="chips">
            <view v-for="r in roles" :key="r.id" class="chip" :class="{ on: form.role_ids.includes(r.id) }" @click="toggleRole(r.id)">{{ r.name }}</view>
          </view>
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveCreate">{{ saving ? "创建中…" : "创建账号" }}</button>
      </view>
    </view>

    <!-- 角色/状态 -->
    <view v-if="rolesSheet" class="sheet-mask" @click="rolesSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">调整角色与状态 · {{ target?.name }}</view>
        <view class="field">
          <text class="field-label">角色（可多选）</text>
          <view class="chips">
            <view v-for="r in roles" :key="r.id" class="chip" :class="{ on: editRoleIds.includes(r.id) }" @click="toggleEditRole(r.id)">{{ r.name }}</view>
          </view>
        </view>
        <view class="field">
          <text class="field-label">账号状态</text>
          <view class="chips">
            <view class="chip" :class="{ on: editStatus === 'active' }" @click="editStatus = 'active'">启用</view>
            <view class="chip" :class="{ on: editStatus === 'disabled' }" @click="editStatus = 'disabled'">停用</view>
          </view>
        </view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveRoles">{{ saving ? "保存中…" : "保存" }}</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface UserItem {
  id: string; login_name: string; name: string; status: string;
  role_ids: string[]; roles: string[];
}
interface RoleItem { id: string; code: string; name: string }

const items = ref<UserItem[]>([]);
const roles = ref<RoleItem[]>([]);
const loading = ref(false);
const saving = ref(false);

const createSheet = ref(false);
const formError = ref("");
const form = ref({ employee_no: "", name: "", phone: "", password: "", role_ids: [] as string[] });

const rolesSheet = ref(false);
const target = ref<UserItem | null>(null);
const editRoleIds = ref<string[]>([]);
const editStatus = ref("active");

async function load() {
  loading.value = true;
  try {
    const res = await request<{ items: UserItem[] }>("/users", { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function loadRoles() {
  try {
    const res = await request<{ items: RoleItem[] }>("/roles", { silent: true });
    roles.value = res.items || [];
  } catch {
    roles.value = [];
  }
}

function toggleRole(id: string) {
  const idx = form.value.role_ids.indexOf(id);
  if (idx >= 0) form.value.role_ids.splice(idx, 1);
  else form.value.role_ids.push(id);
}
function toggleEditRole(id: string) {
  const idx = editRoleIds.value.indexOf(id);
  if (idx >= 0) editRoleIds.value.splice(idx, 1);
  else editRoleIds.value.push(id);
}

function openCreate() {
  form.value = { employee_no: "", name: "", phone: "", password: "", role_ids: [] };
  formError.value = "";
  createSheet.value = true;
}

async function saveCreate() {
  formError.value = "";
  const f = form.value;
  if (!f.employee_no || !f.name) { formError.value = "工号与姓名必填"; return; }
  if (f.password.length < 12) { formError.value = "初始密码至少 12 位"; return; }
  saving.value = true;
  try {
    await request("/users", {
      method: "POST",
      data: {
        employee_no: f.employee_no,
        name: f.name,
        phone: f.phone || undefined,
        password: f.password,
        role_ids: f.role_ids,
      },
      idempotency: true,
    });
    uni.showToast({ title: "账号已创建", icon: "none" });
    createSheet.value = false;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

function openRoles(u: UserItem) {
  target.value = u;
  editRoleIds.value = [...u.role_ids];
  editStatus.value = u.status;
  rolesSheet.value = true;
}

async function saveRoles() {
  const u = target.value;
  if (!u) return;
  saving.value = true;
  try {
    await request(`/users/${u.id}/roles`, {
      method: "PUT",
      data: { role_ids: editRoleIds.value, status: editStatus.value },
    });
    uni.showToast({ title: "已保存", icon: "none" });
    rolesSheet.value = false;
    load();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "保存失败", icon: "none" });
  } finally {
    saving.value = false;
  }
}

function remove(u: UserItem) {
  uni.showModal({
    title: "删除账号",
    content: `确定删除「${u.name}」的账号及入职资料吗？已参与业务的用户不能删除。`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/users/${u.id}`, { method: "DELETE" });
        uni.showToast({ title: "已删除", icon: "none" });
        load();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "删除失败", icon: "none" });
      }
    },
  });
}

onLoad(() => {
  load();
  loadRoles();
});
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
