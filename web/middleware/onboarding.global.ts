export default defineNuxtRouteMiddleware(async (to) => {
  if (to.path === '/getting-started') return
  const onboarding = useOnboarding()
  try {
    await onboarding.load()
    if (onboarding.autoOpen.value) return navigateTo('/getting-started')
  } catch {
    // Onboarding must never make the existing workbench unavailable.
  }
})
