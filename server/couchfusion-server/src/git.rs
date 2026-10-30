//! Git per repository, hardened (spec §8): no system/global config, hooks off, fsmonitor off,
//! refuse repos whose local config sets risky keys, explicit path lists only, never a shell.

use std::path::{Path, PathBuf};
use std::process::Stdio;

use tokio::process::Command;

use crate::dto::{HeadInfo, RepoDiff, RepoFile};
use crate::error::ApiError;

const AUTHOR_NAME: &str = "Radu Stanciu";
const AUTHOR_EMAIL: &str = "radu@kangu.ro";
const DIFF_CAP: usize = 256 * 1024;

pub struct Git {
    pub bin: PathBuf,
}

pub struct Output {
    pub code: i32,
    pub stdout: Vec<u8>,
    pub stderr: String,
}

/// Local config keys that would make git run code or contact hosts on our behalf.
pub fn risky_config_key(key: &str) -> bool {
    let key = key.to_ascii_lowercase();
    key.starts_with("filter.")
        || (key.starts_with("diff.") && (key.ends_with(".textconv") || key.ends_with(".command")))
        || key == "diff.external"
        || key == "core.sshcommand"
        || key == "core.pager"
        || key == "core.editor"
        || key == "core.hookspath"
        || key == "core.fsmonitor"
        || key == "core.askpass"
        || key == "core.gitproxy"
        || key.starts_with("include")
        || key.starts_with("credential.")
        || key.starts_with("uploadpack.")
        || key.starts_with("gpg.")
        || key == "sequence.editor"
        || (key.starts_with("merge.") && key.ends_with(".driver"))
}

/// Denied commit paths (secrets and build output).
pub fn denied_path(path: &str) -> bool {
    let name = path.rsplit('/').next().unwrap_or(path);
    name.starts_with(".env")
        || name.ends_with(".pem")
        || name.ends_with(".key")
        || name == "auth.json"
        || path.split('/').any(|seg| seg == "node_modules" || seg == ".output" || seg == ".nuxt")
}

/// Credentials for one remote: a git-credential-format file (`username=…`/`password=…`), offered
/// only to `origin` (`scheme://host`) through a helper that `cat`s it. The secret never appears in
/// argv or the environment; only the file path does.
pub struct RemoteAuth {
    pub origin: String,
    pub file: PathBuf,
}

/// The `-c` arguments that install the scoped credential helper (and clear any other helper).
pub fn credential_args(auth: &RemoteAuth) -> Vec<String> {
    vec![
        "-c".into(),
        "credential.helper=".into(),
        "-c".into(),
        format!("credential.{}.helper=", auth.origin),
        "-c".into(),
        format!("credential.{}.helper=!f() {{ test \"$1\" = get && cat \"$CF_GIT_CREDENTIAL\"; }}; f", auth.origin),
        "-c".into(),
        "credential.useHttpPath=false".into(),
    ]
}

impl Git {
    pub async fn run(&self, repo: &Path, args: &[&str]) -> anyhow::Result<Output> {
        self.run_with(repo, args, None).await
    }

    /// `run`, optionally with a scoped credential helper for network operations.
    pub async fn run_with(&self, repo: &Path, args: &[&str], auth: Option<&RemoteAuth>) -> anyhow::Result<Output> {
        let safe = format!("safe.directory={}", repo.display());
        let mut cmd = Command::new(&self.bin);
        cmd.arg("-C")
            .arg(repo)
            .args(["-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", &safe])
            .args(["-c", "core.pager=cat", "-c", "color.ui=false"]);
        if let Some(auth) = auth {
            cmd.args(credential_args(auth));
        }
        cmd.args(args)
            .env_clear()
            .env("PATH", "/usr/bin:/bin")
            .env("HOME", "/nonexistent")
            .env("GIT_CONFIG_NOSYSTEM", "1")
            .env("GIT_CONFIG_GLOBAL", "/dev/null")
            .env("GIT_TERMINAL_PROMPT", "0")
            .env("GIT_OPTIONAL_LOCKS", "0")
            .env("LC_ALL", "C")
            .env("GIT_SSH_COMMAND", "/bin/false")
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        if let Some(auth) = auth {
            cmd.env("CF_GIT_CREDENTIAL", &auth.file);
        }
        let out = cmd.output().await?;
        Ok(Output {
            code: out.status.code().unwrap_or(-1),
            stdout: out.stdout,
            stderr: String::from_utf8_lossy(&out.stderr).trim().to_string(),
        })
    }

    async fn ok(&self, repo: &Path, args: &[&str]) -> Result<Vec<u8>, ApiError> {
        self.ok_with(repo, args, None).await
    }

    async fn ok_with(&self, repo: &Path, args: &[&str], auth: Option<&RemoteAuth>) -> Result<Vec<u8>, ApiError> {
        let out = self.run_with(repo, args, auth).await.map_err(|e| ApiError::git(format!("git failed to start: {e}")))?;
        if out.code != 0 {
            return Err(ApiError::git(format!("git {} failed: {}", args.first().unwrap_or(&""), out.stderr)));
        }
        Ok(out.stdout)
    }

    pub async fn remote_url(&self, repo: &Path, remote: &str) -> Option<String> {
        let out = self.run(repo, &["remote", "get-url", remote]).await.ok()?;
        (out.code == 0).then(|| String::from_utf8_lossy(&out.stdout).trim().to_string())
    }

    /// Upstream of the current branch and `(ahead, behind)` against it, from local refs only.
    pub async fn upstream(&self, repo: &Path) -> (Option<String>, Option<(u32, u32)>) {
        let Ok(out) = self.run(repo, &["rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"]).await else { return (None, None) };
        if out.code != 0 {
            return (None, None);
        }
        let upstream = String::from_utf8_lossy(&out.stdout).trim().to_string();
        let counts = match self.run(repo, &["rev-list", "--left-right", "--count", "HEAD...@{u}"]).await {
            Ok(o) if o.code == 0 => {
                let text = String::from_utf8_lossy(&o.stdout);
                let mut it = text.split_whitespace().filter_map(|n| n.parse::<u32>().ok());
                it.next().zip(it.next())
            }
            _ => None,
        };
        (Some(upstream), counts)
    }

    pub async fn fetch(&self, repo: &Path, remote: &str, auth: Option<&RemoteAuth>) -> Result<(), ApiError> {
        self.ok_with(repo, &["fetch", "--prune", "--no-tags", "--no-write-fetch-head", remote], auth).await.map(|_| ())
    }

    /// Fast-forward only; never creates merge commits.
    pub async fn pull_ff(&self, repo: &Path, remote: &str, branch: &str, auth: Option<&RemoteAuth>) -> Result<String, ApiError> {
        self.fetch(repo, remote, auth).await?;
        let target = format!("refs/remotes/{remote}/{branch}");
        self.ok(repo, &["merge", "--ff-only", "--no-edit", &target]).await?;
        Ok(self.head(repo).await.map(|h| h.sha).unwrap_or_default())
    }

    /// Pushes `branch` to the same name on `remote` (never forced), then tracks it.
    pub async fn push(&self, repo: &Path, remote: &str, branch: &str, auth: Option<&RemoteAuth>) -> Result<String, ApiError> {
        let spec = format!("refs/heads/{branch}:refs/heads/{branch}");
        let out = self.ok_with(repo, &["push", "--porcelain", "--no-verify", remote, &spec], auth).await?;
        let upstream = format!("{remote}/{branch}");
        self.ok(repo, &["branch", &format!("--set-upstream-to={upstream}"), branch]).await?;
        Ok(String::from_utf8_lossy(&out).lines().find(|l| l.contains(&spec)).unwrap_or("").trim().to_string())
    }

    /// Refuses repos whose local config defines keys that execute code.
    pub async fn check_config(&self, repo: &Path) -> Result<(), ApiError> {
        if !repo.join(".git").exists() {
            return Err(ApiError::not_found("Not a git repository."));
        }
        let out = self.ok(repo, &["config", "--local", "--name-only", "--list"]).await?;
        let text = String::from_utf8_lossy(&out);
        if let Some(key) = text.lines().find(|k| risky_config_key(k)) {
            return Err(ApiError::git(format!("Refusing repository: local git config sets `{key}`.")));
        }
        Ok(())
    }

    pub async fn head(&self, repo: &Path) -> Option<HeadInfo> {
        let out = self.run(repo, &["log", "-1", "--format=%H%x00%s"]).await.ok()?;
        if out.code != 0 {
            return None;
        }
        let text = String::from_utf8_lossy(&out.stdout);
        let (sha, subject) = text.trim_end().split_once('\0')?;
        Some(HeadInfo { sha: sha.to_string(), subject: subject.to_string() })
    }

    pub async fn has_remote(&self, repo: &Path) -> bool {
        self.run(repo, &["remote"]).await.is_ok_and(|o| o.code == 0 && !o.stdout.trim_ascii().is_empty())
    }

    /// Branch and changed files from `git status --porcelain=v2 -z --branch`.
    pub async fn status(&self, repo: &Path) -> Result<(String, Vec<RepoFile>), ApiError> {
        let out = self.ok(repo, &["status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all"]).await?;
        Ok(parse_status(&out))
    }

    pub async fn diff(&self, repo: &Path, path: &str, untracked: bool) -> Result<RepoDiff, ApiError> {
        let out = if untracked {
            // Exit code 1 means "differences found" for --no-index.
            let o = self
                .run(repo, &["diff", "--no-color", "--no-ext-diff", "--no-index", "--", "/dev/null", path])
                .await
                .map_err(|e| ApiError::git(e.to_string()))?;
            if o.code > 1 {
                return Err(ApiError::git(o.stderr));
            }
            o.stdout
        } else {
            self.ok(repo, &["diff", "--no-color", "--no-ext-diff", "HEAD", "--", path]).await?
        };
        let binary = out.windows(13).any(|w| w == b"Binary files ");
        let truncated = out.len() > DIFF_CAP;
        let mut text = String::from_utf8_lossy(&out[..out.len().min(DIFF_CAP)]).into_owned();
        if truncated {
            text.push_str("\n… (truncated)\n");
        }
        Ok(RepoDiff { path: path.to_string(), diff: text, truncated, binary })
    }

    /// Stages exactly `paths` and commits only them.
    pub async fn commit(&self, repo: &Path, message: &str, paths: &[String]) -> Result<String, ApiError> {
        let mut add: Vec<&str> = vec!["add", "--"];
        add.extend(paths.iter().map(String::as_str));
        self.ok(repo, &add).await?;
        let full = format!("{}\n\nCommitted-Via: couchfusion-cp\n", message.trim_end());
        let author = format!("{AUTHOR_NAME} <{AUTHOR_EMAIL}>");
        let name = format!("user.name={AUTHOR_NAME}");
        let email = format!("user.email={AUTHOR_EMAIL}");
        let mut args: Vec<&str> = vec!["-c", &name, "-c", &email, "commit", "--no-verify", "--author", &author, "-m", &full, "--only", "--"];
        args.extend(paths.iter().map(String::as_str));
        self.ok(repo, &args).await?;
        Ok(self.head(repo).await.map(|h| h.sha).unwrap_or_default())
    }
}

pub fn parse_status(out: &[u8]) -> (String, Vec<RepoFile>) {
    let mut branch = String::new();
    let mut files = Vec::new();
    let mut records = out.split(|b| *b == 0).map(|r| String::from_utf8_lossy(r).into_owned());
    while let Some(rec) = records.next() {
        if rec.is_empty() {
            continue;
        }
        if let Some(rest) = rec.strip_prefix("# branch.head ") {
            branch = rest.to_string();
        } else if rec.starts_with('#') {
            continue;
        } else if let Some(rest) = rec.strip_prefix("? ") {
            files.push(RepoFile { path: rest.to_string(), index: ".".into(), worktree: "?".into(), untracked: true });
        } else if rec.starts_with("1 ") {
            // 1 XY sub mH mI mW hH hI path
            let parts: Vec<&str> = rec.splitn(9, ' ').collect();
            if parts.len() == 9 {
                let xy: Vec<char> = parts[1].chars().collect();
                files.push(RepoFile { path: parts[8].to_string(), index: xy[0].to_string(), worktree: xy[1].to_string(), untracked: false });
            }
        } else if rec.starts_with("2 ") {
            // 2 XY sub mH mI mW hH hI Xscore path \0 origPath
            let parts: Vec<&str> = rec.splitn(10, ' ').collect();
            if parts.len() == 10 {
                let xy: Vec<char> = parts[1].chars().collect();
                files.push(RepoFile { path: parts[9].to_string(), index: xy[0].to_string(), worktree: xy[1].to_string(), untracked: false });
            }
            records.next();
        } else if rec.starts_with("u ") {
            let parts: Vec<&str> = rec.splitn(11, ' ').collect();
            if let Some(path) = parts.get(10) {
                files.push(RepoFile { path: path.to_string(), index: "U".into(), worktree: "U".into(), untracked: false });
            }
        }
    }
    (branch, files)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn credential_helper_is_scoped_and_secret_free() {
        let auth = RemoteAuth { origin: "https://git2.kangu.ro".into(), file: PathBuf::from("/run/credentials/couchfusion.service/git-apps-radustanciu") };
        let args = credential_args(&auth);
        assert_eq!(args[1], "credential.helper=");
        assert!(args.iter().any(|a| a.starts_with("credential.https://git2.kangu.ro.helper=!f()")));
        // The helper answers only `get` and reads the file named by the env var; the path itself
        // (let alone the secret) is not in argv.
        let helper = args.iter().find(|a| a.contains("!f()")).unwrap();
        assert!(helper.contains(r#"test "$1" = get"#) && helper.contains(r#"cat "$CF_GIT_CREDENTIAL""#));
        assert!(!args.iter().any(|a| a.contains("git-apps-radustanciu")));
    }

    #[test]
    fn risky_keys() {
        assert!(risky_config_key("filter.lfs.smudge"));
        assert!(risky_config_key("core.sshCommand"));
        assert!(risky_config_key("include.path"));
        assert!(risky_config_key("diff.foo.textconv"));
        assert!(!risky_config_key("core.bare"));
        assert!(!risky_config_key("remote.origin.url"));
    }

    #[test]
    fn denied_paths() {
        assert!(denied_path(".env"));
        assert!(denied_path("app/.env.local"));
        assert!(denied_path("x/node_modules/a.js"));
        assert!(denied_path("key.pem"));
        assert!(!denied_path("app/components/content/X.vue"));
    }

    #[test]
    fn porcelain_v2() {
        let raw = b"# branch.oid abc\0# branch.head main\x001 .M N... 100644 100644 100644 aaa bbb app/x.vue\0? app/new file.vue\x002 R. N... 100644 100644 100644 a b R100 new.vue\0old.vue\0";
        let (branch, files) = parse_status(raw);
        assert_eq!(branch, "main");
        assert_eq!(files.len(), 3);
        assert_eq!(files[0], RepoFile { path: "app/x.vue".into(), index: ".".into(), worktree: "M".into(), untracked: false });
        assert_eq!(files[1].path, "app/new file.vue");
        assert!(files[1].untracked);
        assert_eq!(files[2].path, "new.vue");
        assert_eq!(files[2].index, "R");
    }

    #[tokio::test]
    async fn commit_in_temp_repo() {
        let tmp = tempfile::tempdir().unwrap();
        let repo = tmp.path();
        let git = Git { bin: "/usr/bin/git".into() };
        assert_eq!(git.run(repo, &["init", "-q", "-b", "main"]).await.unwrap().code, 0);
        std::fs::write(repo.join("a.txt"), "a").unwrap();
        std::fs::write(repo.join("b.txt"), "b").unwrap();
        git.check_config(repo).await.unwrap();
        let (_, files) = git.status(repo).await.unwrap();
        assert_eq!(files.len(), 2);
        let sha = git.commit(repo, "add a", &["a.txt".into()]).await.unwrap();
        assert_eq!(sha.len(), 40);
        let (branch, files) = git.status(repo).await.unwrap();
        assert_eq!(branch, "main");
        assert_eq!(files.iter().map(|f| f.path.as_str()).collect::<Vec<_>>(), vec!["b.txt"]);
        let head = git.head(repo).await.unwrap();
        assert_eq!(head.subject, "add a");
        let msg = git.run(repo, &["log", "-1", "--format=%B%n%an <%ae>"]).await.unwrap();
        let text = String::from_utf8_lossy(&msg.stdout);
        assert!(text.contains("Committed-Via: couchfusion-cp"));
        assert!(text.contains("Radu Stanciu <radu@kangu.ro>"));
        let d = git.diff(repo, "b.txt", true).await.unwrap();
        assert!(d.diff.contains("+b"));
        assert!(!git.has_remote(repo).await);
        // risky config is refused
        git.run(repo, &["config", "core.sshCommand", "evil"]).await.unwrap();
        assert!(git.check_config(repo).await.is_err());
    }
}
