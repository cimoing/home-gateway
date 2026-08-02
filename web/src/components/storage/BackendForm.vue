<script setup lang="ts">
import { reactive, watch } from 'vue'
import type { StorageBackend, StorageType } from './types'

const props = defineProps<{
  initial?: StorageBackend | null
  busy: boolean
}>()
const emit = defineEmits<{
  save: [payload: Record<string, unknown>]
  test: [payload: Record<string, unknown>]
  cancel: []
}>()

const form = reactive({
  name: '',
  type: 'local' as StorageType,
  enabled: true,
  secret: '',
  root: '',
  host: '',
  port: 445,
  share: '',
  username: '',
  domain: '',
  endpoint: '',
  region: 'us-east-1',
  bucket: '',
  prefix: '',
  accessKeyId: '',
  forcePathStyle: true,
})

watch(
  () => props.initial,
  (backend) => {
    if (!backend) {
      form.name = ''
      form.type = 'local'
      form.enabled = true
      form.secret = ''
      form.root = ''
      return
    }
    form.name = backend.name
    form.type = backend.type
    form.enabled = backend.enabled
    form.secret = ''
    const cfg = backend.config || {}
    form.root = String(cfg.root || '')
    form.host = String(cfg.host || '')
    form.port = Number(cfg.port || 445)
    form.share = String(cfg.share || '')
    form.username = String(cfg.username || '')
    form.domain = String(cfg.domain || '')
    form.endpoint = String(cfg.endpoint || '')
    form.region = String(cfg.region || 'us-east-1')
    form.bucket = String(cfg.bucket || '')
    form.prefix = String(cfg.prefix || '')
    form.accessKeyId = String(cfg.accessKeyId || '')
    form.forcePathStyle = Boolean(cfg.forcePathStyle)
  },
  { immediate: true },
)

function payload() {
  const base: Record<string, unknown> = {
    name: form.name,
    type: form.type,
    enabled: form.enabled,
    secret: form.secret,
  }
  if (form.type === 'local') {
    base.config = { root: form.root }
  } else if (form.type === 'smb') {
    base.config = {
      host: form.host,
      port: form.port,
      share: form.share,
      username: form.username,
      domain: form.domain,
    }
  } else {
    base.config = {
      endpoint: form.endpoint,
      region: form.region,
      bucket: form.bucket,
      prefix: form.prefix,
      accessKeyId: form.accessKeyId,
      forcePathStyle: form.forcePathStyle,
    }
  }
  return base
}
</script>

<template>
  <form class="panel settings-form" @submit.prevent="emit('save', payload())">
    <div class="panel-heading">
      <h2>{{ initial ? '编辑存储后端' : '添加存储后端' }}</h2>
      <p>本地路径、Samba（SMB 3+）或 S3 兼容对象存储。</p>
    </div>
    <label>
      <span>名称</span>
      <input v-model="form.name" required maxlength="128" />
    </label>
    <label>
      <span>类型</span>
      <select v-model="form.type" :disabled="!!initial">
        <option value="local">本地文件系统</option>
        <option value="smb">Samba / SMB</option>
        <option value="s3">S3 对象存储</option>
      </select>
    </label>
    <template v-if="form.type === 'local'">
      <label>
        <span>根目录（绝对路径）</span>
        <input v-model="form.root" required placeholder="/data/downloads" />
      </label>
    </template>
    <template v-else-if="form.type === 'smb'">
      <label><span>主机</span><input v-model="form.host" required /></label>
      <label><span>端口</span><input v-model.number="form.port" type="number" min="1" max="65535" /></label>
      <label><span>共享名</span><input v-model="form.share" required /></label>
      <label><span>用户名</span><input v-model="form.username" required /></label>
      <label><span>域（可选）</span><input v-model="form.domain" /></label>
      <label>
        <span>密码{{ initial?.hasSecret ? '（留空保持不变）' : '' }}</span>
        <input v-model="form.secret" type="password" :required="!initial?.hasSecret" />
      </label>
    </template>
    <template v-else>
      <label><span>Endpoint（可选，MinIO 等）</span><input v-model="form.endpoint" placeholder="http://minio:9000" /></label>
      <label><span>Region</span><input v-model="form.region" /></label>
      <label><span>Bucket</span><input v-model="form.bucket" required /></label>
      <label><span>Prefix（可选）</span><input v-model="form.prefix" /></label>
      <label><span>Access Key</span><input v-model="form.accessKeyId" required /></label>
      <label>
        <span>Secret Key{{ initial?.hasSecret ? '（留空保持不变）' : '' }}</span>
        <input v-model="form.secret" type="password" :required="!initial?.hasSecret" />
      </label>
      <label class="checkbox-label">
        <input v-model="form.forcePathStyle" type="checkbox" />
        强制 Path-Style（MinIO 建议开启）
      </label>
    </template>
    <label class="checkbox-label">
      <input v-model="form.enabled" type="checkbox" />
      启用
    </label>
    <div class="file-heading-actions">
      <button type="button" class="secondary-button" :disabled="busy" @click="emit('cancel')">取消</button>
      <button type="button" class="secondary-button" :disabled="busy" @click="emit('test', payload())">
        测试连接
      </button>
      <button type="submit" :disabled="busy">保存</button>
    </div>
  </form>
</template>
