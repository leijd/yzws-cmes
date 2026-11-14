<template>
  <BrandNavBar title="员工与账号" />
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">员工与账号</view>
      <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新建员工</button>
    </view>
    <view class="search-bar">
      <input class="input" v-model.trim="keyword" placeholder="姓名/工号搜索" @confirm="reload" />
      <button class="btn btn-outline" style="width: 160rpx; height: 80rpx; line-height: 80rpx" @click="reload">查询</button>
    </view>
    <view class="chips">
      <view v-for="s in statusOptions" :key="s.value" class="chip" :class="{ on: status === s.value }" @click="status = s.value; reload()">{{ s.label }}</view>
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无员工</view>

    <view v-for="e in items" :key="e.id" class="card">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title" style="font-size: 28rpx">{{ e.name }}</text>
            <text class="tag" :class="e.status === 'active' ? 'tag-green' : 'tag-gray'">{{ e.status === "active" ? "在职" : "停用" }}</text>
            <text v-if="e.has_account" class="tag tag-blue">已开通账号</text>
          </view>
          <text class="muted">工号 {{ e.code }} · {{ e.position || "未设置岗位" }}</text>
        </view>
      </view>
      <view class="row between" style="margin-top: 12rpx">
        <text class="muted">{{ e.workshop || "未分配车间" }}{{ e.production_line ? " · " + e.production_line : "" }} · {{ e.phone || "无手机号" }}</text>
      </view>
      <view class="actions">
        <button class="btn btn-outline btn-sm" @click="openEdit(e)">编辑</button>
        <button v-if="!e.has_account && canWrite" class="btn btn-solid btn-sm" @click="openAccount(e)">开通账号</button>
        <button v-if="e.has_account && canWrite" class="btn btn-outline btn-sm" @click="resetPassword(e)">重置密码</button>
        <button v-if="e.status === 'active' && canWrite" class="btn btn-danger btn-sm" @click="disable(e)">停用</button>
        <button v-if="!auth.hasPerm('identity:write')" class="btn btn-outline btn-sm" @click="applyView(e)">申请查看敏感信息</button>
      </view>
    </view>

    <!-- 新建/编辑员工 -->
    <view v-if="sheet" class="sheet-mask" @click="sheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">{{ form.id ? "编辑员工" : "新建员工" }}</view>
        <view v-if="!form.id" class="field">
          <text class="field-label">工号（留空自动生成）</text>
          <input class="input" v-model.trim="form.employee_no" placeholder="如 YJZY2-000003" />
        </view>
        <view class="field">
          <text class="field-label required">姓名</text>
          <input class="input" v-model.trim="form.name" placeholder="员工姓名" />
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">性别</text>
            <picker :range="genderLabels" :value="genderIndex" @change="genderIndex = $event.detail.value">
              <view class="picker-box">{{ genderLabels[genderIndex] }}</view>
            </picker>
          </view>
          <view class="field grow">
            <text class="field-label">入职日期</text>
            <picker mode="date" :value="form.hire_date" @change="form.hire_date = $event.detail.value">
              <view class="picker-box">{{ form.hire_date || "选择日期" }}</view>
            </picker>
          </view>
        </view>
        <view class="field">
          <text class="field-label">手机号</text>
          <input class="input" v-model.trim="form.phone" type="number" maxlength="11" placeholder="11 位手机号" />
        </view>
        <view class="field">
          <text class="field-label">岗位</text>
          <input class="input" v-model.trim="form.position" placeholder="如 车工/组长" />
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">车间</text>
            <input class="input" v-model.trim="form.workshop" placeholder="如 缝制一车间" />
          </view>
          <view class="field grow">
            <text class="field-label">产线</text>
            <input class="input" v-model.trim="form.production_line" placeholder="如 1# 产线" />
          </view>
        </view>
        <view class="field">
          <text class="field-label">计薪方式</text>
          <picker :range="payModeLabels" :value="payModeIndex" @change="payModeIndex = $event.detail.value">
            <view class="picker-box">{{ payModeLabels[payModeIndex] }}</view>
          </picker>
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="save">{{ saving ? "保存中…" : "保存" }}</button>
      </view>
    </view>

    <!-- 开通账号 -->
    <view v-if="accountSheet" class="sheet-mask" @click="accountSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">开通登录账号 · {{ target?.name }}</view>
        <view class="field">
          <text class="field-label">登录名（留空同工号）</text>
          <input class="input" v-model.trim="accountForm.login_name" :placeholder="target?.code" />
        </view>
        <view class="field">
          <text class="field-label">初始密码（留空自动生成，至少 12 位）</text>
          <input class="input" v-model="accountForm.password" password placeholder="可选" />
        </view>
        <view class="field">
          <text class="field-label">角色（可多选）</text>
          <view class="chips">
            <view v-for="r in roles" :key="r.id" class="chip" :class="{ on: accountForm.role_ids.includes(r.id) }" @click="toggleRole(r.id)">{{ r.name }}</view>
          </view>
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveAccount">{{ saving ? "开通中…" : "开通账号" }}</button>
      </view>
    </view>

    <!-- 初始密码结果 -->
    <view v-if="resultSheet" class="sheet-mask">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">账号已开通</view>
        <view class="kv"><text class="k">登录名</text><text class="v strong">{{ resultLoginName }}</text></view>
        <view class="kv"><text class="k">初始密码</text><text class="v strong" style="user-select: all">{{ resultPassword }}</text></view>
        <text class="muted" style="display: block; margin-top: 16rpx">请立即将初始密码告知员工，并提醒首次登录后修改密码。此密码仅显示一次。</text>
        <button class="btn btn-solid btn-block" @click="resultSheet = false">知道了</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request, uuid } from "../../utils/request";
import { auth } from "../../store/auth";

interface EmployeeItem {
  id: string; code: string; name: string; status: string;
  position?: string; workshop?: string; production_line?: string; phone?: string;
  has_account: boolean; account?: { login_name: string; status: string };
  version: number;
}
interface RoleItem { id: string; code: string; name: string }

const items = ref<EmployeeItem[]>([]);
const roles = ref<RoleItem[]>([]);
const loading = ref(false);
const saving = ref(false);
const createKey = ref("");
const keyword = ref("");
const status = ref("");
const statusOptions = [
  { label: "全部", value: "" },
  { label: "在职", value: "active" },
  { label: "停用", value: "disabled" },
];

const canWrite = computed(() => auth.hasPerm("identity:write") || auth.hasPerm("masterdata:write"));

const sheet = ref(false);
const formError = ref("");
const genderLabels = ["未设置", "男", "女"];
const genderValues = ["", "male", "female"];
const genderIndex = ref(0);
const payModeLabels = ["计件", "计时", "月薪", "计件+计时"];
const payModeValues = ["piece", "hourly", "monthly", "piece_hourly"];
const payModeIndex = ref(0);
const form = ref({
  id: "", employee_no: "", name: "", phone: "", position: "",
  workshop: "", production_line: "", hire_date: "", pay_mode: "piece", version: 0,
});

const accountSheet = ref(false);
const target = ref<EmployeeItem | null>(null);
const accountForm = ref({ login_name: "", password: "", role_ids: [] as string[] });

const resultSheet = ref(false);
const resultLoginName = ref("");
const resultPassword = ref("");

async function load() {
  loading.value = true;
  try {
    const q: string[] = [];
    if (keyword.value) q.push(`keyword=${encodeURIComponent(keyword.value)}`);
    if (status.value) q.push(`status=${status.value}`);
    const res = await request<{ items: EmployeeItem[] }>(`/employees${q.length ? "?" + q.join("&") : ""}`, { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}
function reload() {
  load();
}

/* 非租户管理员：发起敏感信息查看授权码申请，审批结果/演示授权码在申请页展示 */
function applyView(e: EmployeeItem) {
  uni.navigateTo({
    url: `/pages/notify/sensitive-apply?employee_id=${encodeURIComponent(e.id)}&employee_name=${encodeURIComponent(e.name || "")}&field_label=${encodeURIComponent("员工全部敏感信息")}`,
  });
}

async function loadRoles() {
  try {
    const res = await request<{ items: RoleItem[] }>("/roles", { silent: true });
    roles.value = res.items || [];
  } catch {
    roles.value = [];
  }
}

function openCreate() {
  form.value = { id: "", employee_no: "", name: "", phone: "", position: "", workshop: "", production_line: "", hire_date: "", pay_mode: "piece", version: 0 };
  genderIndex.value = 0;
  payModeIndex.value = 0;
  formError.value = "";
  /* 一次建档一个幂等键：连点/网络重试复用同键，服务端重放首次响应，不会多建员工 */
  createKey.value = uuid();
  sheet.value = true;
}

function openEdit(e: EmployeeItem) {
  form.value = {
    id: e.id, employee_no: e.code, name: e.name, phone: e.phone || "",
    position: e.position || "", workshop: e.workshop || "", production_line: e.production_line || "",
    hire_date: "", pay_mode: "piece", version: e.version,
  };
  genderIndex.value = 0;
  payModeIndex.value = 0;
  formError.value = "";
  sheet.value = true;
}

async function save() {
  formError.value = "";
  const f = form.value;
  if (!f.name) { formError.value = "请填写姓名"; return; }
  saving.value = true;
  try {
    const data: Record<string, unknown> = {
      name: f.name,
      phone: f.phone || undefined,
      gender: genderValues[genderIndex.value] || undefined,
      position: f.position || undefined,
      workshop: f.workshop || undefined,
      production_line: f.production_line || undefined,
      hire_date: f.hire_date || undefined,
      pay_mode: payModeValues[payModeIndex.value],
    };
    if (f.id) {
      data.version = f.version;
      await request(`/employees/${f.id}`, { method: "PATCH", data });
    } else {
      if (f.employee_no) data.employee_no = f.employee_no;
      await request("/employees", { method: "POST", data, idempotency: true, idempotencyKey: createKey.value || undefined });
    }
    uni.showToast({ title: "已保存", icon: "none" });
    sheet.value = false;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "保存失败";
  } finally {
    saving.value = false;
  }
}

function toggleRole(id: string) {
  const idx = accountForm.value.role_ids.indexOf(id);
  if (idx >= 0) accountForm.value.role_ids.splice(idx, 1);
  else accountForm.value.role_ids.push(id);
}

function openAccount(e: EmployeeItem) {
  target.value = e;
  accountForm.value = { login_name: "", password: "", role_ids: [] };
  formError.value = "";
  accountSheet.value = true;
}

async function saveAccount() {
  const e = target.value;
  if (!e) return;
  formError.value = "";
  if (accountForm.value.password && accountForm.value.password.length < 12) {
    formError.value = "初始密码至少 12 位";
    return;
  }
  saving.value = true;
  try {
    const res = await request<{ login_name: string; initial_password?: string }>(`/employees/${e.id}/account`, {
      method: "POST",
      data: {
        login_name: accountForm.value.login_name || undefined,
        password: accountForm.value.password || undefined,
        role_ids: accountForm.value.role_ids,
      },
      idempotency: true,
    });
    accountSheet.value = false;
    resultLoginName.value = res.login_name;
    resultPassword.value = res.initial_password || "（使用您设置的密码）";
    resultSheet.value = true;
    load();
  } catch (e) {
    formError.value = (e as Error).message || "开通失败";
  } finally {
    saving.value = false;
  }
}

function resetPassword(e: EmployeeItem) {
  uni.showModal({
    title: "重置密码",
    content: `确定重置「${e.name}」的登录密码吗？将生成新的 12 位初始密码。`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        const result = await request<{ login_name: string; initial_password?: string }>(`/employees/${e.id}/account/reset-password`, {
          method: "POST",
          data: {},
        });
        resultLoginName.value = result.login_name;
        resultPassword.value = result.initial_password || "（已重置）";
        resultSheet.value = true;
      } catch (err) {
        uni.showToast({ title: (err as Error).message || "重置失败", icon: "none" });
      }
    },
  });
}

function disable(e: EmployeeItem) {
  uni.showModal({
    title: "停用员工",
    content: `确定停用「${e.name}」吗？停用后不能登录与报工。`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/employees/${e.id}/disable`, { method: "POST", data: { version: e.version } });
        uni.showToast({ title: "已停用", icon: "none" });
        load();
      } catch (err) {
        uni.showToast({ title: (err as Error).message || "停用失败", icon: "none" });
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
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 90vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
