# AgentRQ Frontend Design System

This document outlines the core design principles and UI patterns to ensure consistency across the AgentRQ application.

## 1. General Aesthetic

- **Minimalist & High-Density**: Prioritize information and architectural clarity over decorative elements.
- **Flatter Design**: No elevation (drop shadows) on cards or buttons (except for subtle shadow-sm on parent containers).

## 2. Colors

- **Main Background**: `bg-zinc-50` (soft off-white for structural depth).
- **Workspace Canvas**: `bg-white` (pure white for the main interaction areas).
- **Text Primary**: `text-gray-900` or `text-black` for headers.
- **Text Secondary**: `text-gray-500` or `text-gray-400` for meta-info and descriptions.
- **Borders**: `border-gray-100` or `border-gray-200` for architectural separation.

## 3. Topography & Components

- **Main Action Buttons**:
  - Rounding: `rounded-lg` (sharp and disciplined, not pill-shaped).
  - Font Size: `text-[11px]` font-black uppercase tracking-widest.
  - Colors: `bg-black` for primary, `bg-white` with `border-gray-200` for secondary.
- **Cards**:
  - Rounding: `rounded-xl`.
  - Border: `border-gray-100` (subtle definition).
  - No Hover-Lift: Do not use `hover:-translate-y-*` or shadow increases on hover. Use border color shifts (`hover:border-gray-200`) or subtle background changes instead.
- **Overall Container**:
  - Main component wrapper should use `rounded-xl` with a `border-gray-200`.

## 4. Layout

- **Centering**: The Login page must be perfectly centered on the screen.
- **Dashboard Aligment**: Workspace-specific pages use left-aligned, high-density layouts mirroring the `hasmcp-app` structure.
- **Paddings**: Use consistent horizontal padding (`px-4 py-6 md:px-16 md:pt-8 md:pb-8`) for the main content area inside the rounded container.

## 5. Dialogs & Interactions

- **No Native Modals**: Never use browser `confirm()` or `alert()`.
- **Custom Deletion Flow**: Always use the branded `DeleteModal.vue` component for destructive actions like purging workspaces or deleting tasks.
- **Toast Notifications**: Use the global `useToasts` system for all mission-critical feedback (Success/Error/Info).

## 6. API Data Conventions

- **Naming**: All API data properties must use `camelCase` (e.g., `workspaceId`, `createdAt`).
- **Standard**: Strictly avoid using `snake_case` for any data received from or sent to the backend.

## 7. Application Bootstrap

- **One route table**: routes, the auth guard, the `v-click-outside` directive and the pinia
  setup all live in `src/app.js`, exported as `createAgentRQApp({ history, platform })`.
  `src/main.js` (browser) and `desktop/src/renderer/main.js` are thin bootstraps that call it.
- **Never add a route anywhere else.** The desktop app mounts this same table, so a second
  copy would silently let the two builds drift apart. Add the route in `src/app.js` and both
  applications get it.
- **Platform-specific behavior**: read `usePlatformStore()` (`platform`, `isDesktop`, `isWeb`)
  rather than sniffing the user agent or probing for `window.agentrq`. That store is the
  supported seam for an affordance only one build can offer.
- **Browser-only concerns** — the base path the backend injects, the service worker — belong
  in `src/main.js`, not in the shared factory. The desktop build has its own answers for both.
