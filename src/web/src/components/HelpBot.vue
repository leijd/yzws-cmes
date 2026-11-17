<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { permissionKnowledge, quickQuestions, type KnowledgeItem } from '../help/permissionKnowledge'

/** 问题助手：上方为可滚动的聊天记录，底部为多行输入框；常见问题与解答收在右上角「?」打开的 QA 窗口。 */
const props = defineProps<{ open: boolean; platform?: boolean; canManage?: boolean }>()
const emit = defineEmits<{ close: [] }>()

type Scope = 'auto' | 'all' | 'platform' | 'tenant'
interface ChatMessage { id: number; role: 'bot' | 'user'; text: string; hits?: KnowledgeItem[] }

const scopeOptions: { value: Scope; label: string }[] = [
  { value: 'auto', label: '本角色' }, { value: 'all', label: '全部' },
  { value: 'platform', label: '平台管理员' }, { value: 'tenant', label: '制衣厂管理员' },
]
const welcomeText = '你好，我是问题助手。描述遇到的现象或权限提示，我会给出原因与解决步骤；点击右上角「?」可浏览全部常见问题与解答。'

const scope = ref<Scope>('auto')
const qaOpen = ref(false)
const qaQuery = ref('')
const draft = ref('')
const expanded = ref<Record<string, boolean>>({})
const chatBody = ref<HTMLElement | null>(null)
let seq = 1
const messages = ref<ChatMessage[]>([{ id: seq++, role: 'bot', text: welcomeText }])

const audience = computed(() => props.platform ? '平台管理员' : (props.canManage ? '制衣厂管理员' : '当前账号'))

function inScope(item: KnowledgeItem) {
  if (scope.value === 'all') return true
  if (scope.value === 'auto') {
    const role = props.platform ? 'platform' : 'tenant'
    return item.audience === 'both' || item.audience === role
  }
  return item.audience === 'both' || item.audience === scope.value
}

function score(item: KnowledgeItem, q: string) {
  if (!q) return 1
  const terms = q.toLowerCase().split(/\s+/).filter(Boolean)
  const haystack = [item.question, item.category, item.symptom, ...item.keywords, ...item.causes,
    ...item.steps, ...(item.permissions || []).flatMap(p => [p.code, p.name])].join(' ').toLowerCase()
  return terms.reduce((sum, term) => sum + (haystack.includes(term) ? 1 : 0), 0)
}

function search(q: string): KnowledgeItem[] {
  return permissionKnowledge
    .filter(inScope)
    .map(item => ({ item, score: score(item, q) }))
    .filter(row => !q || row.score > 0)
    .sort((a, b) => b.score - a.score)
    .map(row => row.item)
}

const qaResults = computed(() => search(qaQuery.value.trim()))

async function scrollToBottom() {
  await nextTick()
  if (chatBody.value) chatBody.value.scrollTop = chatBody.value.scrollHeight
}

function send(text?: string) {
  const content = (text ?? draft.value).trim()
  if (!content) return
  draft.value = ''
  messages.value.push({ id: seq++, role: 'user', text: content })
  const hits = search(content).slice(0, 3)
  messages.value.push(hits.length
    ? { id: seq++, role: 'bot', text: `为你找到 ${hits.length} 条相关解答，已展开最匹配的一条：`, hits }
    : { id: seq++, role: 'bot', text: '没有找到匹配的解答。可点右上角「?」打开 QA 窗口切换范围，或换个关键词（如 403、角色、模板、登录验证）。' })
  void scrollToBottom()
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Enter' && !event.shiftKey) {
    event.preventDefault()
    send()
  }
}
function toggle(id: string) { expanded.value[id] = !expanded.value[id] }
function clearChat() {
  messages.value = [{ id: seq++, role: 'bot', text: welcomeText }]
  draft.value = ''
  expanded.value = {}
}
function openQa(id?: string) {
  qaOpen.value = true
  if (!id) return
  expanded.value[id] = true
  void nextTick(() => document.querySelector(`[data-kb-id="${id}"]`)?.scrollIntoView({ block: 'start', behavior: 'smooth' }))
}
</script>

<template>
  <aside v-if="open" class="help-bot" role="complementary" aria-label="问题助手">
    <header class="hb-head">
      <div class="hb-avatar">🤖</div>
      <strong class="hb-title">问题助手</strong>
      <button class="hb-icon-button" title="常见问题与解答（QA）" aria-label="打开 QA" @click="openQa()">?</button>
      <button class="hb-close" aria-label="关闭" @click="emit('close')">×</button>
    </header>

    <div ref="chatBody" class="hb-chat">
      <div v-for="msg in messages" :key="msg.id" class="hb-message" :class="msg.role">
        <div class="hb-bubble">{{ msg.text }}</div>
        <div v-if="msg.hits?.length" class="hb-answers">
          <div v-for="(item, idx) in msg.hits" :key="item.id" class="hb-card">
            <button class="hb-question" @click="toggle(item.id)">
              <span class="hb-q-text"><span class="hb-tag">{{ item.category }}</span>{{ item.question }}</span>
              <span class="hb-arrow">{{ idx === 0 || expanded[item.id] ? '−' : '+' }}</span>
            </button>
            <div v-if="idx === 0 || expanded[item.id]" class="hb-answer">
              <div class="hb-section"><strong>现象</strong><p>{{ item.symptom }}</p></div>
              <div class="hb-section"><strong>原因</strong><ul><li v-for="(c, i) in item.causes" :key="i">{{ c }}</li></ul></div>
              <div class="hb-section"><strong>解决步骤</strong><ol><li v-for="(s, i) in item.steps" :key="i">{{ s }}</li></ol></div>
              <div v-if="item.permissions?.length" class="hb-section">
                <strong>相关权限点</strong>
                <div class="hb-perms"><span v-for="p in item.permissions" :key="p.code" class="hb-perm">{{ p.name }} <code>{{ p.code }}</code></span></div>
              </div>
              <button class="button link" @click="openQa(item.id)">在 QA 窗口中查看</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <footer class="hb-compose">
      <textarea v-model="draft" rows="2" placeholder="输入问题、页面名或权限提示；Enter 发送，Shift+Enter 换行" @keydown="onKeydown"></textarea>
      <div class="hb-compose-actions">
        <button class="button secondary small" @click="clearChat()">清空聊天</button>
        <button class="button" :disabled="!draft.trim()" @click="send()">发送</button>
      </div>
    </footer>

    <!-- QA 窗口：通过标题栏「?」图标打开，完整罗列常见问题与解答 -->
    <section v-if="qaOpen" class="hb-qa" aria-label="常见问题与解答">
      <header class="hb-qa-head">
        <strong>常见问题与解答（QA）</strong>
        <span class="hb-qa-role">{{ audience }}</span>
        <button class="hb-close" aria-label="关闭 QA" @click="qaOpen = false">×</button>
      </header>
      <div class="hb-qa-toolbar">
        <div class="hb-scopes">
          <button v-for="opt in scopeOptions" :key="opt.value" class="hb-chip" :class="{ on: scope === opt.value }" @click="scope = opt.value">{{ opt.label }}</button>
        </div>
        <input v-model="qaQuery" class="hb-qa-search" placeholder="检索问题、页面名或权限码，如 物料入库 / project:read">
        <div class="hb-quick">
          <button v-for="q in quickQuestions" :key="q" class="hb-quick-item" @click="qaQuery = q">{{ q }}</button>
        </div>
      </div>
      <div class="hb-qa-list">
        <div v-for="item in qaResults" :key="item.id" :data-kb-id="item.id" class="hb-card">
          <button class="hb-question" @click="toggle(item.id)">
            <span><span class="hb-tag">{{ item.category }}</span>{{ item.question }}</span>
            <span class="hb-arrow">{{ expanded[item.id] ? '−' : '+' }}</span>
          </button>
          <div v-if="expanded[item.id]" class="hb-answer">
            <div class="hb-section"><strong>现象</strong><p>{{ item.symptom }}</p></div>
            <div class="hb-section"><strong>原因</strong><ul><li v-for="(c, i) in item.causes" :key="i">{{ c }}</li></ul></div>
            <div class="hb-section"><strong>解决步骤</strong><ol><li v-for="(s, i) in item.steps" :key="i">{{ s }}</li></ol></div>
            <div v-if="item.permissions?.length" class="hb-section">
              <strong>相关权限点</strong>
              <div class="hb-perms"><span v-for="p in item.permissions" :key="p.code" class="hb-perm">{{ p.name }} <code>{{ p.code }}</code></span></div>
            </div>
            <button class="button link" @click="qaOpen = false; send(item.question)">向助手提问此问题</button>
          </div>
        </div>
        <div v-if="!qaResults.length" class="hb-empty">没有匹配的解答，可切换范围或换个关键词。</div>
      </div>
      <footer class="hb-qa-foot">共 {{ qaResults.length }} / {{ permissionKnowledge.length }} 条 · 仅供排查参考，不改变权限配置</footer>
    </section>
  </aside>
</template>

<style scoped>
.help-bot {
  position: fixed; right: 20px; top: 84px; bottom: 88px; width: 430px; max-width: calc(100vw - 40px);
  display: flex; flex-direction: column; z-index: 90;
  background: #fff; border: 1px solid #dbe7fb; border-radius: 18px; overflow: hidden;
  box-shadow: 0 20px 60px rgba(13, 27, 62, .22);
}
.hb-head { display: flex; align-items: center; gap: 10px; padding: 12px 14px; color: #fff; background: linear-gradient(120deg, #102a56, #2563eb); }
.hb-avatar { width: 34px; height: 34px; flex: none; display: grid; place-items: center; font-size: 19px; border-radius: 11px; background: rgba(255,255,255,.18); }
.hb-title { flex: 1; font-size: 15px; }
.hb-icon-button { width: 26px; height: 26px; flex: none; border: 1px solid rgba(255,255,255,.6); border-radius: 50%; background: rgba(255,255,255,.14); color: #fff; font-size: 14px; font-weight: 700; cursor: pointer; }
.hb-icon-button:hover { background: rgba(255,255,255,.32); }
.hb-close { border: 0; background: transparent; color: #fff; font-size: 22px; cursor: pointer; line-height: 1; padding: 0 4px; }
.hb-chat { flex: 1; min-height: 0; overflow-y: auto; overscroll-behavior: contain; padding: 10px 12px; display: flex; flex-direction: column; gap: 10px; background: #f8fafd; }
.hb-message { flex: 0 0 auto; display: flex; flex-direction: column; gap: 6px; align-items: flex-start; }
.hb-message.user { align-items: flex-end; }
.hb-bubble { max-width: 92%; background: #eaf1ff; color: #17255f; border-radius: 12px 12px 12px 2px; padding: 7px 10px; font-size: 12px; line-height: 1.55; white-space: pre-wrap; word-break: break-word; }
.hb-message.user .hb-bubble { background: #2563eb; color: #fff; border-radius: 12px 12px 2px 12px; }
.hb-answers { width: 100%; display: flex; flex-direction: column; gap: 6px; }
.hb-compose { border-top: 1px solid #eef2f7; padding: 8px 10px; display: flex; flex-direction: column; gap: 6px; }
.hb-compose textarea { width: 100%; resize: vertical; min-height: 56px; max-height: 140px; line-height: 1.5; font-size: 13px; }
.hb-compose-actions { display: flex; justify-content: space-between; align-items: center; }
.hb-card { flex: 0 0 auto; border: 1px solid #e2e8f0; border-radius: 10px; background: #fff; overflow: hidden; }
.hb-question { width: 100%; display: flex; align-items: center; justify-content: space-between; gap: 8px; text-align: left; border: 0; background: transparent; padding: 8px 10px; font-size: 13px; font-weight: 600; line-height: 1.5; color: #17255f; cursor: pointer; }
.hb-q-text { flex: 1; min-width: 0; white-space: normal; word-break: break-word; }
.hb-tag { display: inline-block; margin-right: 6px; padding: 1px 6px; border-radius: 6px; background: #eff6ff; color: #1d4ed8; font-size: 11px; font-weight: 500; }
.hb-arrow { flex: none; color: #64748b; font-size: 16px; }
.hb-answer { padding: 0 10px 10px; font-size: 12px; color: #41537e; line-height: 1.55; word-break: break-word; }
.hb-section { margin-top: 8px; }
.hb-section strong { display: block; color: #17255f; margin-bottom: 4px; }
.hb-section p { margin: 0; }
.hb-section ul, .hb-section ol { margin: 0; padding-left: 18px; }
.hb-perms { display: flex; flex-wrap: wrap; gap: 6px; }
.hb-perm { border: 1px solid #dbe7fb; background: #f8fbff; border-radius: 8px; padding: 3px 7px; font-size: 11px; }
.hb-perm code { color: #2563eb; }
.hb-empty { text-align: center; color: #94a3b8; font-size: 12px; padding: 20px 8px; }
.hb-qa { position: absolute; inset: 0; z-index: 2; display: flex; flex-direction: column; background: #fff; }
.hb-qa-head { display: flex; align-items: center; gap: 10px; padding: 12px 14px; color: #fff; background: linear-gradient(120deg, #102a56, #2563eb); font-size: 14px; }
.hb-qa-head strong { flex: 1; }
.hb-qa-role { font-size: 11px; opacity: .85; }
.hb-qa-toolbar { padding: 8px 10px; border-bottom: 1px solid #eef2f7; display: flex; flex-direction: column; gap: 6px; }
.hb-scopes, .hb-quick { display: flex; flex-wrap: wrap; gap: 4px; }
.hb-chip, .hb-quick-item { border: 1px solid #c7dcff; background: #fff; color: #41537e; border-radius: 999px; padding: 3px 8px; font-size: 12px; line-height: 1.4; cursor: pointer; }
.hb-chip.on { background: #2563eb; border-color: #2563eb; color: #fff; }
.hb-quick-item { border-color: #e2e8f0; color: #64748b; }
.hb-quick-item:hover { border-color: #2563eb; color: #2563eb; }
.hb-qa-search { width: 100%; }
.hb-qa-list { flex: 1; min-height: 0; overflow-y: auto; overscroll-behavior: contain; -webkit-overflow-scrolling: touch; padding: 8px 10px; display: flex; flex-direction: column; gap: 6px; background: #f8fafd; }
.hb-qa-list > .hb-empty { flex: 0 0 auto; }
.hb-qa-foot { padding: 6px 10px; border-top: 1px solid #eef2f7; font-size: 11px; color: #94a3b8; }
/* 固定区不参与压缩，滚动只发生在聊天区 / QA 列表内 */
.hb-head, .hb-compose, .hb-qa-head, .hb-qa-toolbar, .hb-qa-foot { flex: 0 0 auto; }
@media (max-width: 760px) {
  .help-bot { inset: 12px; width: auto; bottom: 76px; top: 12px; }
}
</style>
