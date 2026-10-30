//! Site inspection: component manifest (Go rules), symlink-safe file access, code references,
//! file snapshots for agent baselines, and the `builderFieldMeta` lint.

use std::collections::BTreeMap;
use std::ffi::CString;
use std::fs::File;
use std::os::fd::{AsRawFd, FromRawFd, OwnedFd};
use std::os::unix::ffi::OsStrExt;
use std::path::{Path, PathBuf};
use std::time::SystemTime;

use nix::fcntl::{OFlag, openat};
use nix::sys::stat::Mode;
use serde::Deserialize;

use crate::dto::{CodeRef, ContentComponent};
use crate::util::{kebab_case, pascal_case, sha256_hex};

pub const MANIFEST_REL: &str = "app/content-builder/component-definitions.json";
pub const CONTENT_DIR_REL: &str = "app/components/content";

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct Manifest {
    version: u32,
    #[serde(rename = "typescriptSha256")]
    typescript_sha256: String,
    components: Option<Vec<ContentComponent>>,
}

#[derive(Debug, Clone)]
pub struct Registry {
    /// ready | stale | missing | invalid
    pub status: &'static str,
    pub components: Vec<ContentComponent>,
    pub problem: Option<String>,
}

impl Registry {
    fn invalid(problem: &str) -> Self {
        Self { status: "invalid", components: vec![], problem: Some(problem.to_string()) }
    }
    pub fn usable(&self) -> bool {
        matches!(self.status, "ready" | "stale")
    }
    pub fn find(&self, id: &str) -> Option<&ContentComponent> {
        self.components.iter().find(|c| c.id == id)
    }
    pub fn ids(&self) -> Vec<String> {
        self.components.iter().map(|c| c.id.clone()).collect()
    }
}

/// Inspects `app/content-builder/component-definitions.json` with the same checks as Go's
/// `ComponentInspector.Inspect`: version 1, `typescriptSha256` equals the sibling `.ts`, ids
/// strictly sorted, every source a safe relative `.vue` path that resolves inside the site.
pub fn inspect_registry(app_dir: &Path) -> Registry {
    let manifest_path = [MANIFEST_REL, "content-builder/component-definitions.json"]
        .iter()
        .map(|rel| app_dir.join(rel))
        .find(|p| p.exists());
    let Some(manifest_path) = manifest_path else {
        return Registry { status: "missing", components: vec![], problem: None };
    };
    let Ok(meta) = std::fs::symlink_metadata(&manifest_path) else {
        return Registry::invalid("Component registry metadata is unavailable.");
    };
    if !meta.is_file() {
        return Registry::invalid("Component registry metadata is unavailable.");
    }
    let Ok(text) = std::fs::read_to_string(&manifest_path) else {
        return Registry::invalid("Component registry metadata is unavailable.");
    };
    let manifest: Manifest = match serde_json::from_str(&text) {
        Ok(m) => m,
        Err(_) => return Registry::invalid("Component registry JSON is invalid."),
    };
    let Some(components) = manifest.components else {
        return Registry::invalid("Component registry metadata is invalid.");
    };
    if manifest.version != 1 || !valid_sha256(&manifest.typescript_sha256) {
        return Registry::invalid("Component registry metadata is invalid.");
    }
    let ts_path = manifest_path.with_extension("ts");
    let Ok(ts) = std::fs::read(&ts_path) else {
        return Registry::invalid("Component registry TypeScript definitions are unavailable.");
    };
    if sha256_hex(&ts) != manifest.typescript_sha256 {
        return Registry::invalid("Component registry does not match its TypeScript definitions.");
    }
    let manifest_mtime = meta.modified().unwrap_or(SystemTime::UNIX_EPOCH);
    let mut stale = false;
    let mut previous = String::new();
    for (i, c) in components.iter().enumerate() {
        if !valid_definition(c) || (i > 0 && c.id <= previous) {
            return Registry::invalid("Component registry metadata is invalid.");
        }
        previous = c.id.clone();
        match open_contained(app_dir, &c.source) {
            Ok(file) => {
                let mtime = file.file.metadata().and_then(|m| m.modified()).unwrap_or(SystemTime::UNIX_EPOCH);
                if mtime > manifest_mtime {
                    stale = true;
                }
            }
            Err(_) => return Registry::invalid("A registered component source is unsafe or unavailable."),
        }
    }
    Registry { status: if stale { "stale" } else { "ready" }, components, problem: None }
}

fn valid_sha256(s: &str) -> bool {
    s.len() == 64 && s.bytes().all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}

fn valid_definition(c: &ContentComponent) -> bool {
    valid_resource_id(&c.id) && !c.label.trim().is_empty() && c.label == c.label.trim() && c.prop_count >= 0 && valid_source(&c.source)
}

/// Go `validResourceID` / `validComponentID`: non-empty, ≤ 256, no slashes, whitespace or
/// control characters, not `.`/`..`.
pub fn valid_resource_id(s: &str) -> bool {
    !s.is_empty()
        && s.len() <= 256
        && s != "."
        && s != ".."
        && !s.chars().any(|c| c == '/' || c == '\\' || c.is_whitespace() || c.is_control())
}

/// Relative, clean, forward-slash `.vue` path that does not climb out.
pub fn valid_source(source: &str) -> bool {
    !source.is_empty()
        && !source.contains('\\')
        && !source.starts_with('/')
        && source.ends_with(".vue")
        && source.split('/').all(|seg| !seg.is_empty() && seg != "." && seg != "..")
}

/// A file opened without following any symlink, plus its parent directory fd (for `renameat`).
pub struct ContainedFile {
    pub parent: OwnedFd,
    pub name: CString,
    pub file: File,
}

/// Opens `root/rel` walking one component at a time with `O_NOFOLLOW`, so no symlink anywhere
/// in the path (other than above `root`) is honoured. The final entry must be a regular file.
pub fn open_contained(root: &Path, rel: &str) -> std::io::Result<ContainedFile> {
    let invalid = || std::io::Error::new(std::io::ErrorKind::InvalidInput, "unsafe path");
    let segments: Vec<&str> = rel.split('/').collect();
    if segments.iter().any(|s| s.is_empty() || *s == "." || *s == "..") {
        return Err(invalid());
    }
    let dir_flags = OFlag::O_RDONLY | OFlag::O_DIRECTORY | OFlag::O_CLOEXEC;
    let root_c = CString::new(root.as_os_str().as_bytes()).map_err(|_| invalid())?;
    // SAFETY: plain open(2) of the root directory; the fd is wrapped immediately.
    let raw = unsafe { libc::open(root_c.as_ptr(), (dir_flags.bits()) as libc::c_int) };
    if raw < 0 {
        return Err(std::io::Error::last_os_error());
    }
    let mut dir = unsafe { OwnedFd::from_raw_fd(raw) };
    let (last, parents) = segments.split_last().ok_or_else(invalid)?;
    for seg in parents {
        dir = openat(&dir, *seg, dir_flags | OFlag::O_NOFOLLOW, Mode::empty()).map_err(std::io::Error::from)?;
    }
    let fd = openat(&dir, *last, OFlag::O_RDONLY | OFlag::O_NOFOLLOW | OFlag::O_CLOEXEC, Mode::empty())
        .map_err(std::io::Error::from)?;
    let file = File::from(fd);
    if !file.metadata()?.is_file() {
        return Err(invalid());
    }
    Ok(ContainedFile { parent: dir, name: CString::new(*last).map_err(|_| invalid())?, file })
}

/// `renameat(parent, name, AT_FDCWD, target)`: moves the entry we validated, not whatever a
/// path lookup would find now.
pub fn rename_contained(file: &ContainedFile, target: &Path) -> std::io::Result<()> {
    let target_c = CString::new(target.as_os_str().as_bytes())
        .map_err(|_| std::io::Error::new(std::io::ErrorKind::InvalidInput, "bad target"))?;
    // SAFETY: both strings are NUL-terminated and the dir fd is open for the call's duration.
    let rc = unsafe { libc::renameat(file.parent.as_raw_fd(), file.name.as_ptr(), libc::AT_FDCWD, target_c.as_ptr()) };
    if rc != 0 {
        let err = std::io::Error::last_os_error();
        // Under systemd ReadWritePaths= the workspace and the trash are separate bind mounts, so
        // rename() reports EXDEV even on one filesystem. Fall back to copy + fsync + unlink.
        if err.raw_os_error() == Some(libc::EXDEV) {
            return copy_then_unlink_contained(file, target);
        }
        return Err(err);
    }
    Ok(())
}

/// EXDEV fallback for [`rename_contained`]: copies the already-open (O_NOFOLLOW) handle into a
/// new file at `target`, fsyncs it, and only then unlinks the original name — and only if that
/// name still refers to the inode that was opened, so a file swapped in meanwhile is never removed.
fn copy_then_unlink_contained(file: &ContainedFile, target: &Path) -> std::io::Result<()> {
    use std::io::{Read, Seek, SeekFrom, Write};
    use std::os::unix::fs::MetadataExt;

    let opened = file.file.metadata()?;
    let mut source = &file.file;
    source.seek(SeekFrom::Start(0))?;
    let mut buf = Vec::with_capacity(opened.len() as usize);
    source.read_to_end(&mut buf)?;

    let mut out = std::fs::OpenOptions::new().write(true).create_new(true).open(target)?;
    out.write_all(&buf)?;
    out.sync_all()?;
    drop(out);
    if let Some(dir) = target.parent() {
        File::open(dir)?.sync_all()?;
    }

    // SAFETY: zeroed stat buffer is valid for fstatat to fill; name is NUL-terminated.
    let mut st: libc::stat = unsafe { std::mem::zeroed() };
    let rc = unsafe { libc::fstatat(file.parent.as_raw_fd(), file.name.as_ptr(), &mut st, libc::AT_SYMLINK_NOFOLLOW) };
    if rc != 0 {
        return Err(std::io::Error::last_os_error());
    }
    if st.st_ino as u64 != opened.ino() || st.st_dev as u64 != opened.dev() {
        let _ = std::fs::remove_file(target);
        return Err(std::io::Error::other("source file was replaced during the move; nothing deleted"));
    }
    // SAFETY: parent fd is open and name is NUL-terminated.
    let rc = unsafe { libc::unlinkat(file.parent.as_raw_fd(), file.name.as_ptr(), 0) };
    if rc != 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(())
}

pub fn read_contained(root: &Path, rel: &str) -> std::io::Result<Vec<u8>> {
    use std::io::Read;
    let mut file = open_contained(root, rel)?.file;
    let mut buf = Vec::new();
    file.read_to_end(&mut buf)?;
    Ok(buf)
}

/// Creates `root/rel` with `O_CREAT|O_EXCL` (never overwrites), walking existing directories
/// with `O_NOFOLLOW`, writes `bytes`, sets `mode` explicitly (umask-independent) and fsyncs the
/// file and its directory. Fails with `AlreadyExists` if anything is at `rel` already.
pub fn create_contained(root: &Path, rel: &str, bytes: &[u8], mode: u32) -> std::io::Result<()> {
    use std::io::Write;
    use std::os::unix::fs::PermissionsExt;
    let invalid = || std::io::Error::new(std::io::ErrorKind::InvalidInput, "unsafe path");
    let segments: Vec<&str> = rel.split('/').collect();
    if segments.iter().any(|s| s.is_empty() || *s == "." || *s == "..") {
        return Err(invalid());
    }
    let dir_flags = OFlag::O_RDONLY | OFlag::O_DIRECTORY | OFlag::O_CLOEXEC;
    let root_c = CString::new(root.as_os_str().as_bytes()).map_err(|_| invalid())?;
    // SAFETY: plain open(2) of the root directory; the fd is wrapped immediately.
    let raw = unsafe { libc::open(root_c.as_ptr(), (dir_flags.bits()) as libc::c_int) };
    if raw < 0 {
        return Err(std::io::Error::last_os_error());
    }
    let mut dir = unsafe { OwnedFd::from_raw_fd(raw) };
    let (last, parents) = segments.split_last().ok_or_else(invalid)?;
    for seg in parents {
        dir = openat(&dir, *seg, dir_flags | OFlag::O_NOFOLLOW, Mode::empty()).map_err(std::io::Error::from)?;
    }
    let fd = openat(
        &dir,
        *last,
        OFlag::O_WRONLY | OFlag::O_CREAT | OFlag::O_EXCL | OFlag::O_NOFOLLOW | OFlag::O_CLOEXEC,
        Mode::from_bits_truncate(0o600),
    )
    .map_err(std::io::Error::from)?;
    let mut file = File::from(fd);
    file.write_all(bytes)?;
    file.set_permissions(std::fs::Permissions::from_mode(mode))?;
    file.sync_all()?;
    File::from(dir).sync_all()?;
    Ok(())
}

const SKIP_DIRS: &[&str] = &["node_modules", ".nuxt", ".output", ".git", ".data", "dist", "test-results", ".cache"];

fn walk(dir: &Path, out: &mut Vec<PathBuf>, filter: &dyn Fn(&Path) -> bool) {
    let Ok(entries) = std::fs::read_dir(dir) else { return };
    for entry in entries.flatten() {
        let Ok(ft) = entry.file_type() else { continue };
        let path = entry.path();
        if ft.is_dir() {
            if !SKIP_DIRS.iter().any(|s| entry.file_name() == *s) {
                walk(&path, out, filter);
            }
        } else if ft.is_file() && filter(&path) {
            out.push(path);
        }
    }
}

fn rel(root: &Path, path: &Path) -> String {
    path.strip_prefix(root).unwrap_or(path).to_string_lossy().replace('\\', "/")
}

/// Finds references to a component in app code: `<PascalName`, `<kebab-name`,
/// `resolveComponent('…')`, and imports of its source file.
pub fn code_references(app_dir: &Path, component_id: &str, source: &str) -> Vec<CodeRef> {
    let pascal = regex::escape(&pascal_case(component_id));
    let kebab = regex::escape(component_id);
    let file_name = regex::escape(source.rsplit('/').next().unwrap_or(source));
    let pattern = format!(
        r#"<(?:{pascal}|{kebab})[\s/>]|<(?:{pascal}|{kebab})$|resolveComponent\(\s*['"](?:{pascal}|{kebab})['"]|['"/]{file_name}['"]"#
    );
    let Ok(re) = regex::Regex::new(&pattern) else { return vec![] };
    let app = app_dir.join("app");
    let mut files = Vec::new();
    walk(&app, &mut files, &|p| {
        matches!(p.extension().and_then(|e| e.to_str()), Some("vue" | "ts" | "js" | "mjs"))
    });
    let mut refs = Vec::new();
    for file in files {
        let r = rel(app_dir, &file);
        if r == source || r.starts_with("app/content-builder/") {
            continue;
        }
        let Ok(text) = std::fs::read_to_string(&file) else { continue };
        for (i, line) in text.lines().enumerate() {
            if re.is_match(line) {
                refs.push(CodeRef { path: r.clone(), line: (i + 1) as u32 });
                if refs.len() >= 200 {
                    return refs;
                }
            }
        }
    }
    refs
}

/// Path → sha256 of files the agent may touch, used as a baseline before `codex exec`.
pub type Snapshot = BTreeMap<String, String>;

const ROOT_FILES: &[&str] = &[
    "nuxt.config.ts",
    "nuxt.config.js",
    "nuxt.config.mjs",
    "package.json",
    "bun.lock",
    "bun.lockb",
    "package-lock.json",
    "pnpm-lock.yaml",
    "yarn.lock",
    "app.config.ts",
    "content.config.ts",
    "tailwind.config.js",
];

pub fn snapshot(app_dir: &Path) -> Snapshot {
    let mut files = Vec::new();
    walk(&app_dir.join("app"), &mut files, &|_| true);
    walk(&app_dir.join("server"), &mut files, &|_| true);
    walk(&app_dir.join("public"), &mut files, &|_| true);
    for f in ROOT_FILES {
        let p = app_dir.join(f);
        if p.is_file() {
            files.push(p);
        }
    }
    files
        .into_iter()
        .filter_map(|p| {
            let r = rel(app_dir, &p);
            if r.starts_with("app/content-builder/component-definitions.") {
                return None;
            }
            std::fs::read(&p).ok().map(|bytes| (r, sha256_hex(&bytes)))
        })
        .collect()
}

#[derive(Debug, Default, PartialEq, Eq)]
pub struct Changes {
    pub added: Vec<String>,
    pub modified: Vec<String>,
    pub deleted: Vec<String>,
}

impl Changes {
    pub fn all(&self) -> Vec<String> {
        let mut v: Vec<String> = self.added.iter().chain(&self.modified).chain(&self.deleted).cloned().collect();
        v.sort();
        v
    }
}

pub fn diff_snapshots(before: &Snapshot, after: &Snapshot) -> Changes {
    let mut changes = Changes::default();
    for (path, sha) in after {
        match before.get(path) {
            None => changes.added.push(path.clone()),
            Some(old) if old != sha => changes.modified.push(path.clone()),
            _ => {}
        }
    }
    for path in before.keys() {
        if !after.contains_key(path) {
            changes.deleted.push(path.clone());
        }
    }
    changes
}

/// `package.json` or any lockfile changed (agent policy: not allowed in Phase 1).
pub fn dependency_change(changes: &Changes) -> Vec<String> {
    changes
        .all()
        .into_iter()
        .filter(|p| matches!(p.as_str(), "package.json" | "bun.lock" | "bun.lockb" | "package-lock.json" | "pnpm-lock.yaml" | "yarn.lock"))
        .collect()
}

/// Changes that a running `nuxt dev` does not pick up without a restart.
pub fn restart_reason(changes: &Changes) -> Option<String> {
    let hit = changes.all().into_iter().find(|p| {
        p.starts_with("nuxt.config.") || p.starts_with("app/pages/") || p.starts_with("pages/") || p == "package.json" || p.starts_with("layers/") || p == "app.config.ts"
    })?;
    Some(format!("{hit} changed"))
}

/// New content components among the added files → their registry ids.
pub fn new_component_ids(changes: &Changes) -> Vec<(String, String)> {
    changes
        .added
        .iter()
        .filter(|p| p.starts_with("app/components/content/") && p.ends_with(".vue"))
        .map(|p| {
            let stem = p.rsplit('/').next().unwrap_or(p).trim_end_matches(".vue");
            (kebab_case(stem), p.clone())
        })
        .collect()
}

/// U7 lint: every prop typed as a union of ≥ 2 string literals needs a `builderFieldMeta`
/// entry with `type: 'select'` and `options` in a non-setup `<script lang="ts">` block.
pub fn lint_builder_field_meta(vue_source: &str) -> Vec<String> {
    let union = regex::Regex::new(
        r#"(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)\??\s*:\s*((?:'[^']*'|"[^"]*")(?:\s*\|\s*(?:'[^']*'|"[^"]*"))+)\s*[,;]?\s*(?://.*)?$"#,
    )
    .expect("valid regex");
    let script_open = regex::Regex::new(r"<script\b([^>]*)>").expect("valid regex");
    let mut setup_block = String::new();
    let mut plain_block = String::new();
    let mut rest = vue_source;
    while let Some(m) = script_open.captures(rest) {
        let whole = m.get(0).unwrap();
        let attrs = m.get(1).map_or("", |a| a.as_str());
        let after = &rest[whole.end()..];
        let end = after.find("</script>").unwrap_or(after.len());
        let body = &after[..end];
        if attrs.contains("setup") {
            setup_block.push_str(body);
        } else {
            plain_block.push_str(body);
        }
        rest = &after[end..];
        if rest.len() >= 9 {
            rest = &rest[9..];
        }
    }
    let mut warnings = Vec::new();
    for cap in union.captures_iter(&setup_block) {
        let prop = &cap[1];
        let literals = cap[2].split('|').count();
        if literals < 2 {
            continue;
        }
        let has_select = plain_block.contains("builderFieldMeta")
            && plain_block.find(prop).is_some_and(|i| {
                let window = &plain_block[i..(i + 400).min(plain_block.len())];
                window.contains("select") && window.contains("options")
            });
        if !has_select {
            warnings.push(format!("prop `{prop}` has {literals} options but no builderFieldMeta select entry"));
        }
    }
    warnings
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::symlink;

    fn write(root: &Path, rel: &str, text: &str) {
        let p = root.join(rel);
        std::fs::create_dir_all(p.parent().unwrap()).unwrap();
        std::fs::write(p, text).unwrap();
    }

    fn manifest(dir: &Path, ids: &[(&str, &str)]) {
        let ts = "export default []\n";
        write(dir, "app/content-builder/component-definitions.ts", ts);
        let comps: Vec<_> = ids
            .iter()
            .map(|(id, src)| serde_json::json!({"id": id, "label": "L", "description": "", "source": src, "propCount": 1, "allowChildren": false}))
            .collect();
        let json = serde_json::json!({"version": 1, "typescriptSha256": sha256_hex(ts.as_bytes()), "components": comps});
        write(dir, MANIFEST_REL, &json.to_string());
    }

    #[test]
    fn registry_ready_and_checks() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        write(d, "app/components/content/NetworkHero.vue", "<template/>");
        write(d, "app/components/content/Phil.vue", "<template/>");
        manifest(d, &[("network-hero", "app/components/content/NetworkHero.vue"), ("phil", "app/components/content/Phil.vue")]);
        let r = inspect_registry(d);
        assert!(r.usable(), "{r:?}");
        assert_eq!(r.ids(), vec!["network-hero", "phil"]);

        // unsorted ids are invalid
        manifest(d, &[("phil", "app/components/content/Phil.vue"), ("network-hero", "app/components/content/NetworkHero.vue")]);
        assert_eq!(inspect_registry(d).status, "invalid");

        // ts digest mismatch is invalid
        manifest(d, &[("phil", "app/components/content/Phil.vue")]);
        write(d, "app/content-builder/component-definitions.ts", "changed");
        assert_eq!(inspect_registry(d).status, "invalid");
    }

    #[test]
    fn registry_missing() {
        let tmp = tempfile::tempdir().unwrap();
        assert_eq!(inspect_registry(tmp.path()).status, "missing");
    }

    #[test]
    fn real_radustanciu_manifest_if_present() {
        let app = Path::new("/Users/radu/Projects/nuxt-apps/apps/radustanciu");
        if app.join(MANIFEST_REL).exists() {
            let r = inspect_registry(app);
            assert!(r.usable(), "{r:?}");
            assert!(r.find("network-hero").is_some());
        }
    }

    #[test]
    fn source_validation() {
        assert!(valid_source("app/components/content/X.vue"));
        assert!(!valid_source("/etc/X.vue"));
        assert!(!valid_source("app/../X.vue"));
        assert!(!valid_source("app//X.vue"));
        assert!(!valid_source("app/X.ts"));
        assert!(!valid_resource_id("a/b"));
        assert!(!valid_resource_id(".."));
        assert!(valid_resource_id("network-hero"));
    }

    #[test]
    fn contained_open_rejects_symlinks() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        write(d, "app/components/content/Real.vue", "x");
        write(d, "outside/Secret.vue", "s");
        symlink(d.join("outside/Secret.vue"), d.join("app/components/content/Link.vue")).unwrap();
        symlink(d.join("outside"), d.join("app/components/linkdir")).unwrap();
        assert!(open_contained(d, "app/components/content/Real.vue").is_ok());
        assert!(open_contained(d, "app/components/content/Link.vue").is_err());
        assert!(open_contained(d, "app/components/linkdir/Secret.vue").is_err());
        assert!(open_contained(d, "app/../outside/Secret.vue").is_err());
        assert!(open_contained(d, "app/components/content").is_err());
    }

    #[test]
    fn rename_moves_validated_entry() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        write(d, "app/components/content/Real.vue", "x");
        let f = open_contained(d, "app/components/content/Real.vue").unwrap();
        let target = d.join("trash.vue");
        rename_contained(&f, &target).unwrap();
        assert!(target.exists());
        assert!(!d.join("app/components/content/Real.vue").exists());
    }

    #[test]
    fn create_contained_is_exclusive_and_sets_mode() {
        use std::os::unix::fs::PermissionsExt;
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        std::fs::create_dir_all(d.join("app/components/content")).unwrap();
        create_contained(d, "app/components/content/New.vue", b"x", 0o664).unwrap();
        let meta = std::fs::metadata(d.join("app/components/content/New.vue")).unwrap();
        assert_eq!(meta.permissions().mode() & 0o777, 0o664);
        let err = create_contained(d, "app/components/content/New.vue", b"y", 0o664).unwrap_err();
        assert_eq!(err.kind(), std::io::ErrorKind::AlreadyExists);
        assert_eq!(std::fs::read(d.join("app/components/content/New.vue")).unwrap(), b"x");
        assert!(create_contained(d, "app/components/../x.vue", b"y", 0o664).is_err());
    }

    #[test]
    fn create_contained_refuses_symlinks() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        let outside = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(d.join("app/components")).unwrap();
        symlink(outside.path(), d.join("app/components/content")).unwrap();
        assert!(create_contained(d, "app/components/content/X.vue", b"y", 0o664).is_err());
        assert!(!outside.path().join("X.vue").exists());
        // A dangling symlink at the final name is not followed either.
        std::fs::remove_file(d.join("app/components/content")).unwrap();
        std::fs::create_dir_all(d.join("app/components/content")).unwrap();
        symlink(outside.path().join("Y.vue"), d.join("app/components/content/Y.vue")).unwrap();
        assert!(create_contained(d, "app/components/content/Y.vue", b"y", 0o664).is_err());
        assert!(!outside.path().join("Y.vue").exists());
    }

    #[test]
    fn copy_fallback_moves_content_and_unlinks_source() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        write(d, "app/components/content/Real.vue", "<template>x</template>");
        let f = open_contained(d, "app/components/content/Real.vue").unwrap();
        let target = d.join("trash/Real.vue");
        std::fs::create_dir_all(target.parent().unwrap()).unwrap();
        copy_then_unlink_contained(&f, &target).unwrap();
        assert_eq!(std::fs::read_to_string(&target).unwrap(), "<template>x</template>");
        assert!(!d.join("app/components/content/Real.vue").exists());
    }

    #[test]
    fn copy_fallback_refuses_when_source_was_swapped() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        write(d, "app/components/content/Real.vue", "original");
        let f = open_contained(d, "app/components/content/Real.vue").unwrap();
        // Replace the name with a different inode after opening.
        std::fs::remove_file(d.join("app/components/content/Real.vue")).unwrap();
        write(d, "app/components/content/Real.vue", "swapped");
        let target = d.join("trash.vue");
        assert!(copy_then_unlink_contained(&f, &target).is_err());
        assert_eq!(std::fs::read_to_string(d.join("app/components/content/Real.vue")).unwrap(), "swapped");
        assert!(!target.exists());
    }

    #[test]
    fn finds_code_references() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        write(d, "app/components/content/NetworkHero.vue", "<template/>");
        write(d, "app/components/content/Landing.vue", "<template>\n  <NetworkHero title=\"x\" />\n  <network-hero>\n</template>");
        write(d, "app/pages/x.vue", "const c = resolveComponent('NetworkHero')\nimport H from '~/components/content/NetworkHero.vue'\n<NetworkHeroic/>");
        let refs = code_references(d, "network-hero", "app/components/content/NetworkHero.vue");
        let found: Vec<_> = refs.iter().map(|r| (r.path.as_str(), r.line)).collect();
        assert_eq!(
            found,
            vec![("app/components/content/Landing.vue", 2), ("app/components/content/Landing.vue", 3), ("app/pages/x.vue", 1), ("app/pages/x.vue", 2)]
        );
    }

    #[test]
    fn snapshot_diff_and_policies() {
        let tmp = tempfile::tempdir().unwrap();
        let d = tmp.path();
        write(d, "app/components/content/A.vue", "a");
        write(d, "package.json", "{}");
        let before = snapshot(d);
        write(d, "app/components/content/NewThing.vue", "n");
        write(d, "package.json", "{\"x\":1}");
        write(d, "app/content-builder/component-definitions.json", "{}");
        let after = snapshot(d);
        let ch = diff_snapshots(&before, &after);
        assert_eq!(ch.added, vec!["app/components/content/NewThing.vue"]);
        assert_eq!(ch.modified, vec!["package.json"]);
        assert_eq!(dependency_change(&ch), vec!["package.json"]);
        assert_eq!(restart_reason(&ch).unwrap(), "package.json changed");
        assert_eq!(new_component_ids(&ch), vec![("new-thing".to_string(), "app/components/content/NewThing.vue".to_string())]);
    }

    #[test]
    fn lint_flags_missing_select() {
        let bad = "<script setup lang=\"ts\">\nconst props = withDefaults(defineProps<{\n  variant?: 'light' | 'dark'\n  title?: string\n}>(), {})\n</script>\n<template/>";
        assert_eq!(lint_builder_field_meta(bad), vec!["prop `variant` has 2 options but no builderFieldMeta select entry"]);
        let good = format!(
            "{bad}\n<script lang=\"ts\">\nexport const builderFieldMeta = {{\n  variant: {{ type: 'select', options: ['light', 'dark'] }},\n}}\n</script>"
        );
        assert!(lint_builder_field_meta(&good).is_empty());
    }
}
