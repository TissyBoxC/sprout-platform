import { createApp } from 'vue'

import App from '@/App.vue'
import { createAdminRouter } from '@/app/router'
import { createAdminStore } from '@/app/store'
import { bindAdminSessionStore } from '@/app/sessionBoundary'

/// Creates the admin application with its router and global store.
export function createAdminApp() {
  const app = createApp(App)
  const pinia = createAdminStore()

  bindAdminSessionStore(pinia)
  app.use(pinia)
  app.use(createAdminRouter())

  return app
}
