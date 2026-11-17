<template>
  <BrandNavBar title="员工信息" />
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">员工信息</view>
      <button v-if="canWrite" class="btn btn-solid btn-sm" @click="openCreate">＋ 新建员工</button>
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
          <view class="row gap8 wrap">
            <text class="title" style="font-size: 28rpx">{{ e.name }}</text>
            <text class="tag" :class="e.status === 'active' ? 'tag-green' : 'tag-gray'">{{ e.status === "active" ? "在职" : "停用" }}</text>
            <text v-if="e.has_account" class="tag tag-blue">已开通账号</text>
          </view>
          <text class="muted">工号 {{ e.code }} · {{ e.position || "未设置岗位" }}</text>
          <text class="muted">{{ e.gender === "male" ? "男" : e.gender === "female" ? "女" : "性别未设" }} · {{ e.pay_mode === "piecework" ? "计件" : e.pay_mode === "monthly" ? "月薪" : "计薪未设" }}{{ e.hire_date ? " · " + e.hire_date + " 入职" : "" }}</text>
        </view>
      </view>
      <view class="row between" style="margin-top: 12rpx">
        <text class="muted">{{ e.workshop || "未分配车间" }}{{ e.production_line ? " · " + e.production_line : "" }} · {{ e.phone || "无手机号" }}</text>
      </view>
      <view class="row gap8 wrap" style="margin-top: 12rpx">
        <text v-for="r in displayRoles(e)" :key="r" class="tag tag-purple">{{ r }}</text>
      </view>
      <view class="actions wrap">
        <button v-if="canWrite" class="btn btn-outline btn-sm" @click="openEdit(e)">编辑</button>
        <button v-if="!e.has_account && canWrite" class="btn btn-solid btn-sm" @click="openAccount(e)">开通账号</button>
        <template v-if="e.has_account && canManageIdentity">
          <button class="btn btn-outline btn-sm" @click="openRoles(e)">角色/状态</button>
          <button class="btn btn-outline btn-sm" @click="openAuth(e)">验证方式</button>
        </template>
        <button v-if="e.has_account && canWrite" class="btn btn-outline btn-sm" @click="resetPassword(e)">重置密码</button>
        <button v-if="e.status === 'active' && canWrite" class="btn btn-danger btn-sm" @click="disable(e)">停用</button>
        <button v-if="e.has_account && canManageIdentity" class="btn btn-danger btn-sm" @click="remove(e)">删除</button>
      </view>
    </view>

    <!-- 新建/编辑员工（字段与网页版员工信息一致；编辑时自动回填已存值） -->
    <view v-if="sheet" class="sheet-mask" @click="sheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">{{ form.id ? "编辑员工" : "新建员工" }}</view>
        <!-- 敏感信息锁定提示（与网页版一致：他人查看 *** 占位，解锁/申请后才可见明文） -->
        <view v-if="sensitiveLocked" class="lock-tip">
          <text>敏感信息（手机号/证件/银行卡/住址等）已锁定{{ canEditSensitive ? "" : "，且你无修改权限" }}。</text>
          <view class="row gap8" style="margin-top: 12rpx">
            <button class="btn btn-outline btn-sm" @click="openUnlock">{{ canUnlockByPassword ? "解锁查看" : "申请/解锁查看" }}</button>
          </view>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label" :class="{ required: !!form.id }">工号{{ form.id ? "" : "（留空自动生成）" }}</text>
            <input class="input" v-model.trim="form.employee_no" placeholder="如 YJZY2-000003" />
          </view>
          <view class="field grow">
            <text class="field-label required">姓名</text>
            <input class="input" v-model.trim="form.name" placeholder="员工姓名" />
          </view>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">性别</text>
            <picker :range="genderLabels" :value="genderIndex" @change="genderIndex = $event.detail.value">
              <view class="picker-box">{{ genderLabels[genderIndex] }}</view>
            </picker>
          </view>
          <view class="field grow">
            <text class="field-label">出生日期</text>
            <view v-if="sensitiveReadonly" class="picker-box" style="color: #94a3b8">{{ form.birth_date || "—" }}</view>
            <picker v-else mode="date" :value="form.birth_date" @change="form.birth_date = $event.detail.value">
              <view class="picker-box">{{ form.birth_date || "选择日期" }}</view>
            </picker>
          </view>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">年龄（自动计算）</text>
            <view class="picker-box" style="color: #64748b">{{ ageText }}</view>
          </view>
          <view class="field grow">
            <text class="field-label">联系电话</text>
            <input class="input" v-model.trim="form.phone" type="number" maxlength="11" placeholder="11 位手机号" :disabled="sensitiveReadonly" />
          </view>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">身份证号</text>
            <input class="input" v-model.trim="form.id_card_no" maxlength="18" placeholder="18 位" :disabled="sensitiveReadonly" />
          </view>
          <view class="field grow">
            <text class="field-label">民族</text>
            <input class="input" v-model.trim="form.nation" placeholder="如 汉" :disabled="sensitiveReadonly" />
          </view>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">籍贯</text>
            <input class="input" v-model.trim="form.native_place" placeholder="如 广西贵港" :disabled="sensitiveReadonly" />
          </view>
          <view class="field grow">
            <text class="field-label">入职日期</text>
            <picker mode="date" :value="form.hire_date" @change="form.hire_date = $event.detail.value">
              <view class="picker-box">{{ form.hire_date || "选择日期" }}</view>
            </picker>
          </view>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">岗位</text>
            <input class="input" v-model.trim="form.position" placeholder="如 车工/组长" />
          </view>
          <view class="field grow">
            <text class="field-label">技能评级</text>
            <picker :range="gradeLabels" :value="gradeIndex" @change="gradeIndex = $event.detail.value">
              <view class="picker-box">{{ gradeLabels[gradeIndex] }}</view>
            </picker>
          </view>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">责任车间</text>
            <picker :range="workshopNames" :value="workshopIndex" @change="onWorkshopPick">
              <view class="picker-box">{{ form.workshop || "请选择" }}</view>
            </picker>
          </view>
          <view class="field grow">
            <text class="field-label">生产线</text>
            <picker :range="lineNames" :value="lineIndex" :disabled="!form.workshop" @change="onLinePick">
              <view class="picker-box">{{ form.production_line || (form.workshop ? "请选择" : "先选车间") }}</view>
            </picker>
          </view>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">工资模式</text>
            <view class="chips">
              <view class="chip" :class="{ on: form.pay_mode === 'piecework' }" @click="form.pay_mode = 'piecework'">计件</view>
              <view class="chip" :class="{ on: form.pay_mode === 'monthly' }" @click="form.pay_mode = 'monthly'">月薪</view>
            </view>
          </view>
          <view class="field grow">
            <text class="field-label">基础底薪(元)</text>
            <input class="input" v-model.trim="form.base_salary" type="digit" placeholder="0" />
          </view>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">单价档位</text>
            <picker :range="rateLabels" :value="rateIndex" @change="rateIndex = $event.detail.value">
              <view class="picker-box">{{ rateLabels[rateIndex] }}</view>
            </picker>
          </view>
          <view class="field grow">
            <text class="field-label">银行卡号</text>
            <input class="input" v-model.trim="form.bank_card" type="number" placeholder="可选" :disabled="sensitiveReadonly" />
          </view>
        </view>
        <view class="row gap8">
          <view class="field grow">
            <text class="field-label">开户银行</text>
            <input class="input" v-model.trim="form.bank_name" placeholder="可选" :disabled="sensitiveReadonly" />
          </view>
          <view class="field grow">
            <text class="field-label">紧急联系人</text>
            <input class="input" v-model.trim="form.emergency_contact" placeholder="可选" :disabled="sensitiveReadonly" />
          </view>
        </view>
        <view class="field">
          <text class="field-label">紧急联系电话</text>
          <input class="input" v-model.trim="form.emergency_phone" type="number" maxlength="11" placeholder="可选" :disabled="sensitiveReadonly" />
        </view>
        <view class="field">
          <text class="field-label">现住址</text>
          <textarea class="input area" v-model="form.address" placeholder="可选" :disabled="sensitiveReadonly" />
        </view>
        <view class="field">
          <text class="field-label">工序专长（多选）</text>
          <view class="chips">
            <view
              v-for="p in processes"
              :key="p.id"
              class="chip"
              :class="{ on: form.skill_process_ids.includes(p.id) }"
              @click="toggleProcess(p.id)"
            >{{ p.name }}</view>
          </view>
          <text v-if="!processes.length" class="muted" style="font-size: 22rpx">工序库为空或无读取权限，可后补</text>
        </view>
        <view class="field">
          <text class="field-label">备注</text>
          <textarea class="input area" v-model="form.remark" placeholder="如 熟练工，6年夹克经验" />
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

    <!-- 角色/状态 -->
    <view v-if="rolesSheet" class="sheet-mask" @click="rolesSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">调整角色与状态 · {{ target?.name }}</view>
        <view class="field">
          <text class="field-label">角色（可多选）</text>
          <view class="chips">
            <view v-for="r in roles" :key="r.id" class="chip" :class="{ on: rolesForm.role_ids.includes(r.id) }" @click="toggleRolesForm(r.id)">{{ r.name }}</view>
          </view>
        </view>
        <view class="field">
          <text class="field-label">账号状态</text>
          <view class="chips">
            <view class="chip" :class="{ on: rolesForm.status === 'active' }" @click="rolesForm.status = 'active'">启用</view>
            <view class="chip" :class="{ on: rolesForm.status === 'disabled' }" @click="rolesForm.status = 'disabled'">停用</view>
          </view>
        </view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="saveRoles">{{ saving ? "保存中…" : "保存" }}</button>
      </view>
    </view>

    <!-- 解锁敏感信息（管理员密码 / 审批授权码，5 分钟票据） -->
    <view v-if="unlockSheet" class="sheet-mask" @click="closeUnlock">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">解锁敏感信息 · {{ target?.name }}</view>
        <view class="muted" style="font-size: 24rpx; margin-bottom: 20rpx; display: block">
          解锁后 5 分钟内可查看明文，每次解锁均记审计日志。
        </view>
        <view class="field">
          <text class="field-label">解锁方式</text>
          <view class="chips">
            <view v-if="canUnlockByPassword" class="chip" :class="{ on: unlockMode === 'password' }" @click="unlockMode = 'password'">管理员密码</view>
            <view class="chip" :class="{ on: unlockMode === 'code' }" @click="unlockMode = 'code'">授权码</view>
          </view>
        </view>
        <view class="field">
          <text class="field-label required">{{ unlockMode === "password" ? "管理员登录密码" : "授权码" }}</text>
          <input class="input" v-model="unlockSecret" :password="unlockMode === 'password'" :placeholder="unlockMode === 'password' ? '输入你的登录密码' : '输入管理员签发的授权码'" />
        </view>
        <view v-if="unlockMode === 'code'" class="request-zone">
          <view class="request-title">还没有授权码？向制衣厂管理员发起申请</view>
          <view v-if="requestPending" class="request-state">申请已提交，等待管理员在提醒中审批（每 5 秒自动刷新）</view>
          <view v-else-if="requestInfo?.status === 'rejected'" class="request-state err">上次申请已被拒绝，可修改信息后重新提交</view>
          <view v-else-if="requestSmsSent" class="request-state ok">申请已通过，授权码已短信发送至您的手机，请查收后填入上方输入框</view>
          <view v-if="requestApprovedCode" class="request-state ok row between">
            <text>演示模式授权码：{{ requestApprovedCode }}（仅申请人可见）</text>
            <button class="btn btn-outline btn-sm" @click="useDemoCode">填入并解锁</button>
          </view>
          <view class="field">
            <text class="field-label">接收授权码短信的手机号</text>
            <input class="input" v-model.trim="requestPhone" type="number" maxlength="11" placeholder="留空自动使用本人档案手机号" />
          </view>
          <view class="field">
            <text class="field-label">申请事由（选填）</text>
            <input class="input" v-model.trim="requestReason" placeholder="如 需核对员工联系电话" />
          </view>
          <button class="btn btn-outline btn-block" :disabled="requestBusy" @click="applyViewRequest">
            {{ requestBusy ? "提交中…" : requestPending ? "重新提交申请" : "申请授权码" }}
          </button>
        </view>
        <view v-if="unlockError" class="error-text">{{ unlockError }}</view>
        <button class="btn btn-solid btn-block" :disabled="unlockBusy" @click="doUnlock">{{ unlockBusy ? "解锁中…" : "解锁查看" }}</button>
      </view>
    </view>

    <!-- 验证方式 -->
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

    <!-- 初始密码/重置结果 -->
    <view v-if="resultSheet" class="sheet-mask">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">操作成功</view>
        <view class="kv"><text class="k">登录名</text><text class="v strong">{{ resultLoginName }}</text></view>
        <view class="kv"><text class="k">临时密码</text><text class="v strong" style="user-select: all">{{ resultPassword }}</text></view>
        <text class="muted" style="display: block; margin-top: 16rpx">请立即将密码告知员工，并提醒首次登录后修改。此密码仅显示一次。</text>
        <button class="btn btn-solid btn-block" @click="resultSheet = false">知道了</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, watch, type Ref } from "vue";
import { onLoad, onShow } from "@dcloudio/uni-app";
import { request, uuid } from "../../utils/request";
import { auth } from "../../store/auth";

interface EmployeeItem {
  id: string; code: string; name: string; status: string;
  gender?: string | null; hire_date?: string | null; pay_mode?: string | null;
  position?: string; workshop?: string; production_line?: string; phone?: string;
  has_account: boolean; account?: { login_name: string; status: string };
  version: number;
}
interface RoleItem { id: string; code: string; name: string }
interface UserRow {
  id: string; login_name: string; employee_id: string; status: string;
  role_ids: string[]; roles: string[];
}

const items = ref<EmployeeItem[]>([]);
const usersByEmployee = ref<Map<string, UserRow>>(new Map());
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
const canManageIdentity = computed(() => auth.hasPerm("identity:write"));

function accountOf(e: EmployeeItem): UserRow | undefined {
  return usersByEmployee.value.get(e.id);
}

/* /users 返回角色 code，映射为角色名显示 */
const roleNameMap = computed(() => new Map(roles.value.map((r) => [r.code, r.name])));
function displayRoles(e: EmployeeItem): string[] {
  const u = accountOf(e);
  if (!u) return [];
  return u.roles.map((c) => roleNameMap.value.get(c) || c);
}

const sheet = ref(false);
const formError = ref("");
/* 与网页版一致：piecework=计件 / monthly=月薪 */
const genderLabels = ["未设置", "男", "女"];
const genderValues = ["", "male", "female"];
const genderIndex = ref(0);
const form = ref({
  id: "", employee_no: "", name: "", gender: "", birth_date: "", phone: "",
  id_card_no: "", nation: "汉", native_place: "", position: "", workshop: "",
  production_line: "", skill_grade: "", hire_date: "", pay_mode: "piecework",
  base_salary: "0", rate_level: "", bank_card: "", bank_name: "",
  emergency_contact: "", emergency_phone: "", address: "", remark: "",
  skill_process_ids: [] as string[], version: 0,
});

/* ===== 敏感信息保护（与网页版一致，设计 6.4） =====
 * 敏感列 10 个：非本人查看一律 *** 占位；解锁走 POST /employees/{id}/reveal
 * （管理员密码或审批授权码）换 5 分钟 reveal_token；申请查看走 sensitive-view-requests。 */
const SENSITIVE_FIELDS = [
  "phone", "id_card_no", "address", "birth_date", "nation", "native_place",
  "bank_card", "bank_name", "emergency_contact", "emergency_phone",
] as const;
const revealToken = ref("");
const revealExpiresAt = ref(0);
const sensitiveLocked = ref(false);
const isSelfEmployee = computed(() => !!form.value.id && auth.user?.employee_id === form.value.id);
/* 敏感信息修改权限：本人或制衣厂管理员（identity:write） */
const canEditSensitive = computed(() => isSelfEmployee.value || canManageIdentity.value);
/* 管理员可用密码直接解锁；其他人需凭管理员签发的授权码 */
const canUnlockByPassword = computed(() => canManageIdentity.value);
/* 敏感控件只读：锁定期间、或无修改权限（非本人非管理员）时不可编辑。
 * 解锁只授予查看，不授予修改——修改仅限本人与制衣厂管理员。 */
const sensitiveReadonly = computed(() => sensitiveLocked.value || !canEditSensitive.value);

/* 锁定判定只看 phone：后端占位模式下"非空敏感列一律 ***"；历史污染数据可能让
 * 其他字段的字面值恰好是 ***（误判），phone 格式（11 位数字或空）不可能被污染成 ***。 */
function detectLocked(d: Record<string, unknown>) {
  sensitiveLocked.value = String(d["phone"] ?? "") === "***";
}

/* ===== 解锁到期自动恢复锁定：5 分钟票据到期后敏感字段恢复 *** 占位 =====
 * 只重置敏感列、保留非敏感字段的未保存编辑；表单关闭时停止定时器。 */
let unlockTimer: ReturnType<typeof setTimeout> | null = null;
function stopUnlockTimer() {
  if (unlockTimer) {
    clearTimeout(unlockTimer);
    unlockTimer = null;
  }
}
function startUnlockTimer() {
  stopUnlockTimer();
  const ms = Math.max(1000, revealExpiresAt.value * 1000 - Date.now());
  unlockTimer = setTimeout(() => {
    revealToken.value = "";
    for (const k of SENSITIVE_FIELDS) {
      const cur = String((form.value as Record<string, unknown>)[k] ?? "");
      if (cur && !cur.includes("*")) (form.value as Record<string, unknown>)[k] = "***";
    }
    sensitiveLocked.value = true;
    if (sheet.value) uni.showToast({ title: "解锁已过期，敏感信息已恢复锁定", icon: "none" });
  }, ms);
}
watch(sheet, (open) => { if (!open) stopUnlockTimer(); });

const unlockSheet = ref(false);
const unlockMode = ref<"password" | "code">("password");
const unlockSecret = ref("");
const unlockBusy = ref(false);
const unlockError = ref("");

function openUnlock() {
  unlockMode.value = canUnlockByPassword.value ? "password" : "code";
  unlockSecret.value = "";
  unlockError.value = "";
  /* 不预查历史申请：授权码只在本次点击「申请授权码」并经管理员审批后才显示 */
  requestInfo.value = null;
  requestPhone.value = "";
  requestReason.value = "";
  unlockSheet.value = true;
}

function closeUnlock() {
  unlockSheet.value = false;
  stopRequestPoll();
}

/* ===== 申请授权码（与网页版一致：弹层内申请 → 轮询状态 → 演示码填入解锁） ===== */
const requestBusy = ref(false);
const requestInfo = ref<any>(null);
const requestPhone = ref("");
const requestReason = ref("");
let requestTimer: ReturnType<typeof setInterval> | null = null;
const requestPending = computed(() => requestInfo.value?.status === "pending");
const requestApprovedCode = computed(() =>
  requestInfo.value?.demo_code_visible ? String(requestInfo.value.demo_code) : ""
);
const requestSmsSent = computed(() => !!requestInfo.value?.sms_sent);

async function refreshViewRequest() {
  if (!form.value.id) return;
  try {
    const res = await request<{ items: any[] }>(
      `/sensitive-view-requests/mine?employee_id=${encodeURIComponent(form.value.id)}`,
      { silent: true }
    );
    requestInfo.value = res.items?.[0] || null;
  } catch { /* 轮询失败不打断解锁操作 */ }
}

async function applyViewRequest() {
  if (!form.value.id || requestBusy.value) return;
  requestBusy.value = true;
  try {
    await request("/sensitive-view-requests", {
      method: "POST",
      data: {
        employee_id: form.value.id,
        field_key: "all",
        field_label: "全部敏感信息",
        reason: requestReason.value.trim(),
        phone: requestPhone.value.trim(),
      },
    });
    uni.showToast({ title: "申请已提交，等待管理员审批", icon: "none" });
    await refreshViewRequest();
    startRequestPoll(); /* 申请成功后才轮询审批结果 */
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "申请失败", icon: "none" });
  } finally {
    requestBusy.value = false;
  }
}

/* 演示环境：把签发的授权码直接填入解锁输入框 */
function useDemoCode() {
  unlockMode.value = "code";
  unlockSecret.value = requestApprovedCode.value;
}

function startRequestPoll() {
  stopRequestPoll();
  requestTimer = setInterval(() => { void refreshViewRequest(); }, 5000);
}
function stopRequestPoll() {
  if (requestTimer) {
    clearInterval(requestTimer);
    requestTimer = null;
  }
}

async function doUnlock() {
  if (!form.value.id || unlockBusy.value) return;
  if (!unlockSecret.value.trim()) { unlockError.value = unlockMode.value === "password" ? "请输入管理员密码" : "请输入授权码"; return; }
  unlockBusy.value = true;
  unlockError.value = "";
  try {
    const r = await request<{ reveal_token: string; expires_at: number }>(
      `/employees/${encodeURIComponent(form.value.id)}/reveal`,
      {
        method: "POST",
        data: unlockMode.value === "password"
          ? { password: unlockSecret.value }
          : { grant_code: unlockSecret.value.trim() },
        silent: true,
      }
    );
    /* 先凭票据重拉详情并回填明文，成功后才关闭弹层——任一步失败都留在弹层内可见 */
    const d = await request<EmployeeDetail>(
      `/employees/${encodeURIComponent(form.value.id)}?reveal_token=${encodeURIComponent(r.reveal_token)}`,
      { silent: true }
    );
    revealToken.value = r.reveal_token;
    revealExpiresAt.value = Math.min(Math.floor(r.expires_at / 1000), Date.now() + 5 * 60 * 1000);
    applyDetail(d);
    startUnlockTimer(); /* 5 分钟到期自动恢复 *** 锁定 */
    closeUnlock();
    uni.showToast({ title: "已解锁，5 分钟内可查看", icon: "success" });
  } catch (e) {
    unlockError.value = (e as Error).message || "解锁失败，请检查密码/授权码";
    uni.showToast({ title: unlockError.value, icon: "none" });
  } finally {
    unlockBusy.value = false;
  }
}

/* 工序专长选项（工序库，需 process:read；失败置空可后补） */
const processes = ref<{ id: string; name: string }[]>([]);
async function loadProcesses() {
  if (!auth.hasPerm("process:read")) { processes.value = []; return; }
  try {
    const r = await request<{ items: { id: string; name: string }[] }>("/process-definitions", { silent: true });
    processes.value = r.items || [];
  } catch { processes.value = []; }
}
function toggleProcess(id: string) {
  const idx = form.value.skill_process_ids.indexOf(id);
  if (idx >= 0) form.value.skill_process_ids.splice(idx, 1);
  else form.value.skill_process_ids.push(id);
}

/* 年龄自动计算（与网页版同规则） */
const ageText = computed(() => {
  if (!form.value.birth_date) return "—";
  const birth = new Date(form.value.birth_date);
  if (Number.isNaN(birth.getTime())) return "—";
  const now = new Date();
  let a = now.getFullYear() - birth.getFullYear();
  if (now.getMonth() < birth.getMonth() || (now.getMonth() === birth.getMonth() && now.getDate() < birth.getDate())) a--;
  return `${a} 岁`;
});

/* 技能评级/单价档位选项（字典加载，失败给兜底） */
const gradeLabels = ref(["未设置"]);
const gradeValues = ref([""]);
const gradeIndex = ref(0);
const rateLabels = ref(["未设置"]);
const rateValues = ref([""]);
const rateIndex = ref(0);
async function loadDictOptions(type: string, labels: Ref<string[]>, values: Ref<string[]>) {
  try {
    const r = await request<{ items: { code: string; label?: string; name?: string }[] }>(
      `/dictionaries/${type}/options`, { silent: true }
    );
    const items = (r.items || []).filter((o) => o.code !== "");
    if (items.length) {
      labels.value = ["未设置", ...items.map((o) => o.label || o.name || o.code)];
      values.value = ["", ...items.map((o) => o.code)];
    }
  } catch { /* 字典不可用时保持兜底"未设置" */ }
}

/* 表单内车间/产线级联（存名称文本，与网页版一致） */
const formWorkshops = ref<{ id: string; name: string }[]>([]);
const formLines = ref<{ id: string; name: string }[]>([]);
const workshopNames = computed(() => formWorkshops.value.map((w) => w.name));
const lineNames = computed(() => formLines.value.map((l) => l.name));
const workshopIndex = computed(() => Math.max(0, workshopNames.value.indexOf(form.value.workshop)));
const lineIndex = computed(() => Math.max(0, lineNames.value.indexOf(form.value.production_line)));
async function loadFormWorkshops() {
  try {
    const r = await request<{ items: { id: string; name: string; status: string }[] }>("/workshops", { silent: true });
    formWorkshops.value = (r.items || []).filter((w) => w.status === "active");
  } catch { formWorkshops.value = []; }
}
async function loadFormLines(workshopName: string) {
  const w = formWorkshops.value.find((x) => x.name === workshopName);
  if (!w) { formLines.value = []; return; }
  try {
    const r = await request<{ items: { id: string; name: string; status: string }[] }>(
      `/lines?workshop_id=${encodeURIComponent(w.id)}`, { silent: true }
    );
    formLines.value = (r.items || []).filter((l) => l.status === "active");
  } catch { formLines.value = []; }
}
function onWorkshopPick(e: { detail: { value: number } }) {
  form.value.workshop = workshopNames.value[e.detail.value] || "";
  form.value.production_line = "";
  void loadFormLines(form.value.workshop);
}
function onLinePick(e: { detail: { value: number } }) {
  form.value.production_line = lineNames.value[e.detail.value] || "";
}

const accountSheet = ref(false);
const target = ref<EmployeeItem | null>(null);
const accountForm = ref({ login_name: "", password: "", role_ids: [] as string[] });

const rolesSheet = ref(false);
const rolesForm = ref({ user_id: "", role_ids: [] as string[], status: "active" });

const authSheet = ref(false);
const authForm = ref({ user_id: "", password: true, wechat: false });

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

/* 账号映射：/users 含 employee_id，用于角色/验证方式/删除。
 * 依赖 identity:read 权限——权限刚变更的会话（旧 token）会 403，此时映射为空，
 * 相关按钮点击会提示重新登录。 */
const usersLoadFailed = ref(false);
async function loadUsers() {
  try {
    const res = await request<{ items: UserRow[] }>("/users", { silent: true });
    const m = new Map<string, UserRow>();
    for (const u of res.items || []) if (u.employee_id) m.set(u.employee_id, u);
    usersByEmployee.value = m;
    usersLoadFailed.value = false;
  } catch (e) {
    usersByEmployee.value = new Map();
    usersLoadFailed.value = true;
    uni.showToast({ title: (e as Error).message || "账号列表加载失败", icon: "none" });
  }
}

function reload() {
  load();
}

async function loadRoles() {
  try {
    const res = await request<{ items: RoleItem[] }>("/roles", { silent: true });
    roles.value = res.items || [];
  } catch {
    roles.value = [];
  }
}

function blankForm() {
  return {
    id: "", employee_no: "", name: "", gender: "", birth_date: "", phone: "",
    id_card_no: "", nation: "汉", native_place: "", position: "", workshop: "",
    production_line: "", skill_grade: "", hire_date: "", pay_mode: "piecework",
    base_salary: "0", rate_level: "", bank_card: "", bank_name: "",
    emergency_contact: "", emergency_phone: "", address: "", remark: "",
    skill_process_ids: [] as string[], version: 0,
  };
}

function setGenderIndex(v: string | null | undefined) {
  genderIndex.value = Math.max(0, genderValues.indexOf(v || ""));
}
function setGradeIndex(v: string | null | undefined) {
  gradeIndex.value = Math.max(0, gradeValues.value.indexOf(v || ""));
}
function setRateIndex(v: string | null | undefined) {
  rateIndex.value = Math.max(0, rateValues.value.indexOf(v || ""));
}

function openCreate() {
  form.value = blankForm();
  setGenderIndex("");
  setGradeIndex("");
  setRateIndex("");
  formLines.value = [];
  formError.value = "";
  createKey.value = uuid();
  sheet.value = true;
}

/* 编辑：拉详情回填全部字段（敏感列未解锁时返回 ***，保存时剔除防污染） */
interface EmployeeDetail {
  name: string; employee_no?: string; gender?: string | null; birth_date?: string | null;
  phone?: string | null; id_card_no?: string | null; nation?: string | null; native_place?: string | null;
  position?: string | null; workshop?: string | null; production_line?: string | null;
  skill_grade?: string | null; hire_date?: string | null; pay_mode?: string | null;
  base_salary?: number | null; rate_level?: string | null; bank_card?: string | null;
  bank_name?: string | null; emergency_contact?: string | null; emergency_phone?: string | null;
  address?: string | null; remark?: string | null; skill_process_ids?: string[];
  version: number;
}
/* 详情回填（openEdit 与解锁后共用）；顺带检测敏感列是否 *** 占位（锁定状态） */
function applyDetail(d: EmployeeDetail) {
  if (!form.value.id) return;
  form.value.name = d.name || form.value.name;
  form.value.version = d.version || form.value.version;
  form.value.birth_date = d.birth_date || "";
  form.value.id_card_no = d.id_card_no || "";
  form.value.nation = d.nation || "汉";
  form.value.native_place = d.native_place || "";
  form.value.base_salary = d.base_salary != null ? String(d.base_salary) : "0";
  form.value.bank_card = d.bank_card || "";
  form.value.bank_name = d.bank_name || "";
  form.value.emergency_contact = d.emergency_contact || "";
  form.value.emergency_phone = d.emergency_phone || "";
  form.value.address = d.address || "";
  form.value.remark = d.remark || "";
  form.value.skill_process_ids = d.skill_process_ids || [];
  form.value.hire_date = d.hire_date || form.value.hire_date;
  form.value.position = d.position || form.value.position;
  /* 电话：openEdit 用的是列表脱敏值；详情（本人/解锁明文，否则 ***）回填覆盖，
   * 解锁后此处即明文。含 * 的值保存时不回写（防污染）。 */
  if (d.phone !== undefined && d.phone !== null) form.value.phone = d.phone;
  if (d.workshop !== undefined && d.workshop !== null) form.value.workshop = d.workshop;
  if (d.production_line !== undefined && d.production_line !== null) form.value.production_line = d.production_line;
  setGenderIndex(d.gender);
  setGradeIndex(d.skill_grade);
  setRateIndex(d.rate_level);
  if (d.pay_mode) form.value.pay_mode = d.pay_mode === "monthly" ? "monthly" : "piecework";
  detectLocked(d as Record<string, unknown>);
  void loadFormLines(form.value.workshop);
}

function openEdit(e: EmployeeItem) {
  target.value = e;
  revealToken.value = "";
  sensitiveLocked.value = false;
  form.value = { ...blankForm(), id: e.id, employee_no: e.code, name: e.name, version: e.version };
  setGenderIndex(e.gender);
  setGradeIndex(e.skill_grade);
  setRateIndex(e.rate_level);
  if (e.pay_mode) form.value.pay_mode = e.pay_mode === "monthly" ? "monthly" : "piecework";
  form.value.position = e.position || "";
  form.value.workshop = e.workshop || "";
  form.value.production_line = e.production_line || "";
  form.value.hire_date = e.hire_date || "";
  form.value.phone = e.phone || "";
  void loadFormLines(form.value.workshop);
  formError.value = "";
  sheet.value = true;
  request<EmployeeDetail>(`/employees/${encodeURIComponent(e.id)}`, { silent: true })
    .then((d) => {
      if (!form.value.id || form.value.id !== e.id) return;
      applyDetail(d);
    })
    .catch(() => { /* 详情失败沿用列表值 */ });
}

async function save() {
  formError.value = "";
  const f = form.value;
  if (!f.name) { formError.value = "请填写姓名"; return; }
  saving.value = true;
  try {
    /* 与网页版字段全集对齐（masterdata employees 白名单） */
    const data: Record<string, unknown> = {
      name: f.name,
      gender: genderValues[genderIndex.value] || undefined,
      birth_date: f.birth_date || undefined,
      nation: f.nation || undefined,
      native_place: f.native_place || undefined,
      position: f.position || undefined,
      workshop: f.workshop || undefined,
      production_line: f.production_line || undefined,
      skill_grade: gradeValues.value[gradeIndex.value] || undefined,
      hire_date: f.hire_date || undefined,
      pay_mode: f.pay_mode,
      base_salary: Number(f.base_salary) || 0,
      rate_level: rateValues.value[rateIndex.value] || undefined,
      emergency_contact: f.emergency_contact || undefined,
      emergency_phone: f.emergency_phone || undefined,
      address: f.address || undefined,
      remark: f.remark || undefined,
      skill_process_ids: f.skill_process_ids,
    };
    /* 敏感列（10 个）：值为空或带 *（*** 占位）时不回写，避免用掩码污染明文（同网页版规则） */
    for (const key of SENSITIVE_FIELDS) {
      const v = String(f[key] ?? "").trim();
      if (v && !v.includes("*")) data[key] = v;
    }
    if (f.id) {
      data.version = f.version;
      await request(`/employees/${f.id}`, { method: "PATCH", data });
    } else {
      if (f.employee_no) data.employee_no = f.employee_no;
      await request("/employees", { method: "POST", data, idempotency: true, idempotencyKey: createKey.value || undefined });
    }
    uni.showToast({ title: "已保存", icon: "success" });
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
    loadUsers();
  } catch (e2) {
    formError.value = (e2 as Error).message || "开通失败";
  } finally {
    saving.value = false;
  }
}

function openRoles(e: EmployeeItem) {
  const u = accountOf(e);
  if (!u) {
    uni.showToast({ title: "账号信息未加载：请退出重新登录后再试", icon: "none" });
    return;
  }
  target.value = e;
  rolesForm.value = { user_id: u.id, role_ids: [...u.role_ids], status: u.status };
  rolesSheet.value = true;
}

function toggleRolesForm(id: string) {
  const idx = rolesForm.value.role_ids.indexOf(id);
  if (idx >= 0) rolesForm.value.role_ids.splice(idx, 1);
  else rolesForm.value.role_ids.push(id);
}

async function saveRoles() {
  saving.value = true;
  try {
    await request(`/users/${rolesForm.value.user_id}/roles`, {
      method: "PUT",
      data: { role_ids: rolesForm.value.role_ids, status: rolesForm.value.status },
    });
    uni.showToast({ title: "已保存", icon: "none" });
    rolesSheet.value = false;
    load();
    loadUsers();
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "保存失败", icon: "none" });
  } finally {
    saving.value = false;
  }
}

function openAuth(e: EmployeeItem) {
  const u = accountOf(e);
  if (!u) {
    uni.showToast({ title: "账号信息未加载：请退出重新登录后再试", icon: "none" });
    return;
  }
  target.value = e;
  authForm.value = { user_id: u.id, password: true, wechat: false };
  authSheet.value = true;
  request<{ items: { user_id: string; methods: { sms: boolean; password: boolean; wechat: boolean } }[] }>(
    "/users/auth-methods",
    { silent: true }
  )
    .then((r) => {
      const row = (r.items || []).find((x) => x.user_id === u.id);
      if (row) authForm.value = { user_id: u.id, password: row.methods.password, wechat: row.methods.wechat };
    })
    .catch(() => { /* 拉取失败用默认值 */ });
}

async function saveAuth() {
  saving.value = true;
  try {
    await request(`/users/${authForm.value.user_id}/auth-methods`, {
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

function resetPassword(e: EmployeeItem) {
  uni.showModal({
    title: "重置密码",
    content: `确定重置「${e.name}」的登录密码吗？将生成新的 12 位初始密码，其全部会话将被撤销。`,
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

function remove(e: EmployeeItem) {
  const u = accountOf(e);
  if (!u) return;
  uni.showModal({
    title: "删除账号",
    content: `确定删除「${e.name}」的登录账号吗？员工档案保留。已参与业务的用户不能删除。`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/users/${u.id}`, { method: "DELETE" });
        uni.showToast({ title: "已删除", icon: "none" });
        load();
        loadUsers();
      } catch (err) {
        uni.showToast({ title: (err as Error).message || "删除失败", icon: "none" });
      }
    },
  });
}

onLoad(() => {
  load();
  loadUsers();
  loadRoles();
  loadFormWorkshops();
  loadProcesses();
  void loadDictOptions("skill_grade", gradeLabels, gradeValues);
  void loadDictOptions("rate_level", rateLabels, rateValues);
});

/* 每次进入页面刷新账号映射：重登（权限变更）后返回本页即恢复角色/验证方式按钮 */
onShow(() => {
  load();
  loadUsers();
});
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 90vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
.input.area { width: 100%; min-height: 100rpx; padding: 16rpx 20rpx; box-sizing: border-box; font-size: 28rpx; border: 1rpx solid #dde3ec; border-radius: 12rpx; }
.input[disabled] { color: #94a3b8; background: #f8fafc; }
.lock-tip { background: #fff7ed; border: 1rpx solid #fed7aa; border-radius: 12rpx; padding: 20rpx 24rpx; margin-bottom: 24rpx; font-size: 24rpx; color: #9a3412; }
.request-zone { border-top: 1rpx dashed #e2e8f0; margin-top: 24rpx; padding-top: 20rpx; }
.request-title { font-size: 26rpx; font-weight: 600; color: #334155; margin-bottom: 16rpx; }
.request-state { font-size: 24rpx; color: #64748b; background: #f8fafc; border-radius: 10rpx; padding: 14rpx 18rpx; margin-bottom: 16rpx; }
.request-state.ok { color: #1a9e5c; background: #e6f7ee; }
.request-state.err { color: #d92d20; background: #fef2f2; }
</style>
