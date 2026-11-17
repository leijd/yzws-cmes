<template>
  <BrandNavBar title="员工信息" />
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">员工信息</view>
      <button v-if="canManage" class="btn btn-solid btn-sm" @click="openCreate">＋ 新建账号</button>
    </view>

    <!-- 查询 -->
    <view class="field" style="margin-bottom: 20rpx">
      <input class="input" v-model.trim="keyword" placeholder="搜索姓名 / 工号 / 登录名" />
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!filtered.length" class="empty">{{ items.length ? "没有匹配的员工" : "暂无员工账号" }}</view>

    <view v-for="u in filtered" :key="u.id" class="card">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title" style="font-size: 28rpx">{{ u.name }}</text>
            <text class="tag" :class="u.status === 'active' ? 'tag-green' : 'tag-gray'">{{ u.status === "active" ? "启用" : "停用" }}</text>
          </view>
          <text class="muted">工号 {{ u.employee_no || u.login_name }} · 登录名 {{ u.login_name }}</text>
          <text v-if="u.position || u.workshop_name" class="muted">
            {{ u.position || "未设岗位" }}{{ u.workshop_name ? " · " + u.workshop_name + (u.line_name ? "/" + u.line_name : "") : "" }}
          </text>
        </view>
      </view>
      <view class="row gap8 wrap" style="margin-top: 14rpx">
        <text v-for="r in u.roles" :key="r" class="tag tag-purple">{{ r }}</text>
        <text v-if="!u.roles.length" class="muted">未分配角色</text>
      </view>
      <view class="actions wrap">
        <button v-if="canManage" class="btn btn-outline btn-sm" @click="openProfile(u)">编辑资料</button>
        <button v-if="canManage" class="btn btn-outline btn-sm" @click="openRoles(u)">角色/状态</button>
        <button v-if="canManage" class="btn btn-outline btn-sm" @click="openAuth(u)">验证方式</button>
        <button v-if="canManage" class="btn btn-outline btn-sm" @click="resetPwd(u)">重置密码</button>
        <button v-if="canManage" class="btn btn-danger btn-sm" @click="remove(u)">删除</button>
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

    <!-- 编辑资料（对齐网页版员工信息档案字段） -->
    <view v-if="profileSheet" class="sheet-mask" @click="profileSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">编辑资料 · {{ target?.name }}</view>
        <view class="field">
          <text class="field-label required">姓名</text>
          <input class="input" v-model.trim="profileForm.name" />
        </view>
        <view class="field">
          <text class="field-label">岗位</text>
          <input class="input" v-model.trim="profileForm.position" placeholder="如 车工 / 组长（可选）" />
        </view>
        <view class="field">
          <text class="field-label">性别</text>
          <view class="chips">
            <view class="chip" :class="{ on: profileForm.gender === 'male' }" @click="profileForm.gender = 'male'">男</view>
            <view class="chip" :class="{ on: profileForm.gender === 'female' }" @click="profileForm.gender = 'female'">女</view>
          </view>
        </view>
        <view class="field">
          <text class="field-label">计薪方式</text>
          <view class="chips">
            <view class="chip" :class="{ on: profileForm.pay_mode === 'piecework' }" @click="profileForm.pay_mode = 'piecework'">计件</view>
            <view class="chip" :class="{ on: profileForm.pay_mode === 'monthly' }" @click="profileForm.pay_mode = 'monthly'">月薪</view>
          </view>
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveProfile">{{ saving ? "保存中…" : "保存资料" }}</button>
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

    <!-- 验证方式（短信固定开启；密码/微信可配；与网页版一致） -->
    <view v-if="authSheet" class="sheet-mask" @click="authSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">验证方式 · {{ target?.name }}</view>
        <view class="field row between">
          <text>短信验证码（固定开启）</text>
          <switch :checked="true" :disabled="true" color="#94a3b8" />
        </view>
        <view class="field row between">
          <text>账号密码</text>
          <switch :checked="authForm.password" color="#2563eb" @change="authForm.password = ($event as any).detail.value" />
        </view>
        <view class="field row between">
          <text>微信扫码</text>
          <switch :checked="authForm.wechat" color="#2563eb" @change="authForm.wechat = ($event as any).detail.value" />
        </view>
        <text class="muted" style="display: block; margin-bottom: 20rpx">关闭某方式后，该员工不能用此方式登录；短信方式始终可用。</text>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveAuth">{{ saving ? "保存中…" : "保存验证方式" }}</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { auth } from "../../store/auth";

interface UserItem {
  id: string; login_name: string; employee_id: string; employee_no: string; name: string; status: string;
  position?: string | null; workshop_name?: string | null; line_name?: string | null;
  role_ids: string[]; roles: string[];
}
interface RoleItem { id: string; code: string; name: string }

const canManage = computed(() => auth.hasPerm("identity:write"));

const items = ref<UserItem[]>([]);
const roles = ref<RoleItem[]>([]);
const loading = ref(false);
const saving = ref(false);
const keyword = ref("");

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase();
  if (!k) return items.value;
  return items.value.filter(
    (u) =>
      u.name.toLowerCase().includes(k) ||
      u.login_name.toLowerCase().includes(k) ||
      (u.employee_no || "").toLowerCase().includes(k)
  );
});

/* ===== 新建账号 ===== */
const createSheet = ref(false);
const formError = ref("");
const form = ref({ employee_no: "", name: "", phone: "", password: "", role_ids: [] as string[] });

/* ===== 编辑资料 ===== */
const profileSheet = ref(false);
const profileForm = ref({ employee_id: "", name: "", position: "", gender: "", pay_mode: "piecework", version: 0 });

/* ===== 角色/状态 ===== */
const rolesSheet = ref(false);
const target = ref<UserItem | null>(null);
const editRoleIds = ref<string[]>([]);
const editStatus = ref("active");

/* ===== 验证方式 ===== */
const authSheet = ref(false);
const authForm = ref({ password: true, wechat: false });

async function load() {
  loading.value = true;
  try {
    /* /users 直接返回 employee_id/工号/岗位/车间产线（后端联表），无需二次合并 */
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

function personId(u: UserItem): string {
  return u.employee_id || u.id;
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

/* ===== 编辑资料 ===== */
function openProfile(u: UserItem) {
  target.value = u;
  profileForm.value = {
    employee_id: personId(u),
    name: u.name,
    position: u.position || "",
    gender: "",
    pay_mode: "piecework",
    version: 0,
  };
  formError.value = "";
  profileSheet.value = true;
  /* 拉档案详情补全性别/计薪方式/版本 */
  request<{
    name: string; position?: string | null; gender?: string | null; pay_mode?: string | null; version: number;
  }>(`/employees/${encodeURIComponent(personId(u))}`, { silent: true })
    .then((d) => {
      profileForm.value.name = d.name || profileForm.value.name;
      profileForm.value.position = d.position || profileForm.value.position;
      profileForm.value.gender = d.gender || "";
      profileForm.value.pay_mode = d.pay_mode || "piecework";
      profileForm.value.version = d.version || 0;
    })
    .catch(() => { /* 详情拉取失败时仍可保存基础字段 */ });
}

async function saveProfile() {
  const f = profileForm.value;
  if (!f.name) { formError.value = "姓名必填"; return; }
  saving.value = true;
  try {
    await request(`/employees/${encodeURIComponent(f.employee_id)}`, {
      method: "PATCH",
      data: { name: f.name, position: f.position, gender: f.gender, pay_mode: f.pay_mode, version: f.version },
    });
    uni.showToast({ title: "资料已保存", icon: "success" });
    profileSheet.value = false;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "保存失败";
  } finally {
    saving.value = false;
  }
}

/* ===== 角色/状态 ===== */
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

/* ===== 验证方式 ===== */
function openAuth(u: UserItem) {
  target.value = u;
  authForm.value = { password: true, wechat: false };
  authSheet.value = true;
  request<{ items: { user_id: string; methods: { sms: boolean; password: boolean; wechat: boolean } }[] }>(
    "/users/auth-methods",
    { silent: true }
  )
    .then((r) => {
      const row = (r.items || []).find((x) => x.user_id === u.id);
      if (row) authForm.value = { password: row.methods.password, wechat: row.methods.wechat };
    })
    .catch(() => { /* 拉取失败用默认值 */ });
}

async function saveAuth() {
  const u = target.value;
  if (!u) return;
  saving.value = true;
  try {
    await request(`/users/${u.id}/auth-methods`, {
      method: "PUT",
      data: { password: authForm.value.password, wechat: authForm.value.wechat },
    });
    uni.showToast({ title: "验证方式已保存", icon: "success" });
    authSheet.value = false;
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "保存失败", icon: "none" });
  } finally {
    saving.value = false;
  }
}

/* ===== 重置密码：临时密码仅此一次展示 ===== */
function resetPwd(u: UserItem) {
  uni.showModal({
    title: "重置密码",
    content: `确定重置「${u.name}」的登录密码？其全部会话将被撤销。`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        const r = await request<{ temporary_password: string }>(
          `/people/${encodeURIComponent(personId(u))}/reset-password`,
          { method: "POST" }
        );
        uni.showModal({
          title: "密码已重置",
          content: `请将新密码安全传达给「${u.name}」：\n\n${r.temporary_password}\n\n此密码仅显示一次。`,
          showCancel: false,
        });
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "重置失败", icon: "none" });
      }
    },
  });
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
.page { padding: 24rpx 28rpx 60rpx; }
.title { font-size: 30rpx; font-weight: 600; }
.muted { color: #8a94a6; font-size: 26rpx; display: block; margin-top: 4rpx; }
.empty { text-align: center; color: #8a94a6; padding: 60rpx 0; font-size: 26rpx; }
.card { background: #fff; border-radius: 16rpx; padding: 20rpx 24rpx; margin-bottom: 16rpx; }
.tag { font-size: 22rpx; padding: 4rpx 16rpx; border-radius: 20rpx; }
.tag-green { background: #e6f7ee; color: #1a9e5c; }
.tag-gray { background: #f1f5f9; color: #94a3b8; }
.tag-purple { background: #f3e8ff; color: #7c3aed; }
.actions { display: flex; gap: 12rpx; margin-top: 16rpx; flex-wrap: wrap; }
.actions.wrap { flex-wrap: wrap; }
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
