<template>
  <view class="page">
    <!-- 上次体验厂快速进入 -->
    <view v-if="lastTrial" class="resume-card" @click="resumeLast">
      <view class="resume-left">
        <text class="resume-name">继续体验：{{ lastTrial.name }}</text>
        <text class="resume-code">{{ lastTrial.tenant_code }}</text>
      </view>
      <text class="resume-arrow">›</text>
    </view>

    <!-- 主流程：与网站版一致，验证码通过即一键开通 -->
    <view class="card main-card">
      <text class="card-title">新客户 · 免费体验</text>
      <text class="card-sub">
        手机号验证通过即自动开通体验制衣厂（30 天）——手机号、账号、密码、密钥、类型与起止时间自动生成…到期再决定，不花冤枉钱。
      </text>
      <view class="field">
        <text class="label">手机号</text>
        <input
          v-model.trim="phone"
          type="number"
          maxlength="11"
          placeholder="11 位手机号"
        />
      </view>
      <view class="field">
        <text class="label">短信验证码</text>
        <view class="code-row">
          <input
            v-model.trim="smsCode"
            type="number"
            maxlength="6"
            placeholder="6 位验证码"
            class="code-input"
          />
          <button
            class="send-btn"
            :disabled="countdown > 0 || sending || !phone"
            @click="sendCode"
          >
            {{ countdown > 0 ? `${countdown}s` : sending ? "发送中…" : "获取验证码" }}
          </button>
        </view>
        <text v-if="demoCode" class="demo-tip">演示模式验证码：{{ demoCode }}（未配置短信服务商）</text>
      </view>

      <!-- 制衣厂信息：与网站版「免费体验注册」表单一致 -->
      <view class="info-section">
        <text class="info-title">制衣厂信息</text>
        <text class="info-hint">平台管理员审批时将看到以下内容；联系电话以验证手机号为准，不可修改</text>
        <view class="field">
          <text class="label required">制衣厂名称</text>
          <input v-model.trim="factory.name" maxlength="60" placeholder="与营业执照一致的厂名" />
        </view>
        <view class="field">
          <text class="label">所在国 / 地区</text>
          <input v-model.trim="factory.region" maxlength="40" placeholder="如 中国广东 / 越南" />
        </view>
        <view class="field">
          <text class="label">厂规模</text>
          <picker :range="scaleLabels" :value="scaleIndex" @change="onScaleChange">
            <view class="picker-value">{{ scaleLabels[scaleIndex] }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="label required">联系人</text>
          <input v-model.trim="factory.contact_name" maxlength="30" placeholder="联系人姓名" />
        </view>
        <view class="field">
          <text class="label">微信号</text>
          <input v-model.trim="factory.contact_wechat" maxlength="64" placeholder="选填，便于审批联系" />
        </view>
        <view class="field">
          <text class="label">QQ</text>
          <input v-model.trim="factory.contact_qq" maxlength="32" placeholder="选填" />
        </view>
        <view class="field">
          <text class="label">支付宝账号</text>
          <input v-model.trim="factory.contact_alipay" maxlength="64" placeholder="选填" />
        </view>
        <view class="field">
          <text class="label">厂地址</text>
          <input v-model.trim="factory.address" maxlength="120" placeholder="选填" />
        </view>
        <view class="field">
          <text class="label">备注</text>
          <textarea v-model.trim="factory.remark" maxlength="120" placeholder="主营品类、车间与产线数量等（可选）" class="remark" />
        </view>
      </view>

      <view v-if="mainError" class="error">{{ mainError }}</view>
      <button class="btn primary" :disabled="submitting" @click="startTrial">
        {{ submitting ? "提交中…" : "提交体验注册申请" }}
      </button>
      <view class="check-row">
        <text class="check">✓ 全功能开放</text>
        <text class="check">✓ 无需绑卡</text>
        <text class="check">✓ 专员一对一带教</text>
      </view>

      <!-- 开通结果 -->
      <view v-if="trialResult" class="result-box">
        <text class="result-title">{{ trialResult.name }}</text>
        <text class="result-meta">编码 {{ trialResult.code }} · {{ statusLabel(trialResult) }}</text>
        <text v-if="trialResult.message" class="result-msg">{{ trialResult.message }}</text>
        <!-- 检测到该手机号已是制衣厂员工：引导改用员工短信登录进厂 -->
        <view v-if="trialResult.has_factory_account" class="factory-notice" @click="goEmployeeSection">
          <text class="factory-notice-text">
            检测到该手机号已是制衣厂员工{{ trialResult.factory_names?.length ? `（${trialResult.factory_names.join("、")}）` : "" }}，点击下方「我是员工，登录所属工厂」短信登录直接进入 →
          </text>
        </view>
        <text v-if="!isApproved(trialResult)" class="result-tip">
          平台审核通过后，打开本页点击顶部「继续体验」即可直接进入工作台
        </text>
        <text v-if="!isApproved(trialResult)" class="result-link" @click="goPending">
          查看/修改注册信息 · 加急申请 →
        </text>
      </view>
    </view>

    <!-- 次要入口：员工账号 -->
    <view class="card">
      <view class="emp-header" @click="empOpen = !empOpen">
        <text class="card-title">我是员工，登录所属工厂</text>
        <text class="emp-toggle">{{ empOpen ? "收起 ▲" : "展开 ▼" }}</text>
      </view>
      <template v-if="empOpen">
        <text class="card-sub">绑定手机号的员工可免密进入所属工厂；首次进入需输入厂管理员提供的初始密码。</text>
        <view class="field">
          <text class="label">手机号</text>
          <input v-model.trim="empPhone" type="number" maxlength="11" placeholder="11 位手机号" />
        </view>
        <view class="field">
          <text class="label">短信验证码</text>
          <view class="code-row">
            <input
              v-model.trim="empSmsCode"
              type="number"
              maxlength="6"
              placeholder="6 位验证码"
              class="code-input"
            />
            <button
              class="send-btn"
              :disabled="empCountdown > 0 || empSending || !empPhone"
              @click="sendEmpCode"
            >
              {{ empCountdown > 0 ? `${empCountdown}s` : empSending ? "发送中…" : "获取验证码" }}
            </button>
          </view>
          <text v-if="empDemoCode" class="demo-tip">演示模式验证码：{{ empDemoCode }}</text>
        </view>
        <view v-if="empError" class="error">{{ empError }}</view>
        <button class="btn outline" :disabled="verifying" @click="findMyFactories">
          {{ verifying ? "查找中…" : "验证并查找我的工厂" }}
        </button>

        <template v-if="empVerified">
          <view v-for="t in trialTenants" :key="'t-' + t.code" class="tenant-row">
            <view class="tenant-info">
              <text class="tenant-name">{{ t.name }}</text>
              <text class="tenant-meta">体验厂 · {{ t.code }} · {{ statusLabel(t) }}</text>
            </view>
            <button
              v-if="isApproved(t)"
              class="enter-btn"
              :disabled="!!entering"
              @click="enterTrial(t)"
            >
              {{ entering === t.code ? "进入中…" : "进入工作台" }}
            </button>
          </view>

          <view v-for="f in factoryTenants" :key="'f-' + f.code" class="tenant-row">
            <view class="tenant-info">
              <text class="tenant-name">{{ f.name }}</text>
              <text class="tenant-meta">员工 · {{ f.code }} · {{ f.user_name || f.login_name }}</text>
            </view>
            <button
              class="enter-btn"
              :disabled="!!entering"
              @click="enterFactory(f)"
            >
              {{ entering === f.code ? "进入中…" : "进入" }}
            </button>
          </view>

          <view v-if="!trialTenants.length && !factoryTenants.length" class="empty-tip">
            该手机号下暂无可进入的工厂
          </view>

          <view v-if="needPassword" class="field password-field">
            <text class="label">首次登录密码</text>
            <input
              v-model="password"
              password
              placeholder="请输入厂管理员提供的初始密码"
            />
            <view v-if="passwordError" class="error">{{ passwordError }}</view>
            <button class="btn primary" :disabled="!!entering" @click="retryWithPassword">
              {{ entering ? "验证中…" : "使用密码进入" }}
            </button>
          </view>
        </template>
      </template>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onShow, onUnload } from "@dcloudio/uni-app";
import { request, setToken } from "../../utils/request";
import { auth } from "../../store/auth";

interface FactoryRef {
  code: string;
  name: string;
  login_name?: string;
  user_name?: string;
}

interface TenantItem {
  kind: "trial" | "factory";
  code: string;
  name: string;
  short_code?: string;
  admin_login_name?: string;
  login_name?: string;
  user_name?: string;
  trial_ends_at?: number;
  status?: string;
  registration_status?: string;
  entry_token?: string;
  message?: string;
  has_factory_account?: boolean;
  factory_names?: string[];
  /* 统一入口路由：手机号已属某厂时后端直接给出归属，不再新建体验厂 */
  routed?: string;
  factories?: FactoryRef[];
  login_ticket?: string;
}

interface VerifyResult {
  ticket: string;
  expires_in: number;
  trial_days: number;
  tenants: TenantItem[];
}

interface SendCodeResult {
  expires_in: number;
  sms_sent: boolean;
  demo_code?: string;
  message?: string;
}

interface EnterResult {
  access_token: string;
  refresh_token?: string;
  entry_token?: string;
}

interface TrialSession {
  entry_token: string;
  tenant_code: string;
  name: string;
  phone?: string;
}

const SESSION_KEY = "clothp_trial_entry";

/* 主流程状态（与网站版一致：验证码通过即一键开通） */
const phone = ref("");
const smsCode = ref("");
const demoCode = ref("");
const sending = ref(false);
const countdown = ref(0);
const submitting = ref(false);
const mainError = ref("");
const trialResult = ref<TenantItem | null>(null);
let timer: ReturnType<typeof setInterval> | null = null;

/* 制衣厂信息：与网站版 /register?mode=trial 表单字段一致 */
const scaleLabels = ["请选择（可选）", "微型厂", "小型厂", "中型厂", "大型厂"];
const scaleValues = ["", "micro", "small", "medium", "large"];
const scaleIndex = ref(0);
const factory = ref({
  name: "",
  region: "",
  scale: "",
  contact_name: "",
  contact_wechat: "",
  contact_qq: "",
  contact_alipay: "",
  address: "",
  remark: "",
});

function onScaleChange(e: { detail: { value: number } }) {
  scaleIndex.value = e.detail.value;
  factory.value.scale = scaleValues[e.detail.value];
}

function goPending() {
  uni.navigateTo({ url: "/pages/portal/pending" });
}

/* 检测到员工手机号：展开员工登录区并带出手机号 */
function goEmployeeSection() {
  empOpen.value = true;
  if (phone.value && !empPhone.value) empPhone.value = phone.value;
}

/* 员工入口状态 */
const empOpen = ref(false);
const empPhone = ref("");
const empSmsCode = ref("");
const empDemoCode = ref("");
const empSending = ref(false);
const empCountdown = ref(0);
const empVerified = ref(false);
const empError = ref("");
const verifying = ref(false);
const ticket = ref("");
const trialTenants = ref<TenantItem[]>([]);
const factoryTenants = ref<TenantItem[]>([]);
const entering = ref("");
const needPassword = ref(false);
const password = ref("");
const passwordError = ref("");
const pendingFactoryCode = ref("");
const pendingKind = ref<"trial" | "factory">("factory");

const lastTrial = ref<TrialSession | null>(uni.getStorageSync(SESSION_KEY) || null);

function statusLabel(t: { status?: string; registration_status?: string }): string {
  const s = t.registration_status || t.status || "";
  if (s === "approved") return "体验中";
  if (s === "pending") return "待平台审核";
  if (s === "rejected") return "审核未通过";
  return s || "待平台审核";
}

function isApproved(t: TenantItem): boolean {
  return (t.registration_status || t.status) === "approved";
}

async function sendCode() {
  mainError.value = "";
  if (!/^1\d{10}$/.test(phone.value)) {
    mainError.value = "请填写正确的 11 位手机号";
    return;
  }
  sending.value = true;
  try {
    const res = await request<SendCodeResult>("/public/tenant-register/send-code", {
      method: "POST",
      data: { phone: phone.value },
      silent: true,
      skipAuthRedirect: true,
    });
    if (res.demo_code) {
      demoCode.value = res.demo_code;
      smsCode.value = res.demo_code;
    }
    countdown.value = 60;
    if (timer) clearInterval(timer);
    timer = setInterval(() => {
      countdown.value -= 1;
      if (countdown.value <= 0) {
        countdown.value = 0;
        if (timer) clearInterval(timer);
        timer = null;
      }
    }, 1000);
    uni.showToast({ title: "验证码已发送，3 分钟内有效", icon: "none" });
  } catch (e) {
    mainError.value = (e as Error).message || "验证码发送失败";
  } finally {
    sending.value = false;
  }
}

async function finishLogin(
  res: EnterResult,
  tenantCode: string,
  tenantName: string,
  entryToken?: string
) {
  setToken(res.access_token);
  const nextEntryToken = res.entry_token || entryToken;
  if (nextEntryToken) {
    const session: TrialSession = {
      entry_token: nextEntryToken,
      tenant_code: tenantCode,
      name: tenantName,
    };
    uni.setStorageSync(SESSION_KEY, session);
    lastTrial.value = session;
  }
  await Promise.all([auth.fetchMe(), auth.fetchPermissions()]);
  /* 与网页版一致：登录后先进入「制衣厂门户」，再从门户进工作台 */
  uni.reLaunch({ url: "/pages/portal/home" });
}

/* 与网站版一致：验证码通过即开通（trial-register 携带 sms_code）；
 * 已审核通过的体验厂直接进入工作台，全程只需这一次验证。 */
async function startTrial() {
  mainError.value = "";
  trialResult.value = null;
  if (!/^1\d{10}$/.test(phone.value)) {
    mainError.value = "请填写正确的 11 位手机号";
    return;
  }
  if (!smsCode.value) {
    mainError.value = "请先获取并填写短信验证码";
    return;
  }
  /* 厂名/联系人不强制：后端会自动生成体验厂名；若该手机号已是员工，
   * 后端会直接路由进所属工厂，此步不应拦住员工 */
  const lackInfo = !factory.value.name || factory.value.name.length < 2 || !factory.value.contact_name;
  if (lackInfo) {
    const goOn = await new Promise<boolean>((resolve) => {
      uni.showModal({
        title: "信息不完整",
        content: "未填写厂名/联系人时将自动生成体验厂信息；若该手机号已是制衣厂员工，会直接进入所属工厂。继续提交吗？",
        success: (r) => resolve(!!r.confirm),
        fail: () => resolve(false),
      });
    });
    if (!goOn) return;
  }
  submitting.value = true;
  try {
    const res = await request<TenantItem>("/public/trial-register", {
      method: "POST",
      data: {
        phone: phone.value,
        contact_phone: phone.value,
        sms_code: smsCode.value,
        name: factory.value.name,
        region: factory.value.region,
        scale: factory.value.scale,
        contact_name: factory.value.contact_name,
        contact_wechat: factory.value.contact_wechat,
        contact_qq: factory.value.contact_qq,
        contact_alipay: factory.value.contact_alipay,
        address: factory.value.address,
        remark: factory.value.remark,
      },
      silent: true,
      skipAuthRedirect: true,
      idempotency: true,
    });
    /* 统一入口路由：手机号已属某制衣厂（员工/管理员）→ 直接进入其真正的工作台，
     * 不再卡在体验厂申请里 */
    const canEnterTrial = isApproved(res) && !!res.entry_token;
    if (!canEnterTrial && res.factories?.length && res.login_ticket) {
      empOpen.value = true;
      empPhone.value = phone.value;
      ticket.value = res.login_ticket;
      factoryTenants.value = res.factories.map((f) => ({
        kind: "factory" as const,
        code: f.code,
        name: f.name,
        login_name: f.login_name,
        user_name: f.user_name,
      }));
      trialTenants.value = [];
      empVerified.value = true;
      needPassword.value = false;
      if (factoryTenants.value.length === 1) {
        uni.showToast({ title: "已识别为制衣厂员工，正在进入…", icon: "none" });
        await enterFactory(factoryTenants.value[0]);
      } else {
        uni.showToast({ title: "已识别为制衣厂员工，请选择要进入的工厂", icon: "none" });
      }
      return;
    }
    trialResult.value = res;
    if (res.entry_token) {
      const session: TrialSession = {
        entry_token: res.entry_token,
        tenant_code: res.code,
        name: res.name,
        phone: phone.value,
      };
      uni.setStorageSync(SESSION_KEY, session);
      lastTrial.value = session;
    }
    if (isApproved(res) && res.entry_token) {
      /* 走统一进入逻辑：管理员凭令牌直入；员工首次进入需初始密码 */
      const item: TenantItem = {
        kind: "trial",
        code: res.code,
        name: res.name,
        entry_token: res.entry_token,
        status: res.status,
        registration_status: res.registration_status,
      };
      empOpen.value = true;
      trialTenants.value = [item];
      empVerified.value = true;
      needPassword.value = false;
      await enterTrial(item);
    }
  } catch (e) {
    mainError.value = (e as Error).message || "开通失败，请稍后重试";
  } finally {
    submitting.value = false;
  }
}

async function resumeLast() {
  const s = lastTrial.value;
  if (!s) return;
  entering.value = s.tenant_code;
  uni.showLoading({ title: "进入工作台…", mask: true });
  try {
    const res = await request<EnterResult>("/public/trial-enter", {
      method: "POST",
      data: { entry_token: s.entry_token, node_id: "mobile-app" },
      silent: true,
      skipAuthRedirect: true,
    });
    await finishLogin(res, s.tenant_code, s.name, s.entry_token);
  } catch (e) {
    const err = e as Error & { code?: string };
    const msg = err.message || "";
    if (err.code === "TRIAL_REJECTED" || /已被驳回/.test(msg)) {
      uni.removeStorageSync(SESSION_KEY);
      lastTrial.value = null;
    } else if (err.code === "INVALID_ENTRY_TOKEN" || /体验凭证/.test(msg)) {
      uni.removeStorageSync(SESSION_KEY);
      lastTrial.value = null;
    } else if (err.code === "PASSWORD_REQUIRED") {
      /* 员工首次进入：展开员工区，提示输入厂管理员提供的初始密码 */
      empOpen.value = true;
      empVerified.value = true;
      needPassword.value = true;
      pendingKind.value = "trial";
      pendingFactoryCode.value = s.tenant_code;
      trialTenants.value = [{ kind: "trial", code: s.tenant_code, name: s.name, entry_token: s.entry_token, status: "approved", registration_status: "approved" }];
      return;
    }
    uni.showToast({ title: msg || "进入失败，请重新验证手机号", icon: "none" });
  } finally {
    uni.hideLoading();
    entering.value = "";
  }
}

/* ===== 员工入口 ===== */

async function sendEmpCode() {
  empError.value = "";
  if (!/^1\d{10}$/.test(empPhone.value)) {
    empError.value = "请填写正确的 11 位手机号";
    return;
  }
  empSending.value = true;
  try {
    const res = await request<SendCodeResult>("/auth/trial/send-code", {
      method: "POST",
      data: { phone: empPhone.value },
      silent: true,
      skipAuthRedirect: true,
    });
    if (res.demo_code) {
      empDemoCode.value = res.demo_code;
      empSmsCode.value = res.demo_code;
    }
    empCountdown.value = 60;
    let t: ReturnType<typeof setInterval> | null = null;
    t = setInterval(() => {
      empCountdown.value -= 1;
      if (empCountdown.value <= 0 && t) clearInterval(t);
    }, 1000);
    uni.showToast({ title: "验证码已发送", icon: "none" });
  } catch (e) {
    empError.value = (e as Error).message || "验证码发送失败";
  } finally {
    empSending.value = false;
  }
}

async function findMyFactories() {
  empError.value = "";
  if (!/^1\d{10}$/.test(empPhone.value)) {
    empError.value = "请填写正确的 11 位手机号";
    return;
  }
  if (!empSmsCode.value) {
    empError.value = "请先获取并填写短信验证码";
    return;
  }
  verifying.value = true;
  try {
    const res = await request<VerifyResult>("/auth/trial/verify", {
      method: "POST",
      data: { phone: empPhone.value, code: empSmsCode.value },
      silent: true,
      skipAuthRedirect: true,
    });
    ticket.value = res.ticket;
    const items = res.tenants || [];
    /* 仅展示已审批启用的厂：驳回/待审/停用的厂不出现在手机登录列表中 */
    trialTenants.value = items.filter((t) => t.kind === "trial" && isApproved(t));
    factoryTenants.value = items.filter((t) => t.kind === "factory");
    empVerified.value = true;
    needPassword.value = false;
  } catch (e) {
    empError.value = (e as Error).message || "验证失败，请重试";
  } finally {
    verifying.value = false;
  }
}

async function enterTrial(t: TenantItem, pwd = "") {
  empError.value = "";
  entering.value = t.code;
  try {
    const res = await request<EnterResult>("/public/trial-enter", {
      method: "POST",
      data: {
        entry_token: t.entry_token || undefined,
        ticket: t.entry_token ? undefined : ticket.value,
        tenant_code: t.code,
        password: pwd || undefined,
        node_id: "mobile-app",
      },
      silent: true,
      skipAuthRedirect: true,
    });
    await finishLogin(res, t.code, t.name, t.entry_token);
  } catch (e) {
    const err = e as Error & { code?: string };
    /* 体验厂普通员工首次进入需厂管理员提供的初始密码 */
    if (err.code === "PASSWORD_REQUIRED") {
      needPassword.value = true;
      pendingKind.value = "trial";
      pendingFactoryCode.value = t.code;
    } else {
      uni.showToast({ title: err.message || "进入失败", icon: "none" });
    }
  } finally {
    entering.value = "";
  }
}

async function enterFactory(f: TenantItem) {
  passwordError.value = "";
  entering.value = f.code;
  try {
    const res = await request<EnterResult>("/auth/phone-login", {
      method: "POST",
      data: {
        ticket: ticket.value,
        tenant_code: f.code,
        node_id: "mobile-app",
      },
      silent: true,
      skipAuthRedirect: true,
    });
    await finishLogin(res, f.code, f.name);
  } catch (e) {
    const err = e as Error & { code?: string };
    if (err.code === "PASSWORD_REQUIRED") {
      needPassword.value = true;
      pendingKind.value = "factory";
      pendingFactoryCode.value = f.code;
    } else {
      uni.showToast({ title: err.message || "进入失败", icon: "none" });
    }
  } finally {
    entering.value = "";
  }
}

async function retryWithPassword() {
  passwordError.value = "";
  if (!password.value) {
    passwordError.value = "请输入初始密码";
    return;
  }
  entering.value = pendingFactoryCode.value;
  try {
    /* 体验厂员工与收费厂员工分别走各自的进入接口 */
    if (pendingKind.value === "trial") {
      const item = trialTenants.value.find((t) => t.code === pendingFactoryCode.value)
        || ({ kind: "trial", code: pendingFactoryCode.value, name: "" } as TenantItem);
      await enterTrial(item, password.value);
      return;
    }
    const res = await request<EnterResult>("/auth/phone-login", {
      method: "POST",
      data: {
        ticket: ticket.value,
        tenant_code: pendingFactoryCode.value,
        password: password.value,
        node_id: "mobile-app",
      },
      silent: true,
      skipAuthRedirect: true,
    });
    await finishLogin(res, pendingFactoryCode.value, "");
  } catch (e) {
    passwordError.value = (e as Error).message || "密码不正确";
  } finally {
    entering.value = "";
  }
}

/* 本机凭证指向的体验厂若已被驳回，清除「继续体验」，避免展示不可用的厂 */
onShow(async () => {
  const s = lastTrial.value;
  if (!s?.entry_token || !s?.tenant_code) return;
  try {
    const info = await request<{ registration_status: string }>(
      `/public/trial-info?entry_token=${encodeURIComponent(s.entry_token)}&tenant_code=${encodeURIComponent(s.tenant_code)}`,
      { silent: true, skipAuthRedirect: true }
    );
    if (info.registration_status === "rejected") {
      uni.removeStorageSync(SESSION_KEY);
      lastTrial.value = null;
    }
  } catch {
    // 凭证失效时由进入流程自行处理
  }
});

onUnload(() => {
  if (timer) clearInterval(timer);
});
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: linear-gradient(150deg, #dcebff 0%, #e9f4ff 34%, #dff2fb 66%, #d6e9fd 100%);
  padding: 32rpx;
  box-sizing: border-box;
}
.card {
  background: rgba(255, 255, 255, 0.88);
  border-radius: 20rpx;
  box-shadow: 0 12rpx 32rpx rgba(23, 37, 95, 0.08);
  padding: 32rpx;
  margin-bottom: 24rpx;
}
.main-card {
  border: 1px solid rgba(255, 255, 255, 0.9);
}
.card-title {
  display: block;
  font-size: 32rpx;
  font-weight: 700;
  color: #17255f;
}
.card-sub {
  display: block;
  font-size: 24rpx;
  color: #475569;
  margin-top: 10rpx;
  line-height: 1.7;
}
.field {
  padding: 20rpx 0;
}
.label {
  display: block;
  font-size: 24rpx;
  color: #64748b;
  margin-bottom: 12rpx;
}
input {
  border: 1px solid #e2e8f0;
  border-radius: 12rpx;
  padding: 18rpx 24rpx;
  font-size: 28rpx;
  line-height: 44rpx;
  min-height: 80rpx;
  background: #fff;
  box-sizing: border-box;
  width: 100%;
}
.code-row {
  display: flex;
  align-items: center;
  gap: 16rpx;
}
.code-input {
  flex: 1;
}
.send-btn {
  flex-shrink: 0;
  margin: 0;
  font-size: 24rpx;
  background: rgba(37, 99, 235, 0.08);
  color: #1d4ed8;
  border-radius: 10rpx;
  padding: 0 24rpx;
  height: 80rpx;
  line-height: 80rpx;
}
.send-btn[disabled] {
  opacity: 0.5;
}
.demo-tip {
  display: block;
  font-size: 22rpx;
  color: #dd6b20;
  margin-top: 12rpx;
}
.error {
  color: #e53e3e;
  font-size: 26rpx;
  margin: 16rpx 0;
}
.btn {
  border-radius: 999rpx;
  font-size: 30rpx;
  height: 88rpx;
  line-height: 88rpx;
  margin-top: 16rpx;
}
.btn.primary {
  background: linear-gradient(90deg, #2563eb, #06b6d4);
  color: #fff;
  box-shadow: 0 8rpx 20rpx rgba(37, 99, 235, 0.25);
}
.btn.primary[disabled] {
  opacity: 0.6;
}
.btn.outline {
  background: rgba(255, 255, 255, 0.75);
  color: #1d4ed8;
  border: 1px solid rgba(37, 99, 235, 0.35);
}
.check-row {
  display: flex;
  justify-content: center;
  gap: 24rpx;
  margin-top: 24rpx;
  flex-wrap: wrap;
}
.check {
  font-size: 22rpx;
  color: #16a34a;
}
.result-box {
  background: rgba(37, 99, 235, 0.06);
  border-radius: 12rpx;
  padding: 24rpx;
  margin-top: 24rpx;
}
.result-title {
  display: block;
  font-size: 28rpx;
  font-weight: 600;
  color: #17255f;
}
.result-meta {
  display: block;
  font-size: 24rpx;
  color: #64748b;
  margin-top: 10rpx;
}
.result-msg {
  display: block;
  font-size: 24rpx;
  color: #475569;
  margin-top: 12rpx;
  line-height: 1.5;
}
.result-tip {
  display: block;
  font-size: 22rpx;
  color: #94a3b8;
  margin-top: 16rpx;
  line-height: 1.5;
}
.result-link {
  display: block;
  font-size: 24rpx;
  color: #1d4ed8;
  margin-top: 20rpx;
  text-align: center;
}
.factory-notice {
  margin-top: 20rpx;
  background: #fffbeb;
  border: 1px solid #fde68a;
  border-radius: 12rpx;
  padding: 18rpx 24rpx;
}
.factory-notice-text {
  font-size: 24rpx;
  color: #b45309;
  line-height: 1.6;
}
.info-section {
  border-top: 1px dashed #dbeafe;
  margin-top: 24rpx;
  padding-top: 24rpx;
}
.info-title {
  display: block;
  font-size: 28rpx;
  font-weight: 700;
  color: #17255f;
}
.info-hint {
  display: block;
  font-size: 22rpx;
  color: #94a3b8;
  line-height: 1.6;
  margin-top: 8rpx;
}
.info-section .label.required::after {
  content: " *";
  color: #d92d20;
}
.picker-value {
  border: 1px solid #e2e8f0;
  border-radius: 12rpx;
  padding: 18rpx 24rpx;
  font-size: 28rpx;
  line-height: 44rpx;
  min-height: 80rpx;
  color: #0f172a;
  background: #fff;
  box-sizing: border-box;
}
.remark {
  width: 100%;
  height: 140rpx;
  border: 1px solid #e2e8f0;
  border-radius: 12rpx;
  padding: 18rpx 24rpx;
  font-size: 26rpx;
  background: #fff;
  box-sizing: border-box;
}
.emp-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.emp-toggle {
  font-size: 24rpx;
  color: #1d4ed8;
}
.tenant-row {
  display: flex;
  align-items: center;
  margin-top: 24rpx;
  padding-top: 24rpx;
  border-top: 1px solid #e2e8f0;
}
.tenant-info {
  flex: 1;
  min-width: 0;
}
.tenant-name {
  display: block;
  font-size: 28rpx;
  font-weight: 600;
  color: #17255f;
}
.tenant-meta {
  display: block;
  font-size: 22rpx;
  color: #64748b;
  margin-top: 8rpx;
}
.enter-btn {
  flex-shrink: 0;
  margin: 0;
  background: linear-gradient(90deg, #2563eb, #06b6d4);
  color: #fff;
  font-size: 24rpx;
  border-radius: 999rpx;
  padding: 0 28rpx;
  height: 68rpx;
  line-height: 68rpx;
}
.enter-btn[disabled] {
  opacity: 0.6;
}
.empty-tip {
  font-size: 24rpx;
  color: #94a3b8;
  margin-top: 24rpx;
  text-align: center;
}
.password-field {
  border-top: 1px solid #e2e8f0;
  margin-top: 24rpx;
}
.resume-card {
  background: rgba(255, 255, 255, 0.82);
  border: 1px solid rgba(37, 99, 235, 0.35);
  border-radius: 16rpx;
  padding: 28rpx 32rpx;
  margin-bottom: 24rpx;
  display: flex;
  align-items: center;
}
.resume-left {
  flex: 1;
}
.resume-name {
  display: block;
  font-size: 28rpx;
  font-weight: 600;
  color: #1d4ed8;
}
.resume-code {
  display: block;
  font-size: 22rpx;
  color: #0891b2;
  margin-top: 8rpx;
}
.resume-arrow {
  font-size: 40rpx;
  color: #1d4ed8;
}
</style>
