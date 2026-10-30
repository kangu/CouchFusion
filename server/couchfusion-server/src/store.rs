//! sqlite persistence (`/var/lib/couchfusion/control.db`, mode 0600, owned by cfcp).
//! Secrets are stored as SHA-256 hashes only, except the per-start dev control token, which
//! the server must present to the dev server (the database is cfcp-only).

use std::path::Path;
use std::sync::Mutex;

use anyhow::Context;
use rusqlite::{Connection, OptionalExtension, params};

use crate::dto::{DevStatus, JobSnapshot, NeedsRestart};
use crate::util::{sha256_hex, unix_now};

const SESSION_IDLE_SECS: i64 = 7 * 24 * 3600;
const SESSION_ABSOLUTE_SECS: i64 = 30 * 24 * 3600;
const LOGIN_TOKEN_SECS: i64 = 600;
const IDEMPOTENCY_SECS: i64 = 600;

const SCHEMA: &str = r#"
CREATE TABLE IF NOT EXISTS sessions (hash TEXT PRIMARY KEY, created INTEGER NOT NULL, last_seen INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS login_tokens (hash TEXT PRIMARY KEY, expires INTEGER NOT NULL, used INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS jobs (id TEXT PRIMARY KEY, site TEXT, kind TEXT NOT NULL, status TEXT NOT NULL, snapshot TEXT NOT NULL, updated INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS idempotency (key TEXT PRIMARY KEY, job_id TEXT NOT NULL, created INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS dev_sessions (site TEXT PRIMARY KEY, status TEXT NOT NULL, token TEXT NOT NULL, started_at TEXT, needs_restart TEXT);
CREATE TABLE IF NOT EXISTS audit (id INTEGER PRIMARY KEY AUTOINCREMENT, time INTEGER NOT NULL, via TEXT NOT NULL, login TEXT, method TEXT NOT NULL, path TEXT NOT NULL, status INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS trash (job_id TEXT PRIMARY KEY, site TEXT NOT NULL, source TEXT NOT NULL, trash_path TEXT NOT NULL, sha256 TEXT NOT NULL, git TEXT NOT NULL, created INTEGER NOT NULL);
"#;

pub struct Store {
    conn: Mutex<Connection>,
}

/// A trashed component file (the delete job id is the trash id).
#[derive(Debug, Clone)]
pub struct TrashRow {
    pub job_id: String,
    pub site: String,
    pub source: String,
    pub trash_path: String,
    pub sha256: String,
    pub git: String,
    pub created: i64,
    pub restored: Option<i64>,
}

/// Columns added after Phase 1; `ALTER TABLE` fails harmlessly when they already exist.
fn migrate(conn: &Connection) {
    let _ = conn.execute("ALTER TABLE trash ADD COLUMN restored INTEGER", []);
}

const TRASH_COLUMNS: &str = "job_id, site, source, trash_path, sha256, git, created, restored";

fn trash_row(r: &rusqlite::Row<'_>) -> rusqlite::Result<TrashRow> {
    Ok(TrashRow {
        job_id: r.get(0)?,
        site: r.get(1)?,
        source: r.get(2)?,
        trash_path: r.get(3)?,
        sha256: r.get(4)?,
        git: r.get(5)?,
        created: r.get(6)?,
        restored: r.get(7)?,
    })
}

#[derive(Debug, Clone)]
pub struct DevSessionRow {
    pub status: DevStatus,
    pub token: String,
    pub started_at: Option<String>,
    pub needs_restart: Option<NeedsRestart>,
}

impl Store {
    pub fn open(path: &Path) -> anyhow::Result<Self> {
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent).ok();
        }
        let conn = Connection::open(path).with_context(|| format!("opening {}", path.display()))?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let _ = std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600));
        }
        conn.pragma_update(None, "journal_mode", "WAL")?;
        conn.pragma_update(None, "busy_timeout", 5000)?;
        conn.execute_batch(SCHEMA)?;
        migrate(&conn);
        Ok(Self { conn: Mutex::new(conn) })
    }

    #[cfg(test)]
    pub fn memory() -> Self {
        let conn = Connection::open_in_memory().unwrap();
        conn.execute_batch(SCHEMA).unwrap();
        migrate(&conn);
        Self { conn: Mutex::new(conn) }
    }

    fn with<T>(&self, f: impl FnOnce(&Connection) -> rusqlite::Result<T>) -> anyhow::Result<T> {
        let conn = self.conn.lock().map_err(|_| anyhow::anyhow!("store lock poisoned"))?;
        Ok(f(&conn)?)
    }

    // ---- login tokens and sessions ----

    pub fn create_login_token(&self, token: &str) -> anyhow::Result<()> {
        let expires = unix_now() + LOGIN_TOKEN_SECS;
        self.with(|c| {
            c.execute("DELETE FROM login_tokens WHERE expires < ?1 OR used = 1", params![unix_now()])?;
            c.execute("INSERT INTO login_tokens(hash, expires) VALUES (?1, ?2)", params![sha256_hex(token.as_bytes()), expires])
        })?;
        Ok(())
    }

    /// Consumes a login token (single use) and returns a new session token.
    pub fn exchange_login_token(&self, token: &str, session_token: &str) -> anyhow::Result<bool> {
        let hash = sha256_hex(token.as_bytes());
        let now = unix_now();
        self.with(|c| {
            let updated = c.execute(
                "UPDATE login_tokens SET used = 1 WHERE hash = ?1 AND used = 0 AND expires >= ?2",
                params![hash, now],
            )?;
            if updated != 1 {
                return Ok(false);
            }
            c.execute(
                "INSERT INTO sessions(hash, created, last_seen) VALUES (?1, ?2, ?2)",
                params![sha256_hex(session_token.as_bytes()), now],
            )?;
            Ok(true)
        })
    }

    pub fn session_valid(&self, session_token: &str) -> anyhow::Result<bool> {
        let hash = sha256_hex(session_token.as_bytes());
        let now = unix_now();
        self.with(|c| {
            let row: Option<(i64, i64)> = c
                .query_row("SELECT created, last_seen FROM sessions WHERE hash = ?1", params![hash], |r| Ok((r.get(0)?, r.get(1)?)))
                .optional()?;
            let Some((created, last_seen)) = row else { return Ok(false) };
            if now - last_seen > SESSION_IDLE_SECS || now - created > SESSION_ABSOLUTE_SECS {
                c.execute("DELETE FROM sessions WHERE hash = ?1", params![hash])?;
                return Ok(false);
            }
            if now - last_seen > 60 {
                c.execute("UPDATE sessions SET last_seen = ?2 WHERE hash = ?1", params![hash, now])?;
            }
            Ok(true)
        })
    }

    // ---- jobs ----

    pub fn save_job(&self, job: &JobSnapshot) -> anyhow::Result<()> {
        let snapshot = serde_json::to_string(job)?;
        let status = serde_json::to_value(job.status)?.as_str().unwrap_or("").to_string();
        self.with(|c| {
            c.execute(
                "INSERT INTO jobs(id, site, kind, status, snapshot, updated) VALUES (?1, ?2, ?3, ?4, ?5, ?6)
                 ON CONFLICT(id) DO UPDATE SET status = excluded.status, snapshot = excluded.snapshot, updated = excluded.updated",
                params![job.id, job.site_id, job.kind, status, snapshot, unix_now()],
            )
        })?;
        Ok(())
    }

    pub fn load_job(&self, id: &str) -> anyhow::Result<Option<JobSnapshot>> {
        let text: Option<String> = self.with(|c| {
            c.query_row("SELECT snapshot FROM jobs WHERE id = ?1", params![id], |r| r.get(0)).optional()
        })?;
        Ok(match text {
            Some(t) => Some(serde_json::from_str(&t)?),
            None => None,
        })
    }

    /// Jobs left non-terminal by a previous process are marked failed on boot.
    pub fn non_terminal_jobs(&self) -> anyhow::Result<Vec<JobSnapshot>> {
        let rows: Vec<String> = self.with(|c| {
            let mut stmt = c.prepare("SELECT snapshot FROM jobs WHERE status IN ('queued', 'running')")?;
            let rows = stmt.query_map([], |r| r.get::<_, String>(0))?.collect::<rusqlite::Result<Vec<_>>>()?;
            Ok(rows)
        })?;
        Ok(rows.iter().filter_map(|t| serde_json::from_str(t).ok()).collect())
    }

    // ---- idempotency ----

    pub fn idempotent_job(&self, key: &str) -> anyhow::Result<Option<String>> {
        let cutoff = unix_now() - IDEMPOTENCY_SECS;
        self.with(|c| {
            c.execute("DELETE FROM idempotency WHERE created < ?1", params![cutoff])?;
            c.query_row("SELECT job_id FROM idempotency WHERE key = ?1", params![key], |r| r.get(0)).optional()
        })
    }

    pub fn remember_idempotency(&self, key: &str, job_id: &str) -> anyhow::Result<()> {
        self.with(|c| {
            c.execute(
                "INSERT OR REPLACE INTO idempotency(key, job_id, created) VALUES (?1, ?2, ?3)",
                params![key, job_id, unix_now()],
            )
        })?;
        Ok(())
    }

    // ---- dev sessions ----

    pub fn dev_session(&self, site: &str) -> anyhow::Result<Option<DevSessionRow>> {
        self.with(|c| {
            c.query_row(
                "SELECT status, token, started_at, needs_restart FROM dev_sessions WHERE site = ?1",
                params![site],
                |r| {
                    let needs: Option<String> = r.get(3)?;
                    Ok(DevSessionRow {
                        status: DevStatus::parse(&r.get::<_, String>(0)?),
                        token: r.get(1)?,
                        started_at: r.get(2)?,
                        needs_restart: needs.map(|reason| NeedsRestart { reason }),
                    })
                },
            )
            .optional()
        })
    }

    pub fn save_dev_session(&self, site: &str, row: &DevSessionRow) -> anyhow::Result<()> {
        self.with(|c| {
            c.execute(
                "INSERT INTO dev_sessions(site, status, token, started_at, needs_restart) VALUES (?1, ?2, ?3, ?4, ?5)
                 ON CONFLICT(site) DO UPDATE SET status = excluded.status, token = excluded.token,
                   started_at = excluded.started_at, needs_restart = excluded.needs_restart",
                params![
                    site,
                    row.status.as_str(),
                    row.token,
                    row.started_at,
                    row.needs_restart.as_ref().map(|n| n.reason.clone())
                ],
            )
        })?;
        Ok(())
    }

    // ---- audit and trash ----

    pub fn audit(&self, via: &str, login: Option<&str>, method: &str, path: &str, status: u16) {
        let _ = self.with(|c| {
            c.execute(
                "INSERT INTO audit(time, via, login, method, path, status) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
                params![unix_now(), via, login, method, path, status],
            )
        });
    }

    pub fn record_trash(&self, job_id: &str, site: &str, source: &str, trash_path: &str, sha: &str, git: &str) -> anyhow::Result<()> {
        self.with(|c| {
            c.execute(
                "INSERT INTO trash(job_id, site, source, trash_path, sha256, git, created) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)",
                params![job_id, site, source, trash_path, sha, git, unix_now()],
            )
        })?;
        Ok(())
    }

    /// Trash rows of a site, newest first.
    pub fn trash_entries(&self, site: &str) -> anyhow::Result<Vec<TrashRow>> {
        self.with(|c| {
            let mut stmt = c.prepare(&format!("SELECT {TRASH_COLUMNS} FROM trash WHERE site = ?1 ORDER BY created DESC, rowid DESC"))?;
            let rows = stmt.query_map(params![site], trash_row)?.collect::<rusqlite::Result<Vec<_>>>()?;
            Ok(rows)
        })
    }

    pub fn trash_entry(&self, job_id: &str) -> anyhow::Result<Option<TrashRow>> {
        self.with(|c| c.query_row(&format!("SELECT {TRASH_COLUMNS} FROM trash WHERE job_id = ?1"), params![job_id], trash_row).optional())
    }

    /// Marks a trash entry restored (the trashed copy is kept, never unlinked).
    pub fn mark_trash_restored(&self, job_id: &str) -> anyhow::Result<()> {
        self.with(|c| c.execute("UPDATE trash SET restored = ?2 WHERE job_id = ?1", params![job_id, unix_now()]))?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn login_token_is_single_use() {
        let store = Store::memory();
        store.create_login_token("tok").unwrap();
        assert!(store.exchange_login_token("tok", "sess").unwrap());
        assert!(!store.exchange_login_token("tok", "sess2").unwrap());
        assert!(store.session_valid("sess").unwrap());
        assert!(!store.session_valid("sess2").unwrap());
        assert!(!store.exchange_login_token("other", "x").unwrap());
    }

    #[test]
    fn trash_rows() {
        let store = Store::memory();
        store.record_trash("j1", "s", "app/components/content/A.vue", "/t/j1/a", "sha", "untracked").unwrap();
        store.record_trash("j2", "s", "app/components/content/B.vue", "/t/j2/b", "sha", "tracked_clean").unwrap();
        store.record_trash("j3", "other", "app/components/content/C.vue", "/t/j3/c", "sha", "unknown").unwrap();
        let rows = store.trash_entries("s").unwrap();
        assert_eq!(rows.iter().map(|r| r.job_id.as_str()).collect::<Vec<_>>(), vec!["j2", "j1"]);
        assert!(rows[0].restored.is_none());
        store.mark_trash_restored("j1").unwrap();
        assert!(store.trash_entry("j1").unwrap().unwrap().restored.is_some());
        assert!(store.trash_entry("nope").unwrap().is_none());
    }

    #[test]
    fn migration_is_idempotent() {
        let conn = Connection::open_in_memory().unwrap();
        conn.execute_batch(SCHEMA).unwrap();
        migrate(&conn);
        migrate(&conn);
        conn.execute("UPDATE trash SET restored = 1", []).unwrap();
    }

    #[test]
    fn dev_session_roundtrip() {
        let store = Store::memory();
        assert!(store.dev_session("a").unwrap().is_none());
        let row = DevSessionRow {
            status: DevStatus::Ready,
            token: "t".into(),
            started_at: Some("now".into()),
            needs_restart: Some(NeedsRestart { reason: "nuxt.config.ts changed".into() }),
        };
        store.save_dev_session("a", &row).unwrap();
        let back = store.dev_session("a").unwrap().unwrap();
        assert_eq!(back.status, DevStatus::Ready);
        assert_eq!(back.needs_restart.unwrap().reason, "nuxt.config.ts changed");
    }
}
