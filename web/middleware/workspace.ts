export default defineNuxtRouteMiddleware(async (to) => {
  const workspaceId = String(to.params.workspaceId || '')
  if (!workspaceId) return navigateTo('/')
  const selection = useWorkspace()
  if (!selection.ready.value) await selection.validate()
  if (!selection.select(workspaceId)) return navigateTo('/')
})
