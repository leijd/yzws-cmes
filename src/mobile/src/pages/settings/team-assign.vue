<template>
  <BrandNavBar title="生产组调人" />
  <view class="page">
    <view v-if="!permsReady" class="empty">加载中…</view>
    <view v-else-if="!canWrite" class="empty">需要「维护生产组」权限（team:write）才能调整人员归属，请联系厂管理员授权。</view>
    <template v-else>
      <!-- 位置级联：车间 → 产线 → 班组 -->
      <view class="card">
        <view class="field">
          <text class="field-label required">车间</text>
          <picker :range="workshops" range-key="name" @change="onWorkshopChange">
            <view class="picker-box">{{ currentWorkshop?.name || "请选择车间" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label required">生产线</text>
          <picker :range="lines" range-key="name" :disabled="!workshopId" @change="onLineChange">
            <view class="picker-box">{{ currentLine?.name || (workshopId ? "请选择生产线" : "请先选择车间") }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label">班组（可选）</text>
          <picker :range="teamOptions" range-key="name" :disabled="!lineId" @change="onTeamChange">
            <view class="picker-box">{{ currentTeamName }}</view>
          </picker>
        </view>
        <view v-if="lineId" class="field" style="margin-bottom: 0">
          <text class="field-label"></text>
          <text class="link" @click="openTeamSheet">＋ 新建班组</text>
        </view>
        <view v-if="lineId" class="muted row between">
          <text>目标位置已有 {{ targetMembers.length }} 人{{ teamId ? "（本班组）" : "（整条产线）" }}</text>
          <text class="link" @click="openMembers">{{ teamId ? "查看组员 ›" : "查看产线人员 ›" }}</text>
        </view>
      </view>

      <!-- 全厂在职人员多选 -->
      <view class="row between" style="margin: 28rpx 0 12rpx">
        <view class="title">选择要调入的人员</view>
        <view v-if="picked.length" class="tag tag-green">已选 {{ picked.length }}</view>
      </view>
      <view v-if="loading && !people.length" class="empty">加载中…</view>
      <view v-else-if="!people.length" class="empty">暂无在职人员，请先在「员工信息」建档</view>
      <view
        v-for="p in people"
        :key="p.id"
        class="card person"
        :class="{ picked: picked.includes(p.id) }"
        @click="toggle(p.id)"
      >
        <view style="flex: 1; min-width: 0">
          <view style="font-size: 28rpx">
            <text style="font-weight: 600">{{ p.name }}</text>
            <text class="muted" style="margin-left: 12rpx">{{ p.employee_no }}</text>
          </view>
          <view class="muted" style="margin-top: 4rpx">
            当前：{{ p.workshop_name || "—" }} / {{ p.line_name || "—" }} / {{ p.team_name || "—" }}
          </view>
        </view>
        <view class="check">{{ picked.includes(p.id) ? "✓" : "" }}</view>
      </view>

      <button class="btn btn-solid btn-block" style="margin-top: 36rpx" :disabled="!canAssign" @click="assign">
        {{ saving ? "调入中…" : picked.length ? `把选中 ${picked.length} 人调入此处` : "先勾选要调入的人员" }}
      </button>
      <view class="muted" style="margin-top: 16rpx; text-align: center">
        调入会记录换线历史并更新人员当前归属；车间与生产线必选，班组可不指定。
      </view>
    </template>

    <!-- 组员弹层（查看 + 调出） -->
    <view v-if="membersSheet" class="sheet-mask" @click="membersSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">
          {{ teamId ? "组员" : "产线人员" }} · {{ currentTargetName }}（{{ members.length }} 人）
        </view>
        <view v-if="membersBusy" class="empty">加载中…</view>
        <view v-else-if="!members.length" class="empty">暂无组员</view>
        <view v-for="m in members" :key="m.id" class="card person" style="margin-bottom: 12rpx">
          <view style="flex: 1; min-width: 0">
            <view style="font-size: 28rpx">
              <text style="font-weight: 600">{{ m.name }}</text>
              <text class="muted" style="margin-left: 12rpx">{{ m.employee_no }}</text>
            </view>
          </view>
          <button class="btn btn-outline btn-sm" style="color: #d92d20" @click="removeMember(m)">调出</button>
        </view>
        <button class="btn btn-block" style="margin-top: 8rpx" @click="membersSheet = false">关闭</button>
      </view>
    </view>

    <!-- 新建班组弹层 -->
    <view v-if="teamSheet" class="sheet-mask" @click="teamSheet = false">
      <view class="sheet" @click.stop>
        <view class="title" style="margin-bottom: 24rpx">新建班组（挂在当前生产线）</view>
        <view class="field">
          <text class="field-label required">班组名称</text>
          <input class="input" v-model.trim="teamForm.name" placeholder="如 一组 / 缝纫A班" />
        </view>
        <view class="field">
          <text class="field-label">编码（留空自动生成）</text>
          <input class="input" v-model.trim="teamForm.code" placeholder="如 BZ-00001" />
        </view>
        <view class="field">
          <text class="field-label">组长（可选，须为管理人员）</text>
          <picker :range="managerOptions" @change="onManagerChange">
            <view class="picker-box">{{ teamForm.managerLabel || "暂不指定" }}</view>
          </picker>
        </view>
        <view class="field">
          <text class="field-label">备注</text>
          <input class="input" v-model.trim="teamForm.remark" placeholder="可选" />
        </view>
        <view v-if="teamError" class="error-text">{{ teamError }}</view>
        <button class="btn btn-solid btn-block" :disabled="teamSaving" @click="saveTeam">
          {{ teamSaving ? "创建中…" : "创建班组" }}
        </button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { auth } from "../../store/auth";

interface OrgItem { id: string; name: string; status?: string }
interface Person {
  id: string; employee_no: string; name: string; status: string; version: number; person_type?: string;
  position?: string | null;
  workshop_name?: string | null; line_name?: string | null; team_name?: string | null;
}

const permsReady = ref(false);
const canWrite = computed(() => auth.hasPerm("team:write"));

const workshops = ref<OrgItem[]>([]);
const workshopId = ref("");
const lines = ref<OrgItem[]>([]);
const lineId = ref("");
const teams = ref<OrgItem[]>([]);
const teamId = ref("");
const people = ref<Person[]>([]);
const picked = ref<string[]>([]);
const loading = ref(false);
const saving = ref(false);

const currentWorkshop = computed(() => workshops.value.find((w) => w.id === workshopId.value));
const currentLine = computed(() => lines.value.find((l) => l.id === lineId.value));
const teamOptions = computed<OrgItem[]>(() => [{ id: "", name: "不指定班组" }, ...teams.value]);
const currentTeamName = computed(() => {
  if (teamId.value) return teams.value.find((t) => t.id === teamId.value)?.name || "选择班组";
  return lineId.value ? "不指定班组" : "请先选择生产线";
});
/* 目标位置当前已有人员（按归属名称匹配，只读参考） */
const targetMembers = computed(() => {
  const ln = currentLine.value?.name;
  if (!ln) return [];
  const tn = teamId.value ? teams.value.find((t) => t.id === teamId.value)?.name : null;
  return people.value.filter(
    (p) => p.line_name === ln && (tn === null || tn === undefined || p.team_name === tn || (!p.team_name && !tn))
  );
});
const canAssign = computed(
  () => canWrite.value && !!workshopId.value && !!lineId.value && picked.value.length > 0 && !saving.value
);

onLoad(async () => {
  try { await auth.fetchPermissions(); } catch { /* 权限拉取失败按无权限展示 */ }
  permsReady.value = true;
  if (!canWrite.value) return;
  loading.value = true;
  try {
    const w = await request<{ items: OrgItem[] }>("/workshops", { silent: true });
    workshops.value = (w.items || []).filter((x) => (x as OrgItem & { status?: string }).status !== "disabled");
    await loadPeople();
  } finally {
    loading.value = false;
  }
});

async function loadPeople() {
  try {
    const r = await request<{ items: Person[] }>("/people?status=active", { silent: true });
    people.value = r.items || [];
  } catch {
    people.value = [];
  }
}

async function onWorkshopChange(e: { detail: { value: number } }) {
  workshopId.value = workshops.value[e.detail.value]?.id || "";
  lineId.value = "";
  teamId.value = "";
  lines.value = [];
  teams.value = [];
  if (workshopId.value) {
    const r = await request<{ items: OrgItem[] }>(
      `/lines?workshop_id=${encodeURIComponent(workshopId.value)}&status=active`,
      { silent: true }
    );
    lines.value = r.items || [];
  }
}

async function onLineChange(e: { detail: { value: number } }) {
  lineId.value = lines.value[e.detail.value]?.id || "";
  teamId.value = "";
  teams.value = [];
  if (lineId.value) {
    const r = await request<{ items: OrgItem[] }>(
      `/lines/${encodeURIComponent(lineId.value)}/teams`,
      { silent: true }
    );
    teams.value = r.items || [];
  }
}

function onTeamChange(e: { detail: { value: number } }) {
  const idx = e.detail.value;
  teamId.value = idx === 0 ? "" : teams.value[idx - 1]?.id || "";
}

/* ===== 新建班组 ===== */
const teamSheet = ref(false);
const teamSaving = ref(false);
const teamError = ref("");
const teamForm = ref({ name: "", code: "", managerId: "", managerLabel: "", remark: "" });
const managers = computed(() => people.value.filter((p) => p.person_type === "manager"));
const managerOptions = computed(() => ["暂不指定", ...managers.value.map((p) => `${p.name}（${p.employee_no}）`)]);

function openTeamSheet() {
  if (!lineId.value) {
    uni.showToast({ title: "请先选择生产线", icon: "none" });
    return;
  }
  teamForm.value = { name: "", code: "", managerId: "", managerLabel: "", remark: "" };
  teamError.value = "";
  teamSheet.value = true;
}
function onManagerChange(e: { detail: { value: number } }) {
  const idx = e.detail.value;
  teamForm.value.managerId = idx === 0 ? "" : managers.value[idx - 1]?.id || "";
  teamForm.value.managerLabel = managerOptions.value[idx] || "";
}
async function saveTeam() {
  if (!teamForm.value.name) {
    teamError.value = "班组名称必填";
    return;
  }
  teamSaving.value = true;
  teamError.value = "";
  try {
    const r = await request<{ id: string; code: string }>(
      `/lines/${encodeURIComponent(lineId.value)}/teams`,
      {
        method: "POST",
        data: {
          name: teamForm.value.name,
          code: teamForm.value.code || undefined,
          manager_employee_id: teamForm.value.managerId || undefined,
          remark: teamForm.value.remark,
        },
      }
    );
    uni.showToast({ title: `班组已创建（${r.code}）`, icon: "success" });
    teamSheet.value = false;
    const t = await request<{ items: OrgItem[] }>(
      `/lines/${encodeURIComponent(lineId.value)}/teams`,
      { silent: true }
    );
    teams.value = t.items || [];
    teamId.value = r.id;
  } catch (e) {
    teamError.value = (e as Error).message || "创建失败";
  } finally {
    teamSaving.value = false;
  }
}

function toggle(id: string) {
  picked.value = picked.value.includes(id)
    ? picked.value.filter((x) => x !== id)
    : [...picked.value, id];
}

/* ===== 组员/产线人员管理（查看 + 调出）：选定班组按班组、否则按整条产线 ===== */
const membersSheet = ref(false);
const members = ref<Person[]>([]);
const membersBusy = ref(false);
/* 当前查看粒度的显示名：班组名或产线名 */
const currentTargetName = computed(() =>
  teamId.value
    ? (teams.value.find((t) => t.id === teamId.value)?.name || "本班组")
    : (lines.value.find((l) => l.id === lineId.value)?.name || "本产线")
);

async function openMembers() {
  if (!lineId.value) return;
  membersBusy.value = true;
  membersSheet.value = true;
  try {
    /* 选了班组按班组查组员；未选班组按整条产线查人员 */
    const r = teamId.value
      ? await request<{ items: Person[] }>(`/teams/${encodeURIComponent(teamId.value)}/members`, { silent: true })
      : await request<{ items: Person[] }>(`/lines/${encodeURIComponent(lineId.value)}/members`, { silent: true });
    members.value = r.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
    members.value = [];
  } finally {
    membersBusy.value = false;
  }
}

/* 调出（取消分配）：org-binding 全空 = 结束当前主归属并清空员工归属 */
async function removeMember(m: Person) {
  uni.showModal({
    title: "调出人员",
    content: `确定把「${m.name}」调出「${currentTargetName.value}」？`,
    success: async (res) => {
      if (!res.confirm) return;
      try {
        await request(`/people/${encodeURIComponent(m.id)}/org-binding`, {
          method: "PUT",
          data: {
            workshop_org_id: "",
            line_org_id: "",
            team_org_id: "",
            reason: `调出「${currentTargetName.value}」（移动端）`,
            version: m.version,
          },
        });
        uni.showToast({ title: `已调出 ${m.name}`, icon: "success" });
        await openMembers();
        await loadPeople();
      } catch (e) {
        uni.showToast({ title: (e as Error).message || "调出失败", icon: "none" });
      }
    },
  });
}

async function assign() {
  if (!canAssign.value) return;
  saving.value = true;
  const targets = people.value.filter((x) => picked.value.includes(x.id));
  const failures: string[] = [];
  for (const p of targets) {
    try {
      await request(`/people/${p.id}/org-binding`, {
        method: "PUT",
        data: {
          workshop_org_id: workshopId.value,
          line_org_id: lineId.value,
          team_org_id: teamId.value,
          reason: "现场调人（移动端）",
          version: p.version,
        },
      });
    } catch {
      failures.push(p.name);
    }
  }
  saving.value = false;
  if (failures.length) {
    uni.showToast({ title: `成功 ${targets.length - failures.length} 人，失败 ${failures.length} 人`, icon: "none" });
  } else {
    uni.showToast({ title: `已调入 ${targets.length} 人`, icon: "success" });
    picked.value = [];
  }
  await loadPeople();
}
</script>

<style scoped>
.page { padding: 24rpx 28rpx 60rpx; }
.card { background: #fff; border-radius: 16rpx; padding: 20rpx 24rpx; margin-bottom: 16rpx; }
.card .field { margin-bottom: 16rpx; }
.card .field:last-child { margin-bottom: 0; }
.title { font-size: 30rpx; font-weight: 600; }
.muted { color: #8a94a6; }
.empty { text-align: center; color: #8a94a6; padding: 80rpx 0; font-size: 26rpx; }
.tag { font-size: 22rpx; padding: 4rpx 16rpx; border-radius: 20rpx; }
.tag-green { background: #e6f7ee; color: #1a9e5c; }
.person { display: flex; align-items: center; gap: 16rpx; cursor: pointer; }
.person.picked { border: 2rpx solid #2f7cf6; }
.check { width: 44rpx; height: 44rpx; border-radius: 50%; border: 2rpx solid #c4ccd8; color: #fff; font-size: 28rpx; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.person.picked .check { background: #2f7cf6; border-color: #2f7cf6; }
.link { color: #2f7cf6; font-size: 26rpx; }
.sheet-mask { position: fixed; inset: 0; background: rgba(15, 23, 42, 0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
