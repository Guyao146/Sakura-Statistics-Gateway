<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import TrendChart from './TrendChart.vue'
import IntegrationGuide from './IntegrationGuide.vue'
import { guideSections } from './guide'
import { api, getStatus, ApiError, message, localDate, rangeDays, type Application, type ApplicationStats, type OverviewStats, type ServiceStatus, type Stats, type Metrics } from './api'
const username = ref('')
const password = ref('')
const user = ref('')
const booting = ref(true)
const loggingIn = ref(false)
const authError = ref('')
const apps = ref<Application[]>([])
const selectedId = ref('')
const selected = ref<Application | null>(null)
const view = ref<'overview' | 'apps' | 'status' | 'guide'>('overview')
const guideSection = ref('')
const stats = ref<Stats | null>(null)
const loading = ref(false)
const error = ref('')
const notice = ref('')
const actionError = ref('')
const busy = ref(false)
const status = ref<ServiceStatus | null>(null)
const end = new Date()
const start = new Date(); start.setDate(start.getDate() - 6)
const from = ref(localDate(start))
const to = ref(localDate(end))
const rangeError = computed(() => !from.value || !to.value || !Number.isFinite(rangeDays(from.value, to.value)) ? '请选择完整日期范围' : rangeDays(from.value, to.value) < 1 ? '开始日期不能晚于结束日期' : rangeDays(from.value, to.value) > 366 ? '日期范围最多 366 天（含首尾）' : '')
const newOpen = ref(false)
const draft = ref({ name: '', type: 'web' as 'web' | 'desktop', origins: '' })
const formError = ref('')
const copied = ref(false)
const snippet = computed(() => selected.value?.type === 'web' ? `<script defer src="${location.origin}/sdk.js" data-app-id="${selected.value.id}" data-endpoint="${location.origin}/api/collect" data-timezone="${stats.value?.timezone || 'Asia/Shanghai'}"><\/script>` : '')
const cards: { key: keyof Metrics; label: string; desc: string }[] = [
  { key: 'active', label: '活跃访客', desc: '任意合法事件，区间去重' }, { key: 'starts', label: '启动次数', desc: '仅桌面 app_start 事件' },
  { key: 'sessions', label: '会话数', desc: '仅网站，按会话最早事件日归属' }, { key: 'page_views', label: '页面浏览', desc: '页面访问次数' }, { key: 'new_visitors', label: '新增访客', desc: '首次访问的访客' },
]
const format = (n?: number) => new Intl.NumberFormat('zh-CN').format(n ?? 0)
const when = (s?: string | null) => s ? (Number.isNaN(Date.parse(s)) ? s : new Date(s).toLocaleString('zh-CN', { hour12: false })) : '暂无事件'
const title = computed(() => view.value === 'guide' ? '接入指南' : selected.value?.name || (view.value === 'apps' ? '应用管理' : view.value === 'status' ? '服务状态' : '数据总览'))
const headingLabel = computed(() => view.value === 'guide' ? 'INTEGRATION GUIDE' : view.value === 'overview' ? 'OVERVIEW' : view.value === 'apps' ? 'APPLICATIONS' : 'SYSTEM HEALTH')
const headingDescription = computed(() => view.value === 'guide' ? '从网页 SDK 到桌面客户端，按步骤完成接入与验收。' : selected.value ? `了解应用的访问表现与使用趋势 · ${selected.value.type === 'web' ? 'Web 应用' : '桌面应用'}` : view.value === 'overview' ? '每一次访问，都是增长的信号。' : view.value === 'apps' ? '管理你的应用，连接每一份数据。' : '查看统计服务与数据库的当前状态。')
const canCreateApp = computed(() => view.value === 'overview' || view.value === 'apps')
const snippetCode = ref<HTMLElement | null>(null)
let generation = 0
let noticeTimer: ReturnType<typeof setTimeout> | undefined
function toast(text: string) { notice.value = text; clearTimeout(noticeTimer); noticeTimer = setTimeout(() => notice.value = '', 3500) }
function expired() { generation++; user.value = ''; password.value = ''; stats.value = null; apps.value = []; selected.value = null; selectedId.value = ''; guideSection.value = ''; newOpen.value = false; loading.value = false; authError.value = '登录已失效，请重新登录' }
async function login() {
  if (!username.value.trim() || !password.value) { authError.value = '请输入用户名和密码'; return }
  loggingIn.value = true; authError.value = ''
  try { await api('/auth/login', { method: 'POST', body: JSON.stringify({ username: username.value.trim(), password: password.value }) }); const me = await api<{ username: string }>('/auth/me'); user.value = me.username; password.value = ''; await navigate('overview') }
  catch (e) { authError.value = message(e) } finally { loggingIn.value = false }
}
async function logout() {
  busy.value = true; actionError.value = ''
  try { await api('/auth/logout', { method: 'POST' }); expired(); authError.value = '' } catch (e) { actionError.value = message(e) } finally { busy.value = false }
}
async function navigate(next: typeof view.value, id = '', section = '') {
  if (busy.value) return
  view.value = next; selectedId.value = next === 'guide' ? '' : id; guideSection.value = next === 'guide' ? section : ''; selected.value = null; stats.value = null; status.value = null; newOpen.value = false; copied.value = false; actionError.value = ''; await load()
}
function openGuide(type: Application['type']) { return navigate('guide', '', guideSections[type]) }
async function load() {
  if (view.value === 'guide') { ++generation; loading.value = false; error.value = ''; actionError.value = ''; return }
  if (rangeError.value && view.value === 'overview') return
  const token = ++generation
  loading.value = true; error.value = ''; actionError.value = ''
  const id = selectedId.value
  const query = `?from=${encodeURIComponent(from.value)}&to=${encodeURIComponent(to.value)}`
  try {
    if (view.value === 'status') {
      const result = await getStatus(); if (token === generation) status.value = result
    } else if (id) {
      const [appResult, statsResult] = await Promise.all([api<{ app: Application }>(`/apps/${encodeURIComponent(id)}`), api<ApplicationStats>(`/apps/${encodeURIComponent(id)}/stats${query}`)])
      if (token === generation) { selected.value = appResult.app; stats.value = statsResult }
    } else {
      const [appResult, statsResult] = await Promise.all([api<{ apps: Application[] }>('/apps'), view.value === 'overview' ? api<OverviewStats>(`/stats/overview${query}`) : Promise.resolve(null)])
      if (token === generation) { apps.value = appResult.apps; stats.value = statsResult }
    }
  } catch (e) { if (token === generation) error.value = message(e) } finally { if (token === generation) loading.value = false }
}
function preset(days: number) { const now = new Date(); to.value = localDate(now); now.setDate(now.getDate() - days + 1); from.value = localDate(now); void load() }
async function toggle(app: Application) {
  busy.value = true; actionError.value = ''
  try { await api(`/apps/${encodeURIComponent(app.id)}`, { method: 'PATCH', body: JSON.stringify({ enabled: !app.enabled }) }); toast(app.enabled ? '应用已停用' : '应用已启用'); await load() }
  catch (e) { actionError.value = message(e) } finally { busy.value = false }
}
async function createApp() {
  formError.value = ''
  const name = draft.value.name.trim()
  if (!name) { formError.value = '请输入应用名称'; return }
  if (new TextEncoder().encode(name).length > 120) { formError.value = '应用名称最多 120 字节（约 40 个中文字符）'; return }
  const origins = [...new Set(draft.value.origins.split(/[\n,，]/).map(s => s.trim()).filter(Boolean))]
  if (origins.length > 50) { formError.value = '允许的来源最多 50 个'; return }
  if (origins.some(origin => new TextEncoder().encode(origin).length > 512)) { formError.value = '每个来源最多 512 字节'; return }
  try {
    for (const origin of origins) { const parsed = new URL(origin); if (!['http:', 'https:'].includes(parsed.protocol) || parsed.origin !== origin) throw new Error('来源须为完整 HTTP(S) origin，例如 https://example.com，不含路径或末尾斜杠') }
  } catch (e) { formError.value = e instanceof TypeError ? '来源地址无效，请填写 https://example.com 形式的地址' : message(e); return }
  busy.value = true
  try {
    const result = await api<{ app: Application }>('/apps', { method: 'POST', body: JSON.stringify({ name, type: draft.value.type, allowed_origins: origins }) })
    apps.value = [...apps.value, result.app]
    newOpen.value = false; draft.value = { name: '', type: 'web', origins: '' }; busy.value = false; toast('应用已创建'); await navigate('overview', result.app.id)
  } catch (e) { formError.value = message(e) } finally { busy.value = false }
}
async function copyText(content: string, code: HTMLElement | null, success = '代码已复制'): Promise<boolean> {
  actionError.value = ''
  try {
    await navigator.clipboard.writeText(content)
    toast(success)
    return true
  } catch {
    if (code) {
      const selection = window.getSelection()
      const range = document.createRange()
      range.selectNodeContents(code)
      selection?.removeAllRanges()
      selection?.addRange(range)
    }
    actionError.value = '无法访问剪贴板，请选中代码手动复制'
    return false
  }
}
async function copy() { copied.value = await copyText(snippet.value, snippetCode.value, '接入代码已复制') }
async function copyGuideCode(content: string, code: HTMLElement) { await copyText(content, code) }
function escapeKey(e: KeyboardEvent) { if (e.key === 'Escape' && !busy.value) newOpen.value = false }
onMounted(async () => {
  window.addEventListener('sakura:unauthorized', expired); window.addEventListener('keydown', escapeKey)
  try { const me = await api<{ username: string }>('/auth/me'); user.value = me.username; await load() }
  catch (e) { if (!(e instanceof ApiError && e.status === 401)) authError.value = message(e) }
  finally { booting.value = false }
})
onBeforeUnmount(() => { window.removeEventListener('sakura:unauthorized', expired); window.removeEventListener('keydown', escapeKey); clearTimeout(noticeTimer) })
</script>

<template>
  <div v-if="booting" class="splash"><span class="brand-mark">S</span><p>正在连接 Sakura…</p></div>
  <main v-else-if="!user" class="login-shell">
    <div class="login-art"><div class="wordmark"><span class="brand-mark">S</span>Sakura <small>STATISTICS</small></div><div><span class="eyebrow">让每一次访问，都有迹可循</span><h1>洞察增长，<br>从此刻开始。</h1><p>在一个安静、清晰的空间里，<br>了解你的应用与访客。</p><div class="art-bars"><i v-for="n in 12" :key="n" :style="{ height: `${25 + (n * 17 % 65)}%`, '--h': `${25 + (n * 17 % 65)}%` }"></i></div></div><small>简单的数据，清晰的方向。</small></div>
    <section class="login-card"><span class="eyebrow">欢迎回来</span><h2>登录控制台</h2><p class="muted">使用管理员账户查看应用统计。</p><form @submit.prevent="login"><label>用户名<input v-model="username" name="username" autocomplete="username" autofocus required placeholder="请输入用户名" /></label><label>密码<input v-model="password" name="password" type="password" autocomplete="current-password" required placeholder="请输入密码" /></label><p v-if="authError" class="alert error" role="alert">{{ authError }}</p><button class="primary full" :disabled="loggingIn">{{ loggingIn ? '正在登录…' : '登录控制台 →' }}</button></form><p class="login-foot">Sakura Statistics · 轻量应用分析</p></section>
  </main>
  <div v-else class="shell">
    <aside class="sidebar"><a href="#" class="wordmark" @click.prevent="navigate('overview')"><span class="brand-mark">S</span><span>Sakura<small>STATISTICS</small></span></a><p class="nav-label">工作空间</p><nav aria-label="主导航"><button :class="{ active: view === 'overview' }" aria-label="数据总览" title="数据总览" @click="navigate('overview')"><span aria-hidden="true">◫</span>数据总览</button><button :class="{ active: view === 'apps' }" aria-label="应用管理" title="应用管理" @click="navigate('apps')"><span aria-hidden="true">▦</span>应用管理<span class="nav-count">{{ apps.length }}</span></button><button :class="{ active: view === 'status' }" aria-label="服务状态" title="服务状态" @click="navigate('status')"><span aria-hidden="true">◉</span>服务状态</button><button :class="{ active: view === 'guide' }" aria-label="接入指南" title="接入指南" @click="navigate('guide')"><span aria-hidden="true">▤</span>接入指南</button></nav><div class="sidebar-bottom"><div class="avatar">{{ user.slice(0, 1).toUpperCase() }}</div><div><strong>{{ user }}</strong><small>管理员</small></div><button class="logout" :disabled="busy" aria-label="退出登录" title="退出登录" @click="logout">↗</button></div></aside>
    <div class="workspace"><header class="topbar"><span>工作空间 <span class="muted">/ {{ view !== 'guide' && selected ? '应用详情' : title }}</span></span><span class="top-tag">SAKURA CONSOLE</span></header><main class="content">
      <div class="page-heading"><div><button v-if="view !== 'guide' && selectedId" class="back" @click="navigate('apps')">← 返回应用列表</button><span v-else class="eyebrow">{{ headingLabel }}</span><h1>{{ title }}</h1><p class="muted">{{ headingDescription }}</p></div><button v-if="canCreateApp" class="primary" @click="newOpen = true; formError = ''">＋ 新建应用</button><button v-else-if="view === 'status'" class="button" :disabled="loading" @click="load">刷新状态</button></div>
      <div v-if="view === 'overview'" class="filter-bar"><div class="date-fields"><label>开始日期<input v-model="from" aria-label="开始日期" type="date" /></label><span>—</span><label>结束日期<input v-model="to" aria-label="结束日期" type="date" /></label><button class="button" :disabled="!!rangeError || loading" @click="load">查询</button></div><div class="presets"><button v-for="days in [7, 30, 90]" :key="days" @click="preset(days)">近 {{ days }} 天</button></div></div>
      <p v-if="rangeError && view === 'overview'" class="alert error" role="alert">{{ rangeError }}</p>
      <p v-if="actionError" class="alert error" role="alert">{{ actionError }}</p>
      <IntegrationGuide v-if="view === 'guide'" :section="guideSection" @copy="copyGuideCode" />
      <div v-else-if="loading" class="loading-panel" role="status"><span class="spinner"></span>正在加载数据…</div>
      <div v-else-if="error" class="empty panel"><h3>暂时无法加载</h3><p role="alert">{{ error }}</p><button class="button" @click="load">重新加载</button></div>
      <template v-else>
        <template v-if="view === 'overview' && stats">
          <div class="metric-grid"><article v-for="(card, index) in cards" :key="card.key" class="metric-card"><div><span>{{ card.label }}</span><span class="metric-icon">{{ ['◉','↗','◷','▤','＋'][index] }}</span></div><strong>{{ format(stats.totals[card.key]) }}</strong><small>{{ card.key === 'active' && !selectedId ? '各应用区间去重人数之和' : card.desc }}</small></article></div>
          <p class="count-note">{{ selectedId ? '应用活跃访客包含任意合法事件的区间去重人数；日活跃按每天去重，不能相加作为区间活跃。' : '全局活跃为各应用任意合法事件的区间去重人数相加，同一访客跨应用可能重复计数；全局日活跃也是各应用当日去重人数之和，不能相加作为区间活跃。' }} 启动次数仅统计桌面 app_start；会话仅统计网站，并归属到该会话最早事件所在日。</p>
          <section class="panel"><div class="section-heading"><div><h2>访问趋势</h2><p>观察每日的访问变化</p></div><span class="badge">{{ stats.timezone || '服务端时区' }}</span></div><TrendChart v-if="stats.daily.length" :daily="stats.daily" /><div v-else class="empty"><h3>这段时间还没有访问数据</h3><p>完成接入后，访客活动将在这里呈现。</p></div></section>
          <template v-if="selected">
            <section class="panel app-meta"><div><span class="badge" :class="selected.enabled ? 'green' : 'gray'">{{ selected.enabled ? '采集中' : '已停用' }}</span><span class="mono">{{ selected.id }}</span></div><div><span class="muted">最近事件 {{ when(stats.last_event_at) }}</span><button class="button" :disabled="busy" @click="toggle(selected)">{{ selected.enabled ? '停用应用' : '启用应用' }}</button></div><p class="muted">允许的来源：{{ selected.allowed_origins?.join('、') || '未配置' }}</p></section>
            <div class="detail-grid"><section class="panel"><div class="section-heading"><h2>版本分布</h2><span class="muted">按版本与平台</span></div><div class="table-wrap"><table><thead><tr><th>版本 / 平台</th><th class="number">活跃访客</th><th class="number">启动次数</th></tr></thead><tbody><tr v-for="v in stats.versions || []" :key="`${v.version}-${v.platform}`"><td>{{ v.version || '未知版本' }}<small class="cell-sub">{{ v.platform }}</small></td><td class="number">{{ format(v.active) }}</td><td class="number">{{ format(v.starts) }}</td></tr><tr v-if="!stats.versions?.length"><td colspan="3" class="table-empty">暂无版本数据</td></tr></tbody></table></div></section><section class="panel"><div class="section-heading"><h2>热门页面</h2><span class="muted">仅路径，不含查询参数</span></div><div class="table-wrap"><table><thead><tr><th>页面路径</th><th class="number">浏览次数</th></tr></thead><tbody><tr v-for="p in stats.pages || []" :key="p.path"><td class="mono path-cell">{{ p.path }}</td><td class="number">{{ format(p.page_views) }}</td></tr><tr v-if="!stats.pages?.length"><td colspan="2" class="table-empty">暂无页面数据</td></tr></tbody></table></div></section></div>
            <section class="panel integration"><div class="section-heading"><div><h2>接入你的应用</h2><p>{{ selected.type === 'web' ? '将以下代码放入网站的 <head> 中，即可自动采集页面访问。' : '桌面应用通过采集协议上报，网页 SDK 仅适用于 Web 应用。' }}</p></div><button v-if="snippet" class="button" @click="copy">{{ copied ? '再次复制' : '复制代码' }}</button></div><pre v-if="snippet"><code ref="snippetCode">{{ snippet }}</code></pre><p v-else class="mono">app_id: {{ selected.id }}</p><p><a class="text-link" :href="`#${guideSections[selected.type]}`" @click.prevent="openGuide(selected.type)">{{ selected.type === 'web' ? '查看完整网页接入指南 →' : '查看完整桌面客户端接入指南 →' }}</a></p><p class="muted">应用 ID 是公开标识，不是 API 密钥或安全凭证。请在服务端配置来源校验、限流和数据校验。</p><p v-if="selected.type === 'web'" class="muted">SDK 默认按 pathname 统计页面；支持 SakuraStats.disable() / enable() / trackPage() / flush()。</p></section>
          </template>
          <section v-else class="panel"><div class="section-heading"><div><h2>应用表现</h2><p>所选日期范围内的各应用数据</p></div><span class="muted">{{ stats.apps?.length || 0 }} 个应用</span></div><div class="table-wrap"><table><thead><tr><th>应用</th><th>状态</th><th class="number">活跃访客</th><th class="number">会话</th><th class="number">页面浏览</th><th>最近事件</th><th></th></tr></thead><tbody><tr v-for="app in stats.apps || []" :key="app.id"><td><button class="text-link" @click="navigate('overview', app.id)">{{ app.name }}</button><small class="cell-sub">{{ app.type === 'web' ? 'Web 应用' : '桌面应用' }}</small></td><td><span class="badge" :class="app.enabled ? 'green' : 'gray'">{{ app.enabled ? '已启用' : '已停用' }}</span></td><td class="number">{{ format(app.active) }}</td><td class="number">{{ format(app.sessions) }}</td><td class="number">{{ format(app.page_views) }}</td><td class="muted">{{ when(app.last_event_at) }}</td><td><button class="text-link" @click="navigate('overview', app.id)">详情 →</button></td></tr><tr v-if="!stats.apps?.length"><td colspan="7" class="table-empty">还没有应用，点击「新建应用」开始接入。</td></tr></tbody></table></div></section>
        </template>
        <template v-if="view === 'apps'"><div v-if="!apps.length" class="panel empty"><h3>第一个应用，从这里开始</h3><p>创建 Web 或桌面应用，汇集你的访问数据。</p><button class="primary" @click="newOpen = true">＋ 新建应用</button></div><div v-else class="apps-grid"><article v-for="app in apps" :key="app.id" class="panel application-card"><div class="app-card-top"><span class="app-icon">{{ app.type === 'web' ? 'W' : 'D' }}</span><span class="badge" :class="app.enabled ? 'green' : 'gray'">{{ app.enabled ? '已启用' : '已停用' }}</span></div><h2>{{ app.name }}</h2><p class="mono muted">{{ app.id }}</p><p class="muted">{{ app.type === 'web' ? 'Web 应用' : '桌面应用' }} · 创建于 {{ app.created_at ? when(app.created_at) : '未知' }}</p><p class="muted">最近事件 {{ when(app.last_event_at) }}</p><div class="app-card-actions"><button class="text-link" @click="navigate('overview', app.id)">查看详情 →</button><button class="button" :disabled="busy" @click="toggle(app)">{{ app.enabled ? '停用' : '启用' }}</button></div></article></div></template>
        <section v-if="view === 'status' && status" class="panel status-panel"><span class="status-orb" :class="{ unhealthy: !['ok', 'healthy', 'up'].includes(status.status.toLowerCase()) }"></span><h2>{{ ['ok','healthy','up'].includes(status.status.toLowerCase()) ? '服务运行正常' : '服务状态需要关注' }}</h2><p class="muted">以下为服务端返回的实时状态</p><dl><div><dt>服务状态</dt><dd>{{ status.status }}</dd></div><div><dt>数据库</dt><dd>{{ status.database }}</dd></div><div><dt>服务端时间</dt><dd>{{ when(status.time) }}</dd></div></dl></section>
      </template>
      <footer>Sakura Statistics<span>轻量记录 · 用心洞察</span></footer>
    </main></div>
    <div v-if="notice" class="toast" role="status">{{ notice }}</div>
    <div v-if="newOpen && canCreateApp" class="modal-backdrop" @click.self="!busy && (newOpen = false)"><section class="modal" role="dialog" aria-modal="true" aria-labelledby="create-title"><div class="section-heading"><div><span class="eyebrow">NEW APPLICATION</span><h2 id="create-title">新建应用</h2></div><button class="close" aria-label="关闭" :disabled="busy" @click="newOpen = false">×</button></div><form @submit.prevent="createApp"><label>应用名称<input v-model="draft.name" autofocus required maxlength="120" placeholder="例如：Sakura 官网" /></label><label>应用类型<select v-model="draft.type"><option value="web">Web 应用</option><option value="desktop">桌面应用</option></select></label><label>允许的来源（allowed_origins）<textarea v-model="draft.origins" rows="3" placeholder="https://example.com&#10;http://localhost:5173"></textarea></label><p class="muted form-hint">每行一个 HTTP(S) origin，不含路径，最多 50 个。Web 应用必须配置网站来源，否则采集会被拒绝；桌面应用无 Origin 时可留空。</p><p v-if="formError" class="alert error" role="alert">{{ formError }}</p><div class="modal-actions"><button type="button" class="button" :disabled="busy" @click="newOpen = false">取消</button><button class="primary" :disabled="busy">{{ busy ? '正在创建…' : '创建应用' }}</button></div></form></section></div>
  </div>
</template>
