<script setup lang="ts">
import { onLaunch, onShow, onHide } from "@dcloudio/uni-app";
/* 界面语言固定为简体中文：uni-app 内置的 picker（字典/日期/时间选择）、showModal、
 * showActionSheet 的按钮文案跟随系统语言，英文环境下会显示 Done / Cancel、OK / Cancel。
 * H5 端已由 index.html 预置 UNI_LOCALE + manifest.locale 保证首次渲染即中文，
 * 这里再兜底调用一次 setLocale，覆盖 App 端（跟随系统语言）的情况。 */
function lockChineseLocale() {
  try {
    const api = uni as unknown as { getLocale?: () => string; setLocale?: (locale: string) => void };
    if (api.getLocale?.() !== "zh-Hans") api.setLocale?.("zh-Hans");
  } catch {
    /* 平台不支持时忽略：H5 有 UNI_LOCALE / manifest.locale 兜底 */
  }
}
onLaunch(() => {
  lockChineseLocale();
  console.log("App Launch");
});
onShow(() => {
  console.log("App Show");
});
onHide(() => {
  console.log("App Hide");
});
</script>

<style>
/* uni-app H5 默认把 uni-input 固定为 height:1.4em / min-height:1.4em / overflow:hidden，
 * 页面里为输入框设置的 padding 会被压扁，导致输入框明显比 picker 下拉框矮。
 * 这里全局改为自适应高度，各页面再用 min-height 统一输入框与下拉框的高度。 */
uni-page uni-input {
  height: auto;
  min-height: 0;
  line-height: 1.5;
  overflow: visible;
}

/* ===== 移动端全局工具样式：各业务页面共用，保持一致的移动风格 ===== */
.page {
  min-height: 100vh;
  background: #f5f7fa;
  padding: 24rpx;
  box-sizing: border-box;
}
.card {
  background: #fff;
  border-radius: 16rpx;
  padding: 28rpx;
  margin-bottom: 20rpx;
  box-shadow: 0 4rpx 16rpx rgba(23, 37, 95, 0.04);
}
.row {
  display: flex;
  align-items: center;
}
.between {
  justify-content: space-between;
}
.col {
  display: flex;
  flex-direction: column;
}
.grow {
  flex: 1;
  min-width: 0;
}
.wrap {
  flex-wrap: wrap;
}
.gap8 {
  gap: 16rpx;
}
.title {
  font-size: 30rpx;
  font-weight: 600;
  color: #1e293b;
}
.sub {
  font-size: 24rpx;
  color: #64748b;
  margin-top: 8rpx;
}
.muted {
  font-size: 22rpx;
  color: #94a3b8;
  margin-top: 6rpx;
}
.num {
  font-size: 40rpx;
  font-weight: 700;
  color: #1d4ed8;
}
.section-title {
  font-size: 28rpx;
  font-weight: 700;
  color: #17255f;
  margin: 28rpx 0 16rpx;
}
.tag {
  display: inline-flex;
  align-items: center;
  font-size: 20rpx;
  padding: 4rpx 16rpx;
  border-radius: 999rpx;
  line-height: 1.6;
}
.tag-blue {
  background: rgba(37, 99, 235, 0.08);
  color: #1d4ed8;
}
.tag-green {
  background: #dcfce7;
  color: #15803d;
}
.tag-orange {
  background: #ffedd5;
  color: #c2410c;
}
.tag-red {
  background: #fee2e2;
  color: #b91c1c;
}
.tag-gray {
  background: #f1f5f9;
  color: #64748b;
}
.tag-purple {
  background: #ede9fe;
  color: #6d28d9;
}
.btn {
  border-radius: 14rpx;
  font-size: 28rpx;
  margin: 0;
}
.btn-solid {
  background: linear-gradient(90deg, #2563eb, #06b6d4);
  color: #fff;
}
.btn-outline {
  background: #fff;
  color: #1d4ed8;
  border: 1px solid #2563eb;
}
.btn-ghost {
  background: #f1f5f9;
  color: #475569;
}
.btn-danger {
  background: #fff;
  color: #b91c1c;
  border: 1px solid #fca5a5;
}
.btn-sm {
  font-size: 24rpx;
  padding: 0 24rpx;
  height: 60rpx;
  line-height: 60rpx;
  border-radius: 999rpx;
}
.btn-block {
  width: 100%;
  height: 88rpx;
  line-height: 88rpx;
  border-radius: 999rpx;
  margin-top: 24rpx;
}
.btn[disabled] {
  opacity: 0.5;
}
.input {
  border: 1px solid #e2e8f0;
  border-radius: 12rpx;
  padding: 18rpx 24rpx;
  font-size: 28rpx;
  line-height: 44rpx;
  min-height: 80rpx;
  background: #fff;
  color: #0f172a;
  box-sizing: border-box;
  width: 100%;
}
.field {
  padding: 16rpx 0;
}
.field-label {
  display: block;
  font-size: 24rpx;
  color: #64748b;
  margin-bottom: 10rpx;
}
.field-label.required::after {
  content: " *";
  color: #d92d20;
}
.picker-box {
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
.picker-box .ph {
  color: #94a3b8;
}
.search-bar {
  display: flex;
  gap: 16rpx;
  margin-bottom: 20rpx;
}
.search-bar .input {
  flex: 1;
}
.chips {
  display: flex;
  gap: 12rpx;
  flex-wrap: wrap;
  margin-bottom: 20rpx;
}
.chip {
  font-size: 24rpx;
  color: #64748b;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 999rpx;
  padding: 10rpx 24rpx;
}
.chip.on {
  background: #2563eb;
  color: #fff;
  border-color: #2563eb;
}
.list-row {
  display: flex;
  align-items: center;
  padding: 24rpx 0;
  border-bottom: 1px solid #f1f5f9;
}
.list-row:last-child {
  border-bottom: none;
}
.empty {
  text-align: center;
  color: #94a3b8;
  font-size: 26rpx;
  padding: 80rpx 0;
}
.loadmore {
  text-align: center;
  color: #94a3b8;
  font-size: 24rpx;
  padding: 24rpx 0;
}
.divider {
  border-top: 1px solid #edf2f7;
  margin: 20rpx 0;
}
.actions {
  display: flex;
  gap: 16rpx;
  flex-wrap: wrap;
  margin-top: 24rpx;
}
.kv {
  display: flex;
  justify-content: space-between;
  padding: 12rpx 0;
  font-size: 26rpx;
}
.kv .k {
  color: #94a3b8;
  flex-shrink: 0;
  margin-right: 24rpx;
}
.kv .v {
  color: #1e293b;
  text-align: right;
}
.kv .v.strong {
  font-weight: 600;
}
</style>
