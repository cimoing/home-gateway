<script setup lang="ts">
import { onMounted, ref } from 'vue'

const status = ref('正在连接后端...')
const healthy = ref(false)

onMounted(async () => {
  try {
    const response = await fetch('/api/health')
    if (!response.ok) {
      throw new Error(`HTTP ${response.status}`)
    }

    const data: { status: string } = await response.json()
    healthy.value = data.status === 'ok'
    status.value = healthy.value ? '后端服务运行正常' : '后端返回异常状态'
  } catch {
    status.value = '暂时无法连接后端服务'
  }
})
</script>

<template>
  <main>
    <section class="card">
      <p class="eyebrow">HOME GATEWAY</p>
      <h1>项目初始化完成</h1>
      <p class="description">Go、Gin、Vue 3、Vite 和 TypeScript 已准备就绪。</p>
      <div class="status" :class="{ healthy }">
        <span class="indicator" />
        {{ status }}
      </div>
    </section>
  </main>
</template>
