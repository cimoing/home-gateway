<script setup lang="ts">
import type { BTSettings } from './types'

defineProps<{ settings: BTSettings | null }>()
</script>

<template>
  <section class="panel settings-panel">
    <div class="panel-heading">
      <h2>BT 引擎设置</h2>
      <p>以下设置来自 YAML 配置文件，Web 界面不会修改配置。</p>
    </div>
    <dl v-if="settings" class="detail-grid">
      <div><dt>引擎</dt><dd>{{ settings.running ? '运行中' : '未运行' }}</dd></div>
      <div><dt>启用</dt><dd>{{ settings.enabled ? '是' : '否' }}</dd></div>
      <div><dt>监听端口</dt><dd>{{ settings.listenPort }} / TCP + UDP</dd></div>
      <div><dt>默认下载根目录</dt><dd>{{ settings.downloadRoot }}</dd></div>
    </dl>
    <p v-else class="empty-state">正在读取设置…</p>
  </section>
</template>
