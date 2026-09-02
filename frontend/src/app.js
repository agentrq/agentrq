/**
 * The one place the AgentRQ application is assembled.
 *
 * Both bootstraps — the browser's `main.js` and the desktop renderer's — call
 * `createAgentRQApp` rather than wiring their own router. That is what keeps
 * the two builds at parity: there is a single route table, so a view added
 * here appears in the desktop app with no change on that side. The only things
 * a bootstrap decides for itself are the history mode and which platform it is.
 */
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter } from 'vue-router'

import './style.css'
import '@fontsource-variable/inter'

import App from './App.vue'
import { fetchUser } from './api'
import { usePlatformStore } from './stores/platformStore'

export const routes = [
  { path: '/', component: () => import('./views/WorkspaceView.vue') },
  {
    path: '/tasks/:filter',
    component: () => import('./views/TaskListView.vue'),
    children: [
      { path: ':workspaceId/:taskId', component: () => import('./views/TaskDetailView.vue') },
      { path: ':workspaceId/:taskId/instances', component: () => import('./views/ScheduledTaskInstancesView.vue') }
    ]
  },
  {
    path: '/workspaces/:id',
    component: () => import('./views/WorkspaceDetailView.vue'),
    children: [
      { path: 'board', component: () => import('./views/KanbanBoardView.vue') },
      { path: 'analytics', component: () => import('./views/WorkspaceAnalyticsView.vue') },
      { path: 'settings', component: () => import('./views/WorkspaceSettingsView.vue') },
      { path: 'tasks/:taskId', component: () => import('./views/TaskDetailView.vue') },
      { path: 'tasks/:taskId/instances', component: () => import('./views/ScheduledTaskInstancesView.vue') }
    ]
  },
  { path: '/workspaces/:id/tasks/new', component: () => import('./views/TaskFormView.vue') },
  { path: '/workspaces/:id/tasks/:taskId/edit', component: () => import('./views/TaskFormView.vue') },

  { path: '/events', component: () => import('./views/EventsView.vue') },
  { path: '/events/:id', component: () => import('./views/EventDetailView.vue') },

  { path: '/workflows', component: () => import('./views/WorkflowsView.vue') },
  { path: '/workflows/:id', component: () => import('./views/WorkflowDetailView.vue') },

  { path: '/login', component: () => import('./views/LoginView.vue'), meta: { public: true } }
]

/**
 * Global `v-click-outside` directive: calls the bound handler when a click
 * lands anywhere that is not the element or one of its descendants.
 */
export const clickOutside = {
  mounted(el, binding) {
    el._clickOutside = (event) => {
      if (!(el === event.target || el.contains(event.target))) {
        binding.value(event)
      }
    }
    document.body.addEventListener('click', el._clickOutside)
  },
  unmounted(el) {
    document.body.removeEventListener('click', el._clickOutside)
  },
}

/**
 * Navigation guard: every route except those marked `meta.public` requires a
 * signed-in user.
 *
 * A failed lookup is treated the same as no user — if we cannot confirm who is
 * signed in, the login page is the safe destination.
 *
 * `fetchUserFn` is a parameter so the guard can be tested without a network.
 */
export function createAuthGuard(fetchUserFn = fetchUser) {
  return async function authGuard(to, from, next) {
    if (to.meta.public) return next()

    try {
      const user = await fetchUserFn()
      if (!user) return next('/login')
      next()
    } catch {
      next('/login')
    }
  }
}

/**
 * Assemble the application.
 *
 * @param {object} options
 * @param {import('vue-router').RouterHistory} options.history
 *        `createWebHistory` in both builds today — the desktop app is served
 *        from the root of its own origin, so it needs no hash fallback.
 * @param {'web'|'desktop'} [options.platform]
 *        Recorded in the platform store, which is the seam components use to
 *        offer a desktop-only affordance without forking.
 * @param {string} [options.os]
 *        `process.platform` from the desktop shell. Only macOS behaves
 *        differently — it hides the title bar, leaving the page to draw the
 *        window's drag handle — and the browser has no use for it at all.
 * @returns {{ app: import('vue').App, router: import('vue-router').Router, pinia: import('pinia').Pinia }}
 *          Unmounted, so the caller can do platform-specific setup first.
 */
export function createAgentRQApp({ history, platform = 'web', os = '' } = {}) {
  const router = createRouter({ history, routes })
  router.beforeEach(createAuthGuard())

  const pinia = createPinia()
  const app = createApp(App)

  app.directive('click-outside', clickOutside)
  app.use(pinia)
  app.use(router)

  // Passing the pinia instance explicitly: this runs before mount, so there is
  // no active instance for the store to infer.
  usePlatformStore(pinia).setPlatform(platform, os)

  return { app, router, pinia }
}
