<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

interface Credential {
  id: number
  name: string
  tokenHint: string
  updatedAt: string
}

interface Zone {
  id: number
  credentialId: number
  name: string
  status: string
  lastSyncedAt?: string
}

interface RecordItem {
  id: number
  type: string
  name: string
  content: string
  ttl: number
  proxied?: boolean
  priority?: number
  dataJson: string
  comment: string
}

const props = defineProps<{ userName: string }>()
const emit = defineEmits<{ logout: []; sessionExpired: [] }>()

const tabs = ['credentials', 'zones', 'records'] as const
type Tab = (typeof tabs)[number]
const activeTab = ref<Tab>('credentials')
const loading = ref(true)
const busy = ref(false)
const message = ref('')
const error = ref('')
const credentials = ref<Credential[]>([])
const zones = ref<Zone[]>([])
const records = ref<RecordItem[]>([])
const selectedZoneId = ref<number | null>(null)
const editingRecordId = ref<number | null>(null)
const tokenUpdates = reactive<Record<number, string>>({})
const credentialForm = reactive({ name: '', token: '' })
const zoneForm = reactive({ name: '', credentialId: 0 })
const recordForm = reactive({
  type: 'A',
  name: '',
  content: '',
  ttl: 1,
  proxied: false,
  priority: 10,
  comment: '',
  caaFlags: 0,
  caaTag: 'issue',
  caaValue: '',
  srvPriority: 0,
  srvWeight: 0,
  srvPort: 443,
  srvTarget: '',
})

const selectedZone = computed(() =>
  zones.value.find((zone) => zone.id === selectedZoneId.value),
)
const needsContent = computed(() => !['CAA', 'SRV'].includes(recordForm.type))

onMounted(async () => {
  await Promise.all([loadCredentials(), loadZones()])
  loading.value = false
})

async function api<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    ...init,
    headers: init?.body ? { 'Content-Type': 'application/json', ...init.headers } : init?.headers,
  })
  if (response.status === 401) {
    emit('sessionExpired')
    throw new Error('登录已过期，请重新登录。')
  }
  if (!response.ok) {
    const data = (await response.json().catch(() => ({}))) as { error?: string }
    throw new Error(data.error || `请求失败（${response.status}）`)
  }
  return response.status === 204 ? (undefined as T) : ((await response.json()) as T)
}

async function run(action: () => Promise<void>, success = '') {
  error.value = ''
  message.value = ''
  busy.value = true
  try {
    await action()
    message.value = success
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '操作失败'
  } finally {
    busy.value = false
  }
}

async function loadCredentials() {
  const data = await api<{ credentials: Credential[] }>('/api/dns/credentials')
  credentials.value = data.credentials || []
  if (!zoneForm.credentialId && credentials.value.length) {
    zoneForm.credentialId = credentials.value[0].id
  }
}

async function loadZones() {
  const data = await api<{ zones: Zone[] }>('/api/dns/zones')
  zones.value = data.zones || []
  if (!selectedZoneId.value && zones.value.length) {
    selectedZoneId.value = zones.value[0].id
  }
}

function switchTab(tab: Tab) {
  activeTab.value = tab
  error.value = ''
  message.value = ''
  if (tab === 'records' && selectedZoneId.value) void loadRecords()
}

async function createCredential() {
  await run(async () => {
    await api('/api/dns/credentials', {
      method: 'POST',
      body: JSON.stringify(credentialForm),
    })
    credentialForm.name = ''
    credentialForm.token = ''
    await loadCredentials()
  }, 'API Token 已加密保存。')
}

async function updateCredential(item: Credential) {
  const token = tokenUpdates[item.id]
  if (!token) return
  await run(async () => {
    await api(`/api/dns/credentials/${item.id}`, {
      method: 'PUT',
      body: JSON.stringify({ token }),
    })
    tokenUpdates[item.id] = ''
    await loadCredentials()
  }, 'API Token 已更新。')
}

async function deleteCredential(item: Credential) {
  if (!confirm(`确定删除凭据“${item.name}”？`)) return
  await run(async () => {
    await api(`/api/dns/credentials/${item.id}`, { method: 'DELETE' })
    await loadCredentials()
  }, '凭据已删除。')
}

async function createZone() {
  await run(async () => {
    const data = await api<{ zone: Zone }>('/api/dns/zones', {
      method: 'POST',
      body: JSON.stringify(zoneForm),
    })
    zoneForm.name = ''
    await loadZones()
    selectedZoneId.value = data.zone.id
  }, '域名已绑定并完成首次同步。')
}

async function deleteZone(zone: Zone) {
  if (!confirm(`确定移除域名“${zone.name}”及其本地缓存？`)) return
  await run(async () => {
    await api(`/api/dns/zones/${zone.id}`, { method: 'DELETE' })
    selectedZoneId.value = null
    records.value = []
    await loadZones()
  }, '域名已移除。')
}

async function syncZone(zone: Zone) {
  await run(async () => {
    const data = await api<{ records: RecordItem[] }>(`/api/dns/zones/${zone.id}/sync`, {
      method: 'POST',
    })
    records.value = data.records || []
    await loadZones()
  }, '已从 Cloudflare 更新本地缓存。')
}

async function loadRecords() {
  if (!selectedZoneId.value) {
    records.value = []
    return
  }
  await run(async () => {
    const data = await api<{ records: RecordItem[] }>(
      `/api/dns/zones/${selectedZoneId.value}/records`,
    )
    records.value = data.records || []
  })
}

function recordPayload() {
  const payload: Record<string, unknown> = {
    type: recordForm.type,
    name: recordForm.name,
    ttl: Number(recordForm.ttl),
    comment: recordForm.comment,
  }
  if (needsContent.value) payload.content = recordForm.content
  if (['A', 'AAAA', 'CNAME'].includes(recordForm.type)) payload.proxied = recordForm.proxied
  if (recordForm.type === 'MX') payload.priority = Number(recordForm.priority)
  if (recordForm.type === 'CAA') {
    payload.data = {
      flags: Number(recordForm.caaFlags),
      tag: recordForm.caaTag,
      value: recordForm.caaValue,
    }
  }
  if (recordForm.type === 'SRV') {
    payload.data = {
      priority: Number(recordForm.srvPriority),
      weight: Number(recordForm.srvWeight),
      port: Number(recordForm.srvPort),
      target: recordForm.srvTarget,
    }
  }
  return payload
}

async function saveRecord() {
  if (!selectedZoneId.value) return
  const id = editingRecordId.value
  await run(async () => {
    await api(
      `/api/dns/zones/${selectedZoneId.value}/records${id ? `/${id}` : ''}`,
      { method: id ? 'PUT' : 'POST', body: JSON.stringify(recordPayload()) },
    )
    resetRecordForm()
    await loadRecords()
  }, id ? 'DNS 记录已更新。' : 'DNS 记录已创建。')
}

function editRecord(record: RecordItem) {
  editingRecordId.value = record.id
  recordForm.type = record.type
  recordForm.name = record.name
  recordForm.content = record.content
  recordForm.ttl = record.ttl
  recordForm.proxied = Boolean(record.proxied)
  recordForm.priority = record.priority ?? 10
  recordForm.comment = record.comment || ''
  try {
    const data = JSON.parse(record.dataJson || '{}') as Record<string, unknown>
    recordForm.caaFlags = Number(data.flags ?? 0)
    recordForm.caaTag = String(data.tag ?? 'issue')
    recordForm.caaValue = String(data.value ?? '')
    recordForm.srvPriority = Number(data.priority ?? 0)
    recordForm.srvWeight = Number(data.weight ?? 0)
    recordForm.srvPort = Number(data.port ?? 443)
    recordForm.srvTarget = String(data.target ?? '')
  } catch {
    // Keep editable defaults when provider data is malformed.
  }
}

async function deleteRecord(record: RecordItem) {
  if (!selectedZoneId.value || !confirm(`确定删除 ${record.type} ${record.name}？`)) return
  await run(async () => {
    await api(`/api/dns/zones/${selectedZoneId.value}/records/${record.id}`, {
      method: 'DELETE',
    })
    await loadRecords()
  }, 'DNS 记录已删除。')
}

function resetRecordForm() {
  editingRecordId.value = null
  Object.assign(recordForm, {
    type: 'A',
    name: '',
    content: '',
    ttl: 1,
    proxied: false,
    priority: 10,
    comment: '',
    caaFlags: 0,
    caaTag: 'issue',
    caaValue: '',
    srvPriority: 0,
    srvWeight: 0,
    srvPort: 443,
    srvTarget: '',
  })
}
</script>

<template>
  <section class="dashboard">
    <header class="dashboard-header">
      <div>
        <p class="eyebrow">HOME GATEWAY</p>
        <h1>DNS 管理</h1>
        <p class="muted">你好，{{ props.userName }}</p>
      </div>
      <button class="secondary-button header-button" type="button" @click="emit('logout')">
        退出登录
      </button>
    </header>

    <nav class="tabs" aria-label="DNS 管理导航">
      <button :class="{ active: activeTab === 'credentials' }" @click="switchTab('credentials')">
        API 密钥
      </button>
      <button :class="{ active: activeTab === 'zones' }" @click="switchTab('zones')">域名</button>
      <button :class="{ active: activeTab === 'records' }" @click="switchTab('records')">
        DNS 记录
      </button>
    </nav>

    <p v-if="error" class="notice error-message" role="alert">{{ error }}</p>
    <p v-if="message" class="notice success-message" aria-live="polite">{{ message }}</p>
    <p v-if="loading" class="empty-state">正在加载管理数据…</p>

    <div v-else-if="activeTab === 'credentials'" class="workspace">
      <form class="panel form-panel" @submit.prevent="createCredential">
        <div class="panel-heading">
          <h2>添加 API Token</h2>
          <p>保存前会验证 Token，数据库中仅存储 AES-GCM 密文。</p>
        </div>
        <label>凭据名称<input v-model="credentialForm.name" maxlength="128" required /></label>
        <label>Cloudflare API Token<input v-model="credentialForm.token" type="password" required /></label>
        <button type="submit" :disabled="busy">验证并保存</button>
      </form>
      <div class="panel list-panel">
        <div class="panel-heading"><h2>已保存凭据</h2><p>Token 只显示末四位提示。</p></div>
        <p v-if="!credentials.length" class="empty-state">尚未配置 API Token。</p>
        <article v-for="item in credentials" :key="item.id" class="list-item">
          <div><strong>{{ item.name }}</strong><span>•••• {{ item.tokenHint }}</span></div>
          <input v-model="tokenUpdates[item.id]" type="password" placeholder="输入新 Token" />
          <button class="small-button" :disabled="busy || !tokenUpdates[item.id]" @click="updateCredential(item)">更新</button>
          <button class="danger-button small-button" :disabled="busy" @click="deleteCredential(item)">删除</button>
        </article>
      </div>
    </div>

    <div v-else-if="activeTab === 'zones'" class="workspace">
      <form class="panel form-panel" @submit.prevent="createZone">
        <div class="panel-heading"><h2>绑定域名</h2><p>精确查询 Cloudflare Zone 并立即同步。</p></div>
        <label>域名<input v-model="zoneForm.name" placeholder="example.com" required /></label>
        <label>API 凭据
          <select v-model.number="zoneForm.credentialId" required>
            <option :value="0" disabled>请选择</option>
            <option v-for="item in credentials" :key="item.id" :value="item.id">{{ item.name }}</option>
          </select>
        </label>
        <button type="submit" :disabled="busy || !credentials.length">添加并同步</button>
      </form>
      <div class="panel list-panel">
        <div class="panel-heading"><h2>域名</h2><p>Cloudflare 是 DNS 数据的权威源。</p></div>
        <p v-if="!zones.length" class="empty-state">尚未添加域名。</p>
        <article v-for="zone in zones" :key="zone.id" class="list-item zone-item">
          <div>
            <strong>{{ zone.name }}</strong>
            <span>{{ zone.status }} · {{ zone.lastSyncedAt ? `同步于 ${new Date(zone.lastSyncedAt).toLocaleString()}` : '未同步' }}</span>
          </div>
          <button class="small-button" :disabled="busy" @click="syncZone(zone)">同步</button>
          <button class="danger-button small-button" :disabled="busy" @click="deleteZone(zone)">移除</button>
        </article>
      </div>
    </div>

    <div v-else class="records-layout">
      <div class="record-toolbar">
        <label>当前域名
          <select v-model.number="selectedZoneId" @change="loadRecords">
            <option :value="null" disabled>请选择域名</option>
            <option v-for="zone in zones" :key="zone.id" :value="zone.id">{{ zone.name }}</option>
          </select>
        </label>
        <button v-if="selectedZone" class="secondary-button" :disabled="busy" @click="syncZone(selectedZone)">从 Cloudflare 同步</button>
      </div>

      <form v-if="selectedZone" class="panel record-form" @submit.prevent="saveRecord">
        <div class="panel-heading"><h2>{{ editingRecordId ? '编辑记录' : '新增记录' }}</h2></div>
        <label>类型<select v-model="recordForm.type"><option v-for="type in ['A','AAAA','CNAME','TXT','MX','CAA','SRV']" :key="type">{{ type }}</option></select></label>
        <label>名称<input v-model="recordForm.name" placeholder="www.example.com" required /></label>
        <label v-if="needsContent">内容<input v-model="recordForm.content" required /></label>
        <label>TTL<input v-model.number="recordForm.ttl" type="number" min="1" max="86400" required /></label>
        <label v-if="['A','AAAA','CNAME'].includes(recordForm.type)" class="checkbox-label"><input v-model="recordForm.proxied" type="checkbox" />启用 Cloudflare 代理</label>
        <label v-if="recordForm.type === 'MX'">优先级<input v-model.number="recordForm.priority" type="number" min="0" max="65535" /></label>
        <template v-if="recordForm.type === 'CAA'">
          <label>Flags<input v-model.number="recordForm.caaFlags" type="number" min="0" max="255" /></label>
          <label>Tag<select v-model="recordForm.caaTag"><option>issue</option><option>issuewild</option><option>iodef</option></select></label>
          <label>Value<input v-model="recordForm.caaValue" required /></label>
        </template>
        <template v-if="recordForm.type === 'SRV'">
          <label>优先级<input v-model.number="recordForm.srvPriority" type="number" min="0" max="65535" /></label>
          <label>权重<input v-model.number="recordForm.srvWeight" type="number" min="0" max="65535" /></label>
          <label>端口<input v-model.number="recordForm.srvPort" type="number" min="0" max="65535" /></label>
          <label>目标<input v-model="recordForm.srvTarget" required /></label>
        </template>
        <label>备注<input v-model="recordForm.comment" maxlength="500" /></label>
        <div class="form-actions">
          <button type="submit" :disabled="busy">{{ editingRecordId ? '保存修改' : '创建记录' }}</button>
          <button v-if="editingRecordId" class="secondary-button" type="button" @click="resetRecordForm">取消</button>
        </div>
      </form>

      <div class="panel records-panel">
        <p v-if="!selectedZone" class="empty-state">请先添加并选择域名。</p>
        <p v-else-if="!records.length" class="empty-state">本地缓存中没有 DNS 记录。</p>
        <div v-else class="record-table-wrap">
          <table>
            <thead><tr><th>类型</th><th>名称</th><th>内容</th><th>TTL</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-for="record in records" :key="record.id">
                <td><span class="type-badge">{{ record.type }}</span></td>
                <td>{{ record.name }}</td><td class="content-cell">{{ record.content || record.dataJson }}</td>
                <td>{{ record.ttl === 1 ? '自动' : record.ttl }}</td>
                <td class="table-actions">
                  <button class="small-button" @click="editRecord(record)">编辑</button>
                  <button class="danger-button small-button" @click="deleteRecord(record)">删除</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </section>
</template>
