export async function assertAdminSession() {
  throw createError({
    statusCode: 401,
    statusMessage: 'Authentication is unavailable in the component preview fixture',
  })
}
