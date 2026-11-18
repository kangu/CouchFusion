// The release manager probes the public listener. Start it once admin is ready,
// and fail the unit if either child exits so systemd restarts the pair.
import { spawn } from 'node:child_process'
import { readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { setTimeout as delay } from 'node:timers/promises'

const adminPort = '4502'
async function launch(directory, target, env) {
  const root = new URL('../' + directory + '/', import.meta.url)
  const inventory = JSON.parse(await readFile(new URL('route-inventory.json', root), 'utf8'))
  if (inventory.target !== target) throw new Error('Wrong release target for ' + directory)
  const child = spawn(process.execPath, ['server/index.mjs'], {
    cwd: fileURLToPath(root), env: { ...process.env, ...env, NITRO_HOST: '127.0.0.1' }, stdio: 'inherit'
  })
  child.once('exit', () => process.exit(1))
}
await launch('admin', 'admin', { NITRO_PORT: adminPort, PUBLIC_SITE_URL: 'https://admin.radustanciu.ro',
  NUXT_PUBLIC_SITE_URL: 'https://admin.radustanciu.ro' })
for (let attempt = 0; ; attempt++) {
  const response = await fetch('http://127.0.0.1:' + adminPort + '/login', {
    redirect: 'manual', signal: AbortSignal.timeout(2000)
  }).catch(() => undefined)
  await response?.body?.cancel()
  if (response && response.status >= 200 && response.status < 400) break
  if (attempt >= 19) throw new Error('Admin login failed its startup check')
  await delay(250)
}
await launch('dist', 'public', {})
