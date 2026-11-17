<template>
  <BrandNavBar title="产线管理" />
  <view class="page">
    <view class="row between" style="margin-bottom: 20rpx">
      <view class="title">{{ workshopName ? `${workshopName} · 产线` : "产线管理" }}</view>
      <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新建产线</button>
    </view>
    <view class="chips">
      <view
        v-for="s in statusOptions"
        :key="s.value"
        class="chip"
        :class="{ on: status === s.value }"
        @click="status = s.value; reload()"
      >{{ s.label }}</view>
    </view>

    <view v-if="loading && !items.length" class="empty">加载中…</view>
    <view v-else-if="!items.length" class="empty">暂无产线</view>

    <view v-for="l in items" :key="l.id" class="card">
      <view class="row between">
        <view class="col grow">
          <view class="row gap8">
            <text class="title">{{ l.name }}</text>
            <text class="tag" :class="l.status === 'active' ? 'tag-green' : 'tag-gray'">{{ l.status === "active" ? "启用" : "停用" }}</text>
          </view>
          <text class="muted">编码 {{ l.code }} · {{ l.workshop_name }} · {{ natureText(l.line_nature) }}</text>
        </view>
      </view>
      <view class="row gap8" style="margin-top: 16rpx">
        <text class="muted">在职 {{ l.employee_count }} 人</text>
        <text v-if="l.default_process_segment" class="muted">· 默认工序段 {{ l.default_process_segment }}</text>
      </view>
      <view class="actions">
        <button class="btn btn-outline btn-sm" @click="openEdit(l)">编辑</button>
        <button class="btn btn-outline btn-sm" @click="viewMembers(l)">人员 {{ l.employee_count }}</button>
        <button v-if="l.status === 'active'" class="btn btn-danger btn-sm" @click="disable(l)">停用</button>
      </view>
    </view>

    <!-- 新建/编辑弹层 -->
    <view v-if="editing" class="sheet-mask" @click="editing = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">{{ form.id ? "编辑产线" : "新建产线" }}</view>
        <view v-if="!form.id" class="field">
          <text class="field-label required">所属车间</text>
          <picker :range="workshopNames" :value="workshopIndex" @change="onWorkshopChange">
            <view class="picker-box">{{ workshopNames[workshopIndex] || "请选择车间" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">产线名称</text>
          <input class="input" v-model.trim="form.name" placeholder="如 1# 产线" />
        </view>
        <view v-if="!form.id" class="field">
          <text class="field-label">编码（留空自动生成）</text>
          <input class="input" v-model.trim="form.code" placeholder="如 CX-00001" />
        </view>
        <view class="field">
          <text class="field-label">产线性质</text>
          <picker :range="natureLabels" :value="natureIndex" @change="onNatureChange">
            <view class="picker-box">{{ natureLabels[natureIndex] }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label">默认工序段</text>
          <input class="input" v-model.trim="form.default_process_segment" placeholder="如 车缝（可选）" />
        </view>
        <view v-if="formError" class="error-text">{{ formError }}</view>
        <button class="btn btn-solid btn-block" :disabled="saving" @click="save">
          {{ saving ? "保存中…" : "保存" }}
        </button>
      </view>
    </view>

    <!-- 人员弹层（查看 + 调出 + 调入） -->
    <view v-if="memberPanel" class="sheet-mask" @click="memberPanel = false">
      <view class="sheet" @click.stop>
        <view class="row between" style="margin-bottom: 24rpx">
          <view class="title">本线人员（{{ members.length }}）</view>
          <button v-if="currentLine && currentLine.status === 'active'" class="btn btn-solid btn-sm" @click="openAssign">调入</button>
        </view>
        <view v-if="membersLoading" class="empty">加载中…</view>
        <view v-else-if="!members.length" class="empty">暂无人员</view>
        <view v-for="m in members" :key="m.id" class="list-row">
          <view class="grow">
            <text class="title" style="font-size: 28rpx">{{ m.name }}</text>
            <text class="muted">工号 {{ m.employee_no }} · {{ m.position || "未设置岗位" }}</text>
          </view>
          <text class="tag" :class="m.status === 'active' ? 'tag-green' : 'tag-gray'">{{ m.status === "active" ? "在职" : "停用" }}</text>
          <button class="btn btn-outline btn-sm" style="color: #d92d20" @click="removeMember(m)">调出</button>
        </view>
      </view>
    </view>

    <!-- 调入弹层：全厂在职、不在本线的人员多选 -->
    <view v-if="assignPanel" class="sheet-mask" @click="assignPanel = false">
      <view class="sheet" @click.stop>
        <view class="row between" style="margin-bottom: 24rpx">
          <view class="title">调入「{{ currentLine?.name || "" }}」</view>
          <view v-if="assignPicked.length" class="tag tag-green">已选 {{ assignPicked.length }}</view>
        </view>
        <view class="muted" style="font-size: 24rpx; margin-bottom: 16rpx">勾选不在本线的在职人员，确定后调入本线</view>
        <view v-if="assignLoading" class="empty">加载中…</view>
        <view v-else-if="!assignCandidates.length" class="empty">没有可调入的人员（全厂在职人员均已在本线）</view>
        <view
          v-for="c in assignCandidates"
          :key="c.id"
          class="card person"
          :class="{ picked: assignPicked.includes(c.id) }"
          style="margin-bottom: 12rpx"
          @click="toggleAssign(c.id)"
        >
          <view style="flex: 1; min-width: 0">
            <view style="font-size: 28rpx">
              <text style="font-weight: 600">{{ c.name }}</text>
              <text class="muted" style="margin-left: 12rpx">{{ c.employee_no }}</text>
            </view>
            <view class="muted" style="font-size: 24rpx; margin-top: 4rpx">
              当前：{{ c.workshop_name || "—" }} / {{ c.line_name || "—" }}
            </view>
          </view>
          <view class="check">{{ assignPicked.includes(c.id) ? "✓" : "" }}</view>
        </view>
        <button class="btn btn-solid btn-block" :disabled="!assignPicked.length || assigning" @click="doAssign">
          {{ assigning ? "调入中…" : assignPicked.length ? `确定调入 ${assignPicked.length} 人` : "先勾选要调入的人员" }}
        </button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";

interface Line {
  id: string;
  code: string;
  name: string;
  status: string;
  workshop_id: string;
  workshop_name: string;
  line_nature: string;
  default_process_segment?: string;
  version: number;
  employee_count: number;
}

interface WorkshopOption {
  id: string;
  name: string;
}

interface Member {
  id: string;
  employee_no: string;
  name: string;
  position?: string;
  status: string;
  version: number;
}

interface PersonCandidate {
  id: string;
  employee_no: string;
  name: string;
  version: number;
  line_id?: string | null;
  workshop_name?: string | null;
  line_name?: string | null;
}

const items = ref<Line[]>([]);
const loading = ref(false);
const status = ref("");
const statusOptions = [
  { label: "全部", value: "" },
  { label: "启用", value: "active" },
  { label: "停用", value: "disabled" },
];

const workshops = ref<WorkshopOption[]>([]);
const workshopNames = computed(() => workshops.value.map((w) => w.name));
const workshopIndex = ref(0);

const editing = ref(false);
const saving = ref(false);
const formError = ref("");
const natureLabels = ["固定产线", "柔性产线"];
const natureValues = ["fixed", "flexible"];
const natureIndex = ref(0);
const form = ref({
  id: "",
  workshop_id: "",
  code: "",
  name: "",
  line_nature: "fixed",
  default_process_segment: "",
  remark: "",
  version: 0,
});

const memberPanel = ref(false);
const membersLoading = ref(false);
const members = ref<Member[]>([]);
const currentLine = ref<Line | null>(null);

/* 调入 */
const assignPanel = ref(false);
const assignLoading = ref(false);
const assigning = ref(false);
const assignCandidates = ref<PersonCandidate[]>([]);
const assignPicked = ref<string[]>([]);

const workshopName = ref("");

function natureText(s: string) {
  return s === "flexible" ? "柔性产线" : "固定产线";
}

function onNatureChange(e: { detail: { value: number } }) {
  natureIndex.value = e.detail.value;
  form.value.line_nature = natureValues[e.detail.value] || "fixed";
}

function onWorkshopChange(e: { detail: { value: number } }) {
  workshopIndex.value = e.detail.value;
  form.value.workshop_id = workshops.value[e.detail.value]?.id || "";
}

async function reload() {
  loading.value = true;
  try {
    const q: string[] = [];
    if (status.value) q.push(`status=${status.value}`);
    const presetWorkshopId = form.value.workshop_id || "";
    if (presetWorkshopId) q.push(`workshop_id=${presetWorkshopId}`);
    const res = await request<{ items: Line[] }>(`/lines${q.length ? "?" + q.join("&") : ""}`, { silent: true });
    items.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function loadWorkshops() {
  try {
    const res = await request<{ items: { id: string; name: string; status: string }[] }>("/workshops", { silent: true });
    workshops.value = (res.items || []).filter((w) => w.status === "active").map((w) => ({ id: w.id, name: w.name }));
  } catch {
    workshops.value = [];
  }
}

function openCreate() {
  form.value = { id: "", workshop_id: workshops.value[0]?.id || "", code: "", name: "", line_nature: "fixed", default_process_segment: "", remark: "", version: 0 };
  workshopIndex.value = 0;
  natureIndex.value = 0;
  formError.value = "";
  editing.value = true;
}

function openEdit(l: Line) {
  form.value = {
    id: l.id,
    workshop_id: l.workshop_id,
    code: l.code,
    name: l.name,
    line_nature: l.line_nature || "fixed",
    default_process_segment: l.default_process_segment || "",
    remark: "",
    version: l.version,
  };
  natureIndex.value = natureValues.indexOf(form.value.line_nature);
  formError.value = "";
  editing.value = true;
}

async function save() {
  formError.value = "";
  if (!form.value.name) {
    formError.value = "请填写产线名称";
    return;
  }
  if (!form.value.id && !form.value.workshop_id) {
    formError.value = "请选择所属车间";
    return;
  }
  saving.value = true;
  try {
    if (form.value.id) {
      await request(`/lines/${form.value.id}`, {
        method: "PATCH",
        data: {
          name: form.value.name,
          default_process_segment: form.value.default_process_segment,
          remark: form.value.remark,
          status: "active",
          line_nature: form.value.line_nature,
          version: form.value.version,
        },
      });
    } else {
      await request("/lines", {
        method: "POST",
        data: {
          workshop_id: form.value.workshop_id,
          code: form.value.code || undefined,
          name: form.value.name,
          line_nature: form.value.line_nature,
          default_process_segment: form.value.default_process_segment,
          remark: form.value.remark,
        },
        idempotency: true,
      });
    }
    uni.showToast({ title: "已保存", icon: "none" });
    editing.value = false;
    reload();
  } catch (e) {
    formError.value = (e as Error).message || "保存失败";
  } finally {
    saving.value = false;
  }
}

function disable(l: Line) {
  uni.showModal({
    title: "停用产线",
    content: `确定停用「${l.name}」吗？产线下仍有在职人员时将无法停用。`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/lines/${l.id}/disable`, { method: "POST" });
        uni.showToast({ title: "已停用", icon: "none" });
        reload();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "停用失败", icon: "none" });
      }
    },
  });
}

async function viewMembers(l: Line) {
  currentLine.value = l;
  memberPanel.value = true;
  membersLoading.value = true;
  try {
    const res = await request<{ items: Member[] }>(`/lines/${l.id}/members`, { silent: true });
    members.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    membersLoading.value = false;
  }
}

/* 调出：结束当前主归属并清空员工归属 */
async function removeMember(m: Member) {
  uni.showModal({
    title: "调出人员",
    content: `确定把「${m.name}」调出「${currentLine.value?.name || "本线"}」？`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/people/${encodeURIComponent(m.id)}/org-binding`, {
          method: "PUT",
          data: {
            workshop_org_id: "",
            line_org_id: "",
            team_org_id: "",
            reason: `调出产线「${currentLine.value?.name || ""}」（移动端）`,
            version: m.version,
          },
        });
        uni.showToast({ title: `已调出 ${m.name}`, icon: "success" });
        if (currentLine.value) await viewMembers(currentLine.value);
        reload();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "调出失败", icon: "none" });
      }
    },
  });
}

/* 调入：加载全厂在职人员，排除已在本线者，多选批量加入 */
async function openAssign() {
  const l = currentLine.value;
  if (!l) return;
  assignPanel.value = true;
  assignLoading.value = true;
  assignPicked.value = [];
  try {
    const res = await request<{ items: PersonCandidate[] }>("/people?status=active", { silent: true });
    /* 排除已归属本线的人员；其余（含无归属、其他产线）均可调入 */
    assignCandidates.value = (res.items || []).filter((p) => p.line_id !== l.id);
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
    assignCandidates.value = [];
  } finally {
    assignLoading.value = false;
  }
}

function toggleAssign(id: string) {
  assignPicked.value = assignPicked.value.includes(id)
    ? assignPicked.value.filter((x) => x !== id)
    : [...assignPicked.value, id];
}

async function doAssign() {
  const l = currentLine.value;
  if (!l || !assignPicked.value.length) return;
  assigning.value = true;
  const targets = assignCandidates.value.filter((c) => assignPicked.value.includes(c.id));
  const failures: string[] = [];
  for (const p of targets) {
    try {
      await request(`/people/${encodeURIComponent(p.id)}/org-binding`, {
        method: "PUT",
        data: {
          workshop_org_id: l.workshop_id,
          line_org_id: l.id,
          team_org_id: "",
          reason: `调入产线「${l.name}」（移动端）`,
          version: p.version,
        },
      });
    } catch {
      failures.push(p.name);
    }
  }
  assigning.value = false;
  if (failures.length) {
    uni.showToast({ title: `成功 ${targets.length - failures.length} 人，失败 ${failures.length} 人`, icon: "none" });
  } else {
    uni.showToast({ title: `已调入 ${targets.length} 人`, icon: "success" });
    assignPanel.value = false;
  }
  await viewMembers(l);
  reload();
}

onLoad((query) => {
  const q = (query as Record<string, string>) || {};
  if (q.workshop_id) {
    form.value.workshop_id = q.workshop_id;
    workshopName.value = q.workshop_name ? decodeURIComponent(q.workshop_name) : "";
  }
  loadWorkshops().then(reload);
});
</script>

<style scoped>
.sheet-mask {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.45);
  z-index: 50;
  display: flex;
  align-items: flex-end;
}
.sheet {
  background: #fff;
  border-radius: 24rpx 24rpx 0 0;
  padding: 32rpx;
  width: 100%;
  max-height: 86vh;
  overflow-y: auto;
  box-sizing: border-box;
}
.error-text {
  color: #d92d20;
  font-size: 24rpx;
  margin-top: 12rpx;
}
.list-row { display: flex; align-items: center; gap: 16rpx; padding: 20rpx 0; border-bottom: 1rpx solid #eef1f5; }
.grow { flex: 1; min-width: 0; }
.muted { color: #8a94a6; font-size: 26rpx; display: block; }
.card { background: #fff; border-radius: 16rpx; padding: 20rpx 24rpx; }
.person { display: flex; align-items: center; gap: 16rpx; cursor: pointer; }
.person.picked { border: 2rpx solid #2f7cf6; }
.check { width: 44rpx; height: 44rpx; border-radius: 50%; border: 2rpx solid #c4ccd8; color: #fff; font-size: 28rpx; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.person.picked .check { background: #2f7cf6; border-color: #2f7cf6; }
</style>
