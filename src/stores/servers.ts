import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getServers } from '@/api'

export const useServersStore = defineStore('servers', () => {
  const servers = ref<any[]>([])

  async function fetchServers() {
    const result: any = await getServers()
    servers.value = result
  }

  return { servers, fetchServers }
})
