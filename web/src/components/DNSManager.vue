<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api/client'

interface Zone {
  providerZoneId: string
  name: string
  status: string
  lastSyncedAt?: string
}

interface RecordItem {
  id: string
  providerRecordId: string
  type: string
  name: string
  content: string
  ttl: number
  proxied?: boolean
  priority?: number
  data?: Record<string, unknown>
  comment: string
}

const tabs = ['zones', 'records'] as const
type Tab = (typeof tabs)[number]
const activeTab = ref<Tab>('zones')
const loading = ref(true)
const busy = ref(false)
const message = ref('')
const error = ref('')
const zones = ref<Zone[]>([])
const records = ref<RecordItem[]>([])
const selectedZoneName = ref('')
const editingRecordId = ref<string | null>(null)
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
  zones.value.find((zone) => zone.name === selectedZoneName.value),
)
const needsContent = computed(() => !['CAA', 'SRV'].includes(recordForm.type))

onMounted(async () => {
  await loadZones()
  loading.value = false
})

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

async function loadZones() {
  const data = await api<{ zones: Zone[] }>('/api/dns/zones')
  zones.value = data.zones || []
  if (!selectedZoneName.value && zones.value.length) {
    selectedZoneName.value = zones.value[0].name
  }
}

function switchTab(tab: Tab) {
  activeTab.value = tab
  error.value = ''
  message.value = ''
  if (tab === 'records' && selectedZoneName.value) void loadRecords()
}

async function syncZone(zone: Zone) {
  await run(async () => {
    const data = await api<{ records: RecordItem[] }>(
      `/api/dns/zones/${encodeURIComponent(zone.name)}/sync`,
      { method: 'POST' },
    )
    records.value = data.records || []
    await loadZones()
  }, '已从 Cloudflare 刷新记录列表。')
}

async function loadRecords() {
  if (!selectedZoneName.value) {
    records.value = []
    return
  }
  await run(async () => {
    const data = await api<{ records: RecordItem[] }>(
      `/api/dns/zones/${encodeURIComponent(selectedZoneName.value)}/records`,
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
  if (!selectedZoneName.value) return
  const id = editingRecordId.value
  await run(async () => {
    const base = `/api/dns/zones/${encodeURIComponent(selectedZoneName.value)}/records`
    await api(id ? `${base}/${encodeURIComponent(id)}` : base, {
      method: id ? 'PUT' : 'POST',
      body: JSON.stringify(recordPayload()),
    })
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
  const data = record.data || {}
  recordForm.caaFlags = Number(data.flags ?? 0)
  recordForm.caaTag = String(data.tag ?? 'issue')
  recordForm.caaValue = String(data.value ?? '')
  recordForm.srvPriority = Number(data.priority ?? 0)
  recordForm.srvWeight = Number(data.weight ?? 0)
  recordForm.srvPort = Number(data.port ?? 443)
  recordForm.srvTarget = String(data.target ?? '')
}

async function deleteRecord(record: RecordItem) {
  if (!selectedZoneName.value || !confirm(`确定删除 ${record.type} ${record.name}？`)) return
  await run(async () => {
    await api(
      `/api/dns/zones/${encodeURIComponent(selectedZoneName.value)}/records/${encodeURIComponent(record.id)}`,
      { method: 'DELETE' },
    )
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

function contentPreview(record: RecordItem) {
  if (record.content) return record.content
  if (record.data) return JSON.stringify(record.data)
  return '—'
}
</script>

<template>
  <section class="feature-view">
    <nav class="tabs" aria-label="DNS 管理导航">
      <button :class="{ active: activeTab === 'zones' }" @click="switchTab('zones')">域名</button>
      <button :class="{ active: activeTab === 'records' }" @click="switchTab('records')">
        DNS 记录
      </button>
    </nav>

    <p v-if="error" class="notice error-message" role="alert">{{ error }}</p>
    <p v-if="message" class="notice success-message" aria-live="polite">{{ message }}</p>
    <p v-if="loading" class="empty-state">正在加载管理数据…</p>

    <div v-else-if="activeTab === 'zones'" class="workspace">
      <div class="panel list-panel">
        <div class="panel-heading">
          <h2>配置中的域名</h2>
          <p>Zone 与 API Token 来自 YAML（dns.cloudflare）；记录直连 Cloudflare。</p>
        </div>
        <p v-if="!zones.length" class="empty-state">配置文件中尚未声明 dns.cloudflare.zones。</p>
        <article v-for="zone in zones" :key="zone.providerZoneId" class="list-item zone-item">
          <div>
            <strong>{{ zone.name }}</strong>
            <span>
              {{ zone.status }} ·
              {{ zone.lastSyncedAt ? `刷新于 ${new Date(zone.lastSyncedAt).toLocaleString()}` : '尚未刷新' }}
            </span>
          </div>
          <div class="table-actions">
            <button class="small-button" :disabled="busy" @click="syncZone(zone)">刷新</button>
            <button
              class="small-button secondary-button"
              :disabled="busy"
              @click="selectedZoneName = zone.name; switchTab('records')"
            >
              查看
            </button>
          </div>
        </article>
      </div>
    </div>

    <div v-else class="records-layout">
      <div class="record-toolbar">
        <label>
          当前域名
          <select v-model="selectedZoneName" @change="loadRecords">
            <option value="" disabled>请选择域名</option>
            <option v-for="zone in zones" :key="zone.providerZoneId" :value="zone.name">
              {{ zone.name }}
            </option>
          </select>
        </label>
        <button
          v-if="selectedZone"
          class="secondary-button small-button"
          :disabled="busy"
          @click="syncZone(selectedZone)"
        >
          从 Cloudflare 刷新
        </button>
      </div>

      <form v-if="selectedZone" class="panel record-form" @submit.prevent="saveRecord">
        <div class="panel-heading">
          <h2>{{ editingRecordId ? '编辑记录' : '新增记录' }}</h2>
        </div>
        <label>
          类型
          <select v-model="recordForm.type">
            <option v-for="type in ['A', 'AAAA', 'CNAME', 'TXT', 'MX', 'CAA', 'SRV']" :key="type">
              {{ type }}
            </option>
          </select>
        </label>
        <label>名称<input v-model="recordForm.name" placeholder="www.example.com" required /></label>
        <label v-if="needsContent">内容<input v-model="recordForm.content" required /></label>
        <label>TTL<input v-model.number="recordForm.ttl" type="number" min="1" max="86400" required /></label>
        <label
          v-if="['A', 'AAAA', 'CNAME'].includes(recordForm.type)"
          class="checkbox-label"
        >
          <input v-model="recordForm.proxied" type="checkbox" />
          启用 Cloudflare 代理
        </label>
        <label v-if="recordForm.type === 'MX'">
          优先级
          <input v-model.number="recordForm.priority" type="number" min="0" max="65535" />
        </label>
        <template v-if="recordForm.type === 'CAA'">
          <label>Flags<input v-model.number="recordForm.caaFlags" type="number" min="0" max="255" /></label>
          <label>
            Tag
            <select v-model="recordForm.caaTag">
              <option>issue</option>
              <option>issuewild</option>
              <option>iodef</option>
            </select>
          </label>
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
          <button type="submit" :disabled="busy">
            {{ editingRecordId ? '保存修改' : '创建记录' }}
          </button>
          <button
            v-if="editingRecordId"
            class="secondary-button"
            type="button"
            @click="resetRecordForm"
          >
            取消
          </button>
        </div>
      </form>

      <div class="panel records-panel">
        <p v-if="!selectedZone" class="empty-state">请先在配置中声明 zone，并在此选择。</p>
        <p v-else-if="!records.length" class="empty-state">暂无 DNS 记录，可点击刷新从 Cloudflare 拉取。</p>
        <div v-else class="record-table-wrap">
          <table>
            <thead>
              <tr><th>类型</th><th>名称</th><th>内容</th><th>TTL</th><th>操作</th></tr>
            </thead>
            <tbody>
              <tr v-for="record in records" :key="record.id">
                <td><span class="type-badge">{{ record.type }}</span></td>
                <td>{{ record.name }}</td>
                <td class="content-cell">{{ contentPreview(record) }}</td>
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
