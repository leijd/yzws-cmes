<template>
  <view class="portal">
    <!-- 顶部导航 -->
    <view class="nav">
      <view class="nav-brand">
        <view class="brand-mark">智</view>
        <view class="brand-text">
          <text class="brand-name">一智万商</text>
          <text class="brand-sub">南宁一智万商科技有限责任公司</text>
        </view>
      </view>
      <view class="nav-actions">
        <button class="nav-btn ghost" @click="scrollJoin('login')">登录</button>
        <button class="nav-btn solid" @click="scrollJoin('trial')">体验</button>
      </view>
    </view>

    <!-- 主视觉：背景与网页版门户一致（蓝青渐变 + 斜向光带 + 光环 + 星网点阵） -->
    <view class="hero">
      <view class="hero-bg">
        <view class="hero-net"></view>
        <view class="hero-beam"></view>
        <view class="hero-ring ring-lg"></view>
        <view class="hero-ring ring-md"></view>
        <view class="hero-ring ring-sm"></view>
        <view class="hero-fade"></view>
      </view>
      <view class="hero-inner">
        <view class="hero-title">
          <text class="t-dark">一智</text>
          <text class="t-grad">万商</text>
        </view>
        <text class="hero-line grad">企业 AI 经营平台</text>
        <text class="hero-sub">1步2步3步，我们一起走进AI</text>
        <view class="hero-actions">
          <button class="btn solid" @click="scrollJoin('trial')">免费体验一个月</button>
          <button class="btn soft" @click="scrollApps">了解三大应用 ↓</button>
        </view>
        <view v-if="hasSession" class="session-banner" @click="goPortalHome">
          <text class="session-title">已登录，进入制衣厂门户 ›</text>
        </view>
        <!-- 已有体验厂凭证：一键回到自己的体验厂工作台（审核通过后无需再收验证码） -->
        <view v-else-if="trialSession" class="session-banner" @click="resumeTrialSession">
          <text class="session-title">继续体验「{{ trialSession.name }}」工作台 ›</text>
          <text class="session-sub">体验厂 {{ trialSession.tenant_code }}</text>
        </view>
      </view>
    </view>

    <!-- 三大应用 -->
    <view id="apps" class="section">
      <text class="section-head grad">三大应用</text>
      <view
        v-for="app in apps"
        :key="app.name"
        class="app-card"
      >
        <view class="app-top">
          <view class="app-icon" :class="app.theme">{{ app.icon }}</view>
          <view class="app-head">
            <text class="app-name">{{ app.name }}</text>
            <text class="app-en">{{ app.en }}</text>
            <text class="app-slogan">{{ app.slogan }}</text>
          </view>
        </view>
        <view class="app-points">
          <text v-for="p in app.points" :key="p" class="point">{{ p }}</text>
        </view>
        <button class="btn deep app-btn" @click="enterApp(app)">进入{{ app.name }}</button>
      </view>
    </view>

    <!-- 统一入口：体验 / 登录 -->
    <view id="join" class="section">
      <text class="section-head grad">统一入口，各走各的路</text>
      <view class="join-box">
        <view class="tabs">
          <view
            class="tab"
            :class="{ active: joinTab === 'trial' }"
            @click="joinTab = 'trial'"
          >
            <text class="tab-badge">新客户</text>
            <text>免费体验</text>
          </view>
          <view
            class="tab"
            :class="{ active: joinTab === 'login' }"
            @click="joinTab = 'login'"
          >
            <text class="tab-badge blue">注册客户</text>
            <text>客户登录</text>
          </view>
        </view>

        <!-- 免费体验 -->
        <view v-if="joinTab === 'trial'" class="panel">
          <text class="panel-desc">
            手机号验证通过即自动开通体验制衣厂（30 天）——手机号、账号、密码、密钥、类型与起止时间自动生成…到期再决定，不花冤枉钱。
          </text>
          <view class="emp-hint" @click="goEmployeeLogin">
            <text class="emp-hint-text">👷 我是制衣厂员工，短信登录进入所属工厂 →</text>
          </view>
          <view v-if="trialSession" class="emp-hint" @click="resumeTrialSession">
            <text class="emp-hint-text">↩ 继续体验「{{ trialSession.name }}」工作台 →</text>
          </view>
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
          <view v-if="trialError" class="error">{{ trialError }}</view>
          <button class="btn solid block" :disabled="submitting" @click="startTrial">
            {{ submitting ? "开通中…" : "验证并进入一智算" }}
          </button>
          <view class="check-row">
            <text class="check">✓ 全功能开放</text>
            <text class="check">✓ 无需绑卡</text>
            <text class="check">✓ 专员一对一带教</text>
          </view>
          <text class="panel-link" @click="goTrial">填写完整厂信息注册 / 已有体验账号 →</text>

          <!-- 开通结果 -->
          <view v-if="trialResult" class="result-box">
            <text class="result-title">{{ trialResult.name }}</text>
            <text class="result-meta">编码 {{ trialResult.code }} · {{ statusLabel(trialResult) }}</text>
            <text v-if="trialResult.message" class="result-msg">{{ trialResult.message }}</text>
            <!-- 检测到该手机号已是制衣厂员工：引导改用客户登录进厂，而不是开体验厂 -->
            <view v-if="trialResult.has_factory_account" class="factory-notice" @click="goEmployeeLogin">
              <text class="factory-notice-text">
                检测到该手机号已是制衣厂员工{{ trialResult.factory_names?.length ? `（${trialResult.factory_names.join("、")}）` : "" }}，请从「注册客户·客户登录」短信登录直接进入所属工厂 →
              </text>
            </view>
            <text v-if="!isApproved(trialResult)" class="result-tip">
              平台审核通过后，回到本页点顶部「继续体验…」或从「客户登录」短信登录即可直接进入工作台
            </text>
            <text v-if="!isApproved(trialResult)" class="panel-link" @click="goPending">
              查看/修改注册信息 · 加急申请 →
            </text>
          </view>
        </view>

        <!-- 客户登录：同页切换输入面板，不跳转独立登录页 -->
        <view v-else class="panel">
          <view class="method-tabs">
            <view
              class="method"
              :class="{ active: loginMethod === 'sms' }"
              @click="switchLoginMethod('sms')"
            >短信登录</view>
            <view
              class="method"
              :class="{ active: loginMethod === 'account' }"
              @click="switchLoginMethod('account')"
            >账号密码</view>
            <view class="method" @click="switchLoginMethod('register')">极速注册</view>
          </view>

          <!-- 短信登录（默认） -->
          <template v-if="loginMethod === 'sms'">
            <text class="panel-desc">已绑定手机号的员工/管理员，凭短信验证码直接登录所属制衣厂。</text>
            <view class="field">
              <text class="label">手机号</text>
              <input v-model.trim="loginPhone" type="number" maxlength="11" placeholder="11 位手机号" />
            </view>
            <view class="field">
              <text class="label">短信验证码</text>
              <view class="code-row">
                <input
                  v-model.trim="loginSmsCode"
                  type="number"
                  maxlength="6"
                  placeholder="6 位验证码"
                  class="code-input"
                />
                <button
                  class="send-btn"
                  :disabled="loginCountdown > 0 || loginSending || !loginPhone"
                  @click="sendLoginCode"
                >
                  {{ loginCountdown > 0 ? `${loginCountdown}s` : loginSending ? "发送中…" : "获取验证码" }}
                </button>
              </view>
              <text v-if="loginDemoCode" class="demo-tip">演示模式验证码：{{ loginDemoCode }}</text>
            </view>
            <view v-if="loginError" class="error">{{ loginError }}</view>
            <button class="btn solid block" :disabled="loginLoading" @click="doSmsLogin">
              {{ loginLoading ? "验证中…" : "登 录" }}
            </button>
            <text class="panel-link" @click="joinTab = 'trial'">还没有账号？极速注册免费体验 →</text>
          </template>

          <!-- 账号密码 -->
          <template v-else-if="loginMethod === 'account'">
            <text class="panel-desc">使用厂管理员分配的账号与密码登录。</text>
            <view class="field">
              <text class="label">账号</text>
              <input v-model.trim="loginName" placeholder="账号" autocomplete="username" />
            </view>
            <view class="field">
              <text class="label">密码</text>
              <input v-model="password" password placeholder="密码" autocomplete="current-password" />
            </view>
            <view v-if="loginError" class="error">{{ loginError }}</view>
            <button class="btn solid block" :disabled="logging" @click="doLogin">
              {{ logging ? "登录中…" : "登 录" }}
            </button>
          </template>

          <!-- 多厂选择：短信验证命中多个厂时同页展开；单厂进入失败时也保留一行可点重试 -->
          <view
            v-if="loginMethod === 'sms' && (loginChoices.length > 1 || (loginChoices.length === 1 && loginError))"
            class="choice-list"
          >
            <text class="choice-title">{{ loginChoices.length > 1 ? "选择要进入的制衣厂" : "你的制衣厂" }}</text>
            <view
              v-for="t in loginChoices"
              :key="t.code"
              class="choice-row"
              @click="enterTenant(t)"
            >
              <view class="choice-info">
                <text class="choice-name">{{ t.name }}</text>
                <text class="choice-meta">{{ t.code }} · {{ t.kind === "trial" ? "体验厂" : "制衣厂" }}</text>
              </view>
              <text class="choice-arrow">›</text>
            </view>
          </view>

          <!-- 普通员工首次短信登录需补初始密码 -->
          <view v-if="loginMethod === 'sms' && loginNeedPassword" class="field password-block">
            <text class="label">首次登录密码</text>
            <input v-model="loginPassword" password placeholder="厂管理员提供的初始密码" />
            <button class="btn solid block" :disabled="loginLoading" @click="retryLoginWithPassword">
              {{ loginLoading ? "验证中…" : "使用密码进入" }}
            </button>
          </view>
        </view>
      </view>
    </view>

    <!-- 页脚 -->
    <view class="footer">
      <view class="footer-brand">
        <text class="footer-title">一智万商 · 企业 AI 经营平台</text>
        <text class="footer-slogan">引商入局 · 开拓新土 · 共创未来</text>
        <text class="footer-desc">专注小微企业数字化：一智算打地基，二撮合接订单，三生万共成长。</text>
      </view>
      <view class="footer-contact">
        <text class="footer-item">商务合作：0771-XXXXXXX</text>
        <text class="footer-item">微信：yzs-service</text>
        <text class="footer-item">邮箱：business@yizhisuan.cn</text>
      </view>
      <text class="footer-copy">© {{ year }} 南宁一智万商科技有限责任公司</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad, onUnload } from "@dcloudio/uni-app";
import { request, setToken, getToken } from "../../utils/request";
import { auth } from "../../store/auth";

interface SendCodeResult {
  expires_in: number;
  sms_sent: boolean;
  demo_code?: string;
  message?: string;
}

interface FactoryRef {
  code: string;
  name: string;
  login_name?: string;
  user_name?: string;
}

interface TrialResult {
  code: string;
  name: string;
  status?: string;
  registration_status?: string;
  message?: string;
  entry_token?: string;
  has_factory_account?: boolean;
  factory_names?: string[];
  /* 统一入口路由：手机号已属某厂时后端直接给出归属，不再新建体验厂 */
  routed?: string;
  factories?: FactoryRef[];
  login_ticket?: string;
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

interface LoginTenant {
  kind: "trial" | "factory";
  code: string;
  name: string;
  status?: string;
  registration_status?: string;
  entry_token?: string;
  user_name?: string;
  login_name?: string;
}

interface SmsVerifyResult {
  ticket: string;
  expires_in: number;
  tenants: LoginTenant[];
  /* 兼容旧后端：只返回单个 tenant 字段 */
  tenant?: LoginTenant;
}

const SESSION_KEY = "clothp_trial_entry";

const hasSession = !!getToken();
/* 本机保存的体验厂进入凭证（注册/审批通过后写入），有则可一键回到体验厂工作台 */
const trialSession = ref<TrialSession | null>(uni.getStorageSync(SESSION_KEY) || null);
const year = new Date().getFullYear();

const apps = [
  {
    key: "yizhi",
    name: "一智算",
    en: "AI Accounting & Settlement",
    slogan: "算账结算省心",
    icon: "智",
    theme: "blue",
    points: ["订单工单全流程", "扫菲计件算工资", "一厂一密钥加密"],
  },
  {
    key: "erchuohe",
    name: "二撮合",
    en: "Order Matching Platform",
    slogan: "订单客户与制衣厂双向撮合",
    icon: "撮",
    theme: "green",
    points: ["产能评价模型", "AI 漫剧营销", "供需双向撮合"],
  },
  {
    key: "sansheng",
    name: "三生万",
    en: "Business Model Intelligence",
    slogan: "经营分析模型 → 数据资产",
    icon: "万",
    theme: "purple",
    points: ["经营分析模型", "能力提升服务", "数据资产入伙"],
  },
];

/* 体验流程 */
const phone = ref("");
const smsCode = ref("");
const demoCode = ref("");
const sending = ref(false);
const countdown = ref(0);
const submitting = ref(false);
const trialError = ref("");
const trialResult = ref<TrialResult | null>(null);
let timer: ReturnType<typeof setInterval> | null = null;

/* 登录流程：客户登录默认短信登录，同页切换账号密码面板 */
const joinTab = ref<"trial" | "login">("trial");
const loginMethod = ref<"sms" | "account">("sms");
const loginName = ref("");
const password = ref("");
const logging = ref(false);
const loginError = ref("");

const loginPhone = ref("");
const loginSmsCode = ref("");
const loginDemoCode = ref("");
const loginSending = ref(false);
const loginCountdown = ref(0);
const loginLoading = ref(false);
/* 进厂重入锁：与按钮 loading 分离，避免「短信验证中 → 自动进厂」被自身的 loading 标志挡住 */
const enterBusy = ref(false);
const loginTicket = ref("");
const loginChoices = ref<LoginTenant[]>([]);
const loginNeedPassword = ref(false);
const loginPassword = ref("");
const loginPendingCode = ref("");
let loginTimer: ReturnType<typeof setInterval> | null = null;

function isApproved(t: { status?: string; registration_status?: string }): boolean {
  return (t.registration_status || t.status) === "approved";
}

function statusLabel(t: { status?: string; registration_status?: string }): string {
  const s = t.registration_status || t.status || "";
  if (s === "approved") return "体验中";
  if (s === "pending") return "待平台审核";
  if (s === "rejected") return "审核未通过";
  return s || "待平台审核";
}

function scrollJoin(tab: "trial" | "login") {
  joinTab.value = tab;
  scroll("#join");
}
function scrollApps() {
  scroll("#apps");
}
function scroll(selector: string) {
  uni.pageScrollTo({ selector, duration: 300 } as never);
}

async function sendCode() {
  trialError.value = "";
  if (!/^1\d{10}$/.test(phone.value)) {
    trialError.value = "请填写正确的 11 位手机号";
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
    trialError.value = (e as Error).message || "验证码发送失败";
  } finally {
    sending.value = false;
  }
}

/* 与网站版一致：验证码通过即一键开通（trial-register 携带 sms_code） */
async function startTrial() {
  trialError.value = "";
  trialResult.value = null;
  if (!/^1\d{10}$/.test(phone.value)) {
    trialError.value = "请填写正确的 11 位手机号";
    return;
  }
  if (!smsCode.value) {
    trialError.value = "请先获取并填写短信验证码";
    return;
  }
  submitting.value = true;
  try {
    const res = await request<TrialResult>("/public/trial-register", {
      method: "POST",
      data: { phone: phone.value, sms_code: smsCode.value },
      silent: true,
      skipAuthRedirect: true,
      idempotency: true,
    });
    /* 统一入口路由：手机号已属某制衣厂（员工/管理员）→ 直接进入其真正的工作台，
     * 不再走体验厂申请，避免“已开通却进不去”的死循环 */
    const canEnterTrial = isApproved(res) && !!res.entry_token;
    if (!canEnterTrial && res.factories?.length && res.login_ticket) {
      await routeToFactories(res.login_ticket, res.factories);
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
      trialSession.value = session;
    }
    /* 已审核通过：直接进入工作台（员工首次进入需初始密码，走统一进入逻辑） */
    if (isApproved(res) && res.entry_token) {
      await enterTrialFromResult(res);
    }
  } catch (e) {
    trialError.value = (e as Error).message || "开通失败，请稍后重试";
  } finally {
    submitting.value = false;
  }
}

/* 进入体验厂：管理员凭令牌直入；员工首次进入需初始密码，统一在登录面板提示 */
async function enterTrialFromResult(res: TrialResult) {
  joinTab.value = "login";
  loginMethod.value = "sms";
  loginChoices.value = [
    {
      kind: "trial",
      code: res.code,
      name: res.name,
      entry_token: res.entry_token,
      status: res.status,
      registration_status: res.registration_status,
    },
  ];
  scroll("#join");
  await enterTenant(loginChoices.value[0]);
}

/* 统一入口路由：手机号已属制衣厂 → 用后端签发的一次性票据直接登录进入，
 * 单厂直接进；多厂切到客户登录面板让员工选择 */
async function routeToFactories(ticket: string, factories: FactoryRef[]) {
  joinTab.value = "login";
  loginMethod.value = "sms";
  loginPhone.value = phone.value;
  loginTicket.value = ticket;
  loginChoices.value = factories.map((f) => ({
    kind: "factory" as const,
    code: f.code,
    name: f.name,
    login_name: f.login_name,
    user_name: f.user_name,
  }));
  if (!loginChoices.value.length) return;
  if (loginChoices.value.length === 1) {
    uni.showToast({ title: "已识别为制衣厂员工，正在进入…", icon: "none" });
    await enterTenant(loginChoices.value[0]);
  } else {
    uni.showToast({ title: "已识别为制衣厂员工，请选择要进入的工厂", icon: "none" });
    setTimeout(() => scroll("#join"), 60);
  }
}

/* 一键继续体验：用本机保存的进入令牌换正式会话；
 * 管理员直入，员工首次进入则切到登录面板补初始密码。 */
async function resumeTrialSession() {
  const s = trialSession.value;
  if (!s?.entry_token || !s.tenant_code) return;
  uni.showLoading({ title: "进入工作台…", mask: true });
  try {
    const enter = await request<EnterResult>("/public/trial-enter", {
      method: "POST",
      data: { entry_token: s.entry_token, node_id: "mobile-app" },
      silent: true,
      skipAuthRedirect: true,
    });
    await finishLogin(enter.access_token);
  } catch (e) {
    const err = e as Error & { code?: string };
    if (err.code === "PASSWORD_REQUIRED") {
      joinTab.value = "login";
      loginMethod.value = "sms";
      loginPhone.value = s.phone || "";
      loginChoices.value = [
        { kind: "trial", code: s.tenant_code, name: s.name, entry_token: s.entry_token },
      ];
      loginNeedPassword.value = true;
      loginPendingCode.value = s.tenant_code;
      scroll("#join");
      uni.showToast({ title: "首次进入请输入厂管理员提供的初始密码", icon: "none" });
      return;
    }
    /* 令牌失效（被驳回/过期/服务端重启）：清掉本机凭证并提示重新验证手机号 */
    uni.removeStorageSync(SESSION_KEY);
    trialSession.value = null;
    uni.showToast({ title: err.message || "体验凭证已失效，请重新验证手机号", icon: "none" });
  } finally {
    uni.hideLoading();
  }
}

async function finishLogin(accessToken: string) {
  setToken(accessToken);
  await Promise.all([auth.fetchMe(), auth.fetchPermissions()]);
  /* 与网页版一致：先落到「制衣厂门户」，再由门户进入工作台 */
  uni.reLaunch({ url: "/pages/portal/home" });
}

async function doLogin() {
  loginError.value = "";
  if (!loginName.value || !password.value) {
    loginError.value = "请填写账号和密码";
    return;
  }
  logging.value = true;
  try {
    await auth.login(loginName.value, password.value);
    /* 与网页版一致：先进入「制衣厂门户」，再由门户进工作台 */
    uni.reLaunch({ url: "/pages/portal/home" });
  } catch (e) {
    loginError.value = (e as Error).message || "登录失败，请稍后重试";
  } finally {
    logging.value = false;
  }
}

/* ===== 客户登录：同页面板切换 + 短信登录 ===== */

/* 员工通道：从体验面板一键切到客户登录的短信登录，并带出已填手机号 */
function goEmployeeLogin() {
  joinTab.value = "login";
  loginMethod.value = "sms";
  if (phone.value && !loginPhone.value) loginPhone.value = phone.value;
  scroll("#join");
}

function switchLoginMethod(method: "sms" | "account" | "register") {
  if (method === "register") {
    /* 极速注册即免费体验面板：仍在本页切换，不跳转 */
    joinTab.value = "trial";
    scroll("#join");
    return;
  }
  loginMethod.value = method;
  loginError.value = "";
  loginChoices.value = [];
  loginNeedPassword.value = false;
}

async function sendLoginCode() {
  loginError.value = "";
  if (!/^1\d{10}$/.test(loginPhone.value)) {
    loginError.value = "请填写正确的 11 位手机号";
    return;
  }
  loginSending.value = true;
  try {
    const res = await request<SendCodeResult>("/auth/trial/send-code", {
      method: "POST",
      data: { phone: loginPhone.value },
      silent: true,
      skipAuthRedirect: true,
    });
    if (res.demo_code) {
      loginDemoCode.value = res.demo_code;
      loginSmsCode.value = res.demo_code;
    }
    loginCountdown.value = 60;
    if (loginTimer) clearInterval(loginTimer);
    loginTimer = setInterval(() => {
      loginCountdown.value -= 1;
      if (loginCountdown.value <= 0) {
        loginCountdown.value = 0;
        if (loginTimer) clearInterval(loginTimer);
        loginTimer = null;
      }
    }, 1000);
    uni.showToast({ title: "验证码已发送", icon: "none" });
  } catch (e) {
    loginError.value = (e as Error).message || "验证码发送失败";
  } finally {
    loginSending.value = false;
  }
}

async function doSmsLogin() {
  loginError.value = "";
  loginChoices.value = [];
  loginNeedPassword.value = false;
  if (!/^1\d{10}$/.test(loginPhone.value)) {
    loginError.value = "请填写正确的 11 位手机号";
    return;
  }
  if (!loginSmsCode.value) {
    loginError.value = "请先获取并填写短信验证码";
    return;
  }
  loginLoading.value = true;
  try {
    const res = await request<SmsVerifyResult>("/auth/trial/verify", {
      method: "POST",
      data: { phone: loginPhone.value, code: loginSmsCode.value },
      silent: true,
      skipAuthRedirect: true,
    });
    loginTicket.value = res.ticket;
    /* 后端只返回已启用的厂；未启用/驳回的厂不会出现。
     * 兼容旧后端：只回 tenant 单字段时也当作一家厂处理。 */
    loginChoices.value = res.tenants?.length ? res.tenants : res.tenant ? [res.tenant] : [];
    if (!loginChoices.value.length) {
      loginError.value = "该手机号名下暂无可登录的制衣厂，可切换「极速注册」或联系厂管理员";
      return;
    }
    if (loginChoices.value.length === 1) {
      await enterTenant(loginChoices.value[0]);
    }
  } catch (e) {
    loginError.value = (e as Error).message || "验证失败，请重试";
  } finally {
    loginLoading.value = false;
  }
}

async function enterTenant(t: LoginTenant, pwd = "") {
  loginError.value = "";
  if (enterBusy.value) return;
  enterBusy.value = true;
  loginLoading.value = true;
  try {
    let res: EnterResult;
    if (t.kind === "trial") {
      res = await request<EnterResult>("/public/trial-enter", {
        method: "POST",
        data: {
          entry_token: t.entry_token || undefined,
          ticket: t.entry_token ? undefined : loginTicket.value,
          tenant_code: t.code,
          password: pwd || undefined,
          node_id: "mobile-app",
        },
        silent: true,
        skipAuthRedirect: true,
      });
    } else {
      res = await request<EnterResult>("/auth/phone-login", {
        method: "POST",
        data: {
          ticket: loginTicket.value,
          tenant_code: t.code,
          password: pwd,
          node_id: "mobile-app",
        },
        silent: true,
        skipAuthRedirect: true,
      });
    }
    await finishLogin(res.access_token);
  } catch (e) {
    const err = e as Error & { code?: string };
    if (err.code === "PASSWORD_REQUIRED") {
      loginNeedPassword.value = true;
      loginPendingCode.value = t.code;
    } else {
      loginError.value = err.message || "进入失败，请重试";
    }
  } finally {
    loginLoading.value = false;
    enterBusy.value = false;
  }
}

async function retryLoginWithPassword() {
  if (!loginPassword.value) {
    loginError.value = "请输入初始密码";
    return;
  }
  const t = loginChoices.value.find((x) => x.code === loginPendingCode.value);
  if (!t) return;
  await enterTenant(t, loginPassword.value);
}

function enterApp(app: { key: string; name: string }) {
  if (app.key === "yizhi") {
    if (hasSession) goWorkbench();
    else scrollJoin("trial");
    return;
  }
  uni.showToast({ title: `${app.name}即将上线，敬请期待`, icon: "none" });
}

function goWorkbench() {
  uni.switchTab({ url: "/pages/workbench/workbench" });
}
function goPortalHome() {
  uni.navigateTo({ url: "/pages/portal/home" });
}
function goTrial() {
  uni.navigateTo({ url: "/pages/portal/trial" });
}
function goPending() {
  uni.navigateTo({ url: "/pages/portal/pending" });
}


/* 其他页面「去登录」可带 ?tab=login&method=account 直达指定面板 */
onLoad((query) => {
  const q = (query as Record<string, string>) || {};
  if (q.tab === "login") joinTab.value = "login";
  if (q.method === "account" || q.method === "sms") loginMethod.value = q.method;
  if (q.tab === "login") {
    setTimeout(() => scroll("#join"), 120);
  }
});

onUnload(() => {
  if (timer) clearInterval(timer);
  if (loginTimer) clearInterval(loginTimer);
});
</script>

<style scoped>
.portal {
  min-height: 100vh;
  background: linear-gradient(150deg, #dcebff 0%, #e9f4ff 34%, #dff2fb 66%, #d6e9fd 100%);
  padding-bottom: 0;
  box-sizing: border-box;
}

/* 导航 */
.nav {
  position: sticky;
  top: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 20rpx 32rpx;
  background: rgba(255, 255, 255, 0.75);
  backdrop-filter: blur(12px);
}
.nav-brand {
  display: flex;
  align-items: center;
}
.brand-mark {
  width: 72rpx;
  height: 72rpx;
  border-radius: 18rpx;
  background: linear-gradient(135deg, #2563eb, #06b6d4);
  color: #fff;
  font-size: 36rpx;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-right: 20rpx;
}
.brand-name {
  display: block;
  font-size: 32rpx;
  font-weight: 700;
  color: #17255f;
}
.brand-sub {
  display: block;
  font-size: 20rpx;
  color: #64748b;
  margin-top: 4rpx;
}
.nav-actions {
  display: flex;
  gap: 16rpx;
}
.nav-btn {
  margin: 0;
  font-size: 24rpx;
  border-radius: 999rpx;
  padding: 0 28rpx;
  height: 64rpx;
  line-height: 64rpx;
}
.nav-btn.ghost {
  background: rgba(255, 255, 255, 0.6);
  color: #1d4ed8;
  border: 1px solid rgba(37, 99, 235, 0.35);
}
.nav-btn.solid {
  background: linear-gradient(90deg, #2563eb, #06b6d4);
  color: #fff;
  box-shadow: 0 8rpx 20rpx rgba(37, 99, 235, 0.25);
}

/* 主视觉 */
.hero {
  position: relative;
  overflow: hidden;
  padding: 96rpx 48rpx 64rpx;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
}
/* 主视觉背景：复刻网页版门户的 vivid 主题（左上留亮区给深色标题，右下沉入饱和蓝） */
.hero-bg {
  position: absolute;
  left: 0;
  top: 0;
  right: 0;
  bottom: 0;
  overflow: hidden;
  /* 中间偏上的白色亮斑保证居中的标题/副标题对比度 */
  background: radial-gradient(circle at 50% 12%, rgba(255, 255, 255, 0.88) 0%, rgba(255, 255, 255, 0) 54%),
    radial-gradient(circle at 16% 4%, rgba(255, 255, 255, 0.95) 0%, rgba(255, 255, 255, 0) 58%),
    radial-gradient(circle at 92% 96%, rgba(31, 82, 189, 0.55) 0%, rgba(31, 82, 189, 0) 62%),
    linear-gradient(140deg, #eaf7ff 0%, #9fd3fd 34%, #54a2f5 64%, #2f74e6 86%, #1f52bd 100%);
}
/* 星网点阵 */
.hero-net {
  position: absolute;
  left: 0;
  top: 0;
  right: 0;
  bottom: 0;
  background-image: radial-gradient(rgba(255, 255, 255, 0.75) 1.5px, rgba(255, 255, 255, 0) 1.5px);
  background-size: 22px 22px;
  opacity: 0.5;
}
/* 斜向光带 */
.hero-beam {
  position: absolute;
  top: -32%;
  left: 52%;
  width: 46%;
  height: 172%;
  transform: rotate(16deg);
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.5) 0%, rgba(214, 246, 255, 0.22) 52%, rgba(255, 255, 255, 0) 88%);
}
/* 光环 */
.hero-ring {
  position: absolute;
  border-radius: 50%;
  border: 1px solid rgba(255, 255, 255, 0.45);
}
.ring-lg {
  width: 520rpx;
  height: 520rpx;
  right: -170rpx;
  top: -120rpx;
}
.ring-md {
  width: 300rpx;
  height: 300rpx;
  left: -110rpx;
  bottom: -90rpx;
  border-width: 2px;
}
.ring-sm {
  width: 120rpx;
  height: 120rpx;
  left: 10%;
  top: 12%;
}
/* 底部渐隐：与页面浅色渐变自然衔接 */
.hero-fade {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 180rpx;
  background: linear-gradient(180deg, rgba(220, 235, 255, 0) 0%, rgba(224, 238, 255, 0.85) 62%, #dcebff 100%);
}
.hero-inner {
  position: relative;
  z-index: 1;
  width: 100%;
  /* 保持主视觉内容居中的原有排布（提示条仍按内容宽度收缩） */
  display: flex;
  flex-direction: column;
  align-items: center;
}
.hero-title {
  display: flex;
  align-items: baseline;
  justify-content: center;
}
.t-dark {
  font-size: 72rpx;
  font-weight: 800;
  color: #17255f;
}
.t-grad,
.grad {
  background: linear-gradient(90deg, #2563eb, #06b6d4);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}
.t-grad {
  font-size: 72rpx;
  font-weight: 800;
  margin-left: 8rpx;
}
.hero-line {
  font-size: 44rpx;
  font-weight: 700;
  margin-top: 16rpx;
}
.hero-sub {
  font-size: 28rpx;
  /* 与网页版同色，蓝底上更易读 */
  color: #33477c;
  margin-top: 20rpx;
}
.hero-actions {
  display: flex;
  gap: 20rpx;
  margin-top: 48rpx;
  flex-wrap: wrap;
  justify-content: center;
}
.session-banner {
  margin-top: 40rpx;
  background: rgba(255, 255, 255, 0.82);
  border: 1px solid rgba(37, 99, 235, 0.35);
  border-radius: 999rpx;
  padding: 20rpx 40rpx;
}
.session-title {
  font-size: 26rpx;
  color: #1d4ed8;
  font-weight: 600;
}
.session-sub {
  display: block;
  margin-top: 6rpx;
  font-size: 22rpx;
  color: #64748b;
}

/* 区块 */
.section {
  padding: 48rpx 32rpx;
}
.section-head {
  display: block;
  text-align: center;
  font-size: 40rpx;
  font-weight: 800;
  margin-bottom: 32rpx;
}

/* 应用卡片 */
.app-card {
  background: rgba(255, 255, 255, 0.82);
  border: 1px solid rgba(255, 255, 255, 0.9);
  border-radius: 20rpx;
  box-shadow: 0 12rpx 32rpx rgba(23, 37, 95, 0.08);
  padding: 32rpx;
  margin-bottom: 24rpx;
}
.app-top {
  display: flex;
  align-items: center;
}
.app-icon {
  width: 88rpx;
  height: 88rpx;
  border-radius: 20rpx;
  color: #fff;
  font-size: 40rpx;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-right: 24rpx;
  flex-shrink: 0;
}
.app-icon.blue {
  background: linear-gradient(135deg, #2563eb, #06b6d4);
}
.app-icon.green {
  background: linear-gradient(135deg, #2563eb, #22c55e);
}
.app-icon.purple {
  background: linear-gradient(135deg, #7c3aed, #c084fc);
}
.app-name {
  display: block;
  font-size: 34rpx;
  font-weight: 700;
  color: #17255f;
}
.app-en {
  display: block;
  font-size: 20rpx;
  color: #94a3b8;
  margin-top: 4rpx;
}
.app-slogan {
  display: block;
  font-size: 24rpx;
  color: #475569;
  margin-top: 8rpx;
}
.app-points {
  display: flex;
  flex-wrap: wrap;
  gap: 12rpx;
  margin-top: 24rpx;
}
.point {
  font-size: 22rpx;
  color: #1d4ed8;
  background: rgba(37, 99, 235, 0.08);
  border-radius: 999rpx;
  padding: 8rpx 20rpx;
}
.app-btn {
  margin-top: 28rpx;
}

/* 统一入口 */
.join-box {
  background: rgba(255, 255, 255, 0.88);
  border-radius: 20rpx;
  box-shadow: 0 12rpx 32rpx rgba(23, 37, 95, 0.08);
  padding: 32rpx;
}
.tabs {
  display: flex;
  gap: 16rpx;
  margin-bottom: 32rpx;
}
.tab {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12rpx;
  font-size: 26rpx;
  color: #64748b;
  border: 1px solid #e2e8f0;
  border-radius: 14rpx;
  padding: 20rpx 0;
}
.tab.active {
  border-color: #2563eb;
  color: #1d4ed8;
  background: rgba(37, 99, 235, 0.06);
  font-weight: 600;
}
.tab-badge {
  font-size: 18rpx;
  color: #fff;
  background: linear-gradient(90deg, #22c55e, #06b6d4);
  border-radius: 999rpx;
  padding: 4rpx 14rpx;
}
.tab-badge.blue {
  background: linear-gradient(90deg, #2563eb, #6366f1);
}
.panel-desc {
  display: block;
  font-size: 24rpx;
  color: #475569;
  line-height: 1.7;
}
.emp-hint {
  margin-top: 20rpx;
  background: rgba(37, 99, 235, 0.08);
  border: 1px dashed rgba(37, 99, 235, 0.4);
  border-radius: 12rpx;
  padding: 18rpx 24rpx;
}
.emp-hint-text {
  font-size: 24rpx;
  color: #1d4ed8;
  font-weight: 600;
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
  background: #fff;
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
.panel-link {
  display: block;
  text-align: center;
  font-size: 24rpx;
  color: #1d4ed8;
  margin-top: 28rpx;
}
.method-tabs {
  display: flex;
  gap: 12rpx;
  margin-bottom: 28rpx;
}
.method {
  flex: 1;
  text-align: center;
  font-size: 24rpx;
  color: #64748b;
  border: 1px solid #e2e8f0;
  border-radius: 999rpx;
  padding: 14rpx 0;
  background: #fff;
}
.method.active {
  border-color: #2563eb;
  color: #1d4ed8;
  background: rgba(37, 99, 235, 0.06);
  font-weight: 600;
}
.choice-list {
  border-top: 1px dashed #dbeafe;
  margin-top: 28rpx;
  padding-top: 20rpx;
}
.choice-title {
  display: block;
  font-size: 24rpx;
  color: #475569;
  margin-bottom: 12rpx;
}
.choice-row {
  display: flex;
  align-items: center;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 12rpx;
  padding: 20rpx;
  margin-bottom: 12rpx;
}
.choice-info {
  flex: 1;
}
.choice-name {
  display: block;
  font-size: 26rpx;
  font-weight: 600;
  color: #17255f;
}
.choice-meta {
  display: block;
  font-size: 22rpx;
  color: #94a3b8;
  margin-top: 6rpx;
}
.choice-arrow {
  font-size: 40rpx;
  color: #1d4ed8;
}
.password-block {
  border-top: 1px dashed #dbeafe;
  margin-top: 24rpx;
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

/* 按钮 */
.btn {
  border-radius: 999rpx;
  font-size: 28rpx;
  height: 84rpx;
  line-height: 84rpx;
}
.btn.solid {
  background: linear-gradient(90deg, #2563eb, #06b6d4);
  color: #fff;
  box-shadow: 0 8rpx 20rpx rgba(37, 99, 235, 0.25);
}
.btn.deep {
  background: linear-gradient(90deg, #1d4ed8, #0891b2);
  color: #fff;
}
.btn.soft {
  background: rgba(255, 255, 255, 0.75);
  color: #1d4ed8;
  border: 1px solid rgba(37, 99, 235, 0.35);
}
.btn.block {
  display: block;
  width: 100%;
}
.btn[disabled] {
  opacity: 0.6;
}

/* 页脚 */
.footer {
  background: #0d1b3e;
  padding: 64rpx 48rpx 48rpx;
  display: flex;
  flex-direction: column;
  gap: 32rpx;
}
.footer-title {
  display: block;
  color: #fff;
  font-size: 30rpx;
  font-weight: 700;
}
.footer-slogan {
  display: block;
  color: #8fa3c8;
  font-size: 24rpx;
  margin-top: 12rpx;
}
.footer-desc {
  display: block;
  color: #8fa3c8;
  font-size: 22rpx;
  line-height: 1.6;
  margin-top: 12rpx;
}
.footer-contact {
  display: flex;
  flex-direction: column;
  gap: 10rpx;
}
.footer-item {
  color: #8fa3c8;
  font-size: 22rpx;
}
.footer-copy {
  color: #64748b;
  font-size: 20rpx;
  text-align: center;
  margin-top: 16rpx;
}
</style>
