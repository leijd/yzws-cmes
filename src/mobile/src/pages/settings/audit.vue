<template>
  <view class="page">
    <view class="chips">
      <view class="chip" :class="{ on: tab === 'audit' }" @click="tab = 'audit'; loadAudit()">审计日志</view>
      <view class="chip" :class="{ on: tab === 'fields' }" @click="tab = 'fields'; loadFields()">自定义字段</view>
    </view>

    <!-- 审计日志 -->
    <template v-if="tab === 'audit'">
      <view v-if="loading && !logs.length" class="empty">加载中…</view>
      <view v-else-if="!logs.length" class="empty">暂无审计日志</view>
      <view v-for="l in logs" :key="l.id" class="card">
        <view class="row between">
          <view class="grow">
            <text class="title" style="font-size: 26rpx">{{ l.action }} · {{ l.owner_type }}</text>
            <text class="muted">{{ fmtDateTime(l.created_at) }} · 节点 {{ l.created_node }}</text>
            <text v-if="l.reason" class="muted">原因：{{ l.reason }}</text>
          </view>
        </view>
      </view>
    </template>

    <!-- 自定义字段 -->
    <template v-else>
      <view class="row between" style="margin-bottom: 16rpx">
        <text class="section-title" style="margin: 0">自定义字段定义</text>
        <button class="btn btn-solid btn-sm" @click="openCreate">＋ 新增字段</button>
      </view>
      <view v-if="!fields.length" class="empty">暂无自定义字段</view>
      <view v-for="f in fields" :key="f.id" class="card">
        <view class="row between">
          <view class="grow">
            <text class="title" style="font-size: 28rpx">{{ f.label }}</text>
            <text class="muted">{{ f.owner_type }} · {{ f.field_type }} · 键 {{ f.field_key }}</text>
          </view>
          <text class="tag" :class="f.status === 'active' ? 'tag-green' : 'tag-gray'">{{ f.status === "active" ? "启用" : "停用" }}</text>
        </view>
      </view>

      <view v-if="sheet" class="sheet-mask" @click="sheet = false">
        <view class="sheet" @click.stop>
          <view class="title" style="margin-bottom: 24rpx">新增自定义字段</view>
          <view class="field">
            <text class="field-label required">所属对象</text>
            <picker :range="ownerLabels" :value="ownerIndex" @change="ownerIndex = $event.detail.value">
              <view class="picker-box">{{ ownerLabels[ownerIndex] }}</view>
            </picker>
          </view>
          <view class="field">
            <text class="field-label required">字段键</text>
            <input class="input" v-model.trim="form.field_key" placeholder="如 qc_remark" />
          </view>
          <view class="field">
            <text class="field-label required">显示名称</text>
            <input class="input" v-model.trim="form.label" placeholder="如 质检备注" />
          </view>
          <view class="field">
            <text class="field-label">字段类型</text>
            <picker :range="typeLabels" :value="typeIndex" @change="typeIndex = $event.detail.value">
              <view class="picker-box">{{ typeLabels[typeIndex] }}</view>
            </picker>
          </view>
          <view v-if="formError" class="error-text">{{ formError }}</view>
          <button class="btn btn-solid btn-block" :disabled="saving" @click="save">{{ saving ? "保存中…" : "保存" }}</button>
        </view>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { onLoad } from "@dcloudio/uni-app";
import { request } from "../../utils/request";
import { fmtDateTime } from "../../utils/format";

interface AuditLog {
  id: string; request_id: string; actor_id?: string; action: string; owner_type: string;
  owner_id: string; reason?: string; created_node: string; created_at: number;
}
interface CustomField {
  id: string; owner_type: string; field_key: string; label: string; field_type: string; status: string;
}

const tab = ref<"audit" | "fields">("audit");
const loading = ref(false);
const logs = ref<AuditLog[]>([]);
const fields = ref<CustomField[]>([]);
const saving = ref(false);
const sheet = ref(false);
const formError = ref("");
const ownerLabels = ["工单", "项目", "报工", "员工", "产品"];
const ownerValues = ["work_order", "project", "work_report", "employee", "product"];
const ownerIndex = ref(0);
const typeLabels = ["文本", "数字", "日期", "选项"];
const typeValues = ["text", "number", "date", "select"];
const typeIndex = ref(0);
const form = ref({ field_key: "", label: "" });

async function loadAudit() {
  loading.value = true;
  try {
    const res = await request<{ items: AuditLog[] }>("/audit-logs", { silent: true });
    logs.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

async function loadFields() {
  loading.value = true;
  try {
    const res = await request<{ items: CustomField[] }>("/custom-field-definitions", { silent: true });
    fields.value = res.items || [];
  } catch (e) {
    uni.showToast({ title: (e as Error).message || "加载失败", icon: "none" });
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  form.value = { field_key: "", label: "" };
  ownerIndex.value = 0;
  typeIndex.value = 0;
  formError.value = "";
  sheet.value = true;
}

async function save() {
  formError.value = "";
  if (!form.value.field_key || !form.value.label) { formError.value = "字段键与显示名称必填"; return; }
  saving.value = true;
  try {
    await request("/custom-field-definitions", {
      method: "POST",
      data: {
        owner_type: ownerValues[ownerIndex.value],
        field_key: form.value.field_key,
        label: form.value.label,
        field_type: typeValues[typeIndex.value],
      },
      idempotency: true,
    });
    uni.showToast({ title: "字段已创建", icon: "none" });
    sheet.value = false;
    loadFields();
  } catch (e) {
    formError.value = (e as Error).message || "创建失败";
  } finally {
    saving.value = false;
  }
}

onLoad(loadAudit);
</script>

<style scoped>
.sheet-mask { position: fixed; inset: 0; background: rgba(15,23,42,0.45); z-index: 50; display: flex; align-items: flex-end; }
.sheet { background: #fff; border-radius: 24rpx 24rpx 0 0; padding: 32rpx; width: 100%; max-height: 86vh; overflow-y: auto; box-sizing: border-box; }
.error-text { color: #d92d20; font-size: 24rpx; margin-top: 12rpx; }
</style>
