//! Small shared helpers: time, randomness, hashing, redaction.

use std::path::Path;

use base64::Engine;
use sha2::{Digest, Sha256};
use subtle::ConstantTimeEq;
use time::OffsetDateTime;
use time::format_description::well_known::Rfc3339;

/// RFC3339 UTC timestamp, e.g. `2026-10-29T19:24:05.123Z`.
pub fn now_rfc3339() -> String {
    format_time(OffsetDateTime::now_utc())
}

pub fn format_time(t: OffsetDateTime) -> String {
    t.to_offset(time::UtcOffset::UTC).format(&Rfc3339).unwrap_or_default()
}

pub fn unix_now() -> i64 {
    OffsetDateTime::now_utc().unix_timestamp()
}

/// 32 random bytes, base64url without padding.
pub fn random_token() -> String {
    let mut buf = [0u8; 32];
    getrandom::fill(&mut buf).expect("OS randomness");
    base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(buf)
}

/// Short random id (16 hex chars) for jobs and messages.
pub fn random_id() -> String {
    let mut buf = [0u8; 8];
    getrandom::fill(&mut buf).expect("OS randomness");
    hex(&buf)
}

pub fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

pub fn sha256_hex(bytes: &[u8]) -> String {
    hex(&Sha256::digest(bytes))
}

pub fn sha256_file(path: &Path) -> std::io::Result<String> {
    Ok(sha256_hex(&std::fs::read(path)?))
}

/// Constant-time string comparison.
pub fn ct_eq(a: &str, b: &str) -> bool {
    a.len() == b.len() && bool::from(a.as_bytes().ct_eq(b.as_bytes()))
}

/// Kebab-case from a PascalCase file stem (`NetworkHero` → `network-hero`). Same three
/// regex steps as `toKebabCase` in `layers/content/scripts/generate-component-registry.mjs`.
pub fn kebab_case(stem: &str) -> String {
    use std::sync::OnceLock;
    static RES: OnceLock<[regex::Regex; 3]> = OnceLock::new();
    let [a, b, c] = RES.get_or_init(|| {
        [
            regex::Regex::new(r"([a-z0-9])([A-Z])").unwrap(),
            regex::Regex::new(r"([A-Z])([A-Z][a-z])").unwrap(),
            regex::Regex::new(r"[\s_-]+").unwrap(),
        ]
    });
    let s = a.replace_all(stem, "$1-$2");
    let s = b.replace_all(&s, "$1-$2");
    c.replace_all(&s, "-").to_lowercase()
}

/// `network-hero` → `NetworkHero`.
pub fn pascal_case(kebab: &str) -> String {
    kebab
        .split(['-', '_'])
        .filter(|s| !s.is_empty())
        .map(|s| {
            let mut c = s.chars();
            match c.next() {
                Some(first) => first.to_ascii_uppercase().to_string() + c.as_str(),
                None => String::new(),
            }
        })
        .collect()
}

/// Redacts secrets from a log line before it is buffered or sent to the browser.
/// Covers the Go rules (password/token/authorization/cookie_secret/admin_auth keys, URL userinfo,
/// Authorization/Cookie headers) plus exact secret values passed in `extra`.
pub struct Redactor {
    patterns: Vec<regex::Regex>,
    extra: Vec<String>,
}

impl Redactor {
    pub fn new(extra: Vec<String>) -> Self {
        let patterns = [
            r#"(?i)((?:password|passwd|token|secret|authorization|cookie_secret|admin_auth|api_key|private_key)["']?\s*[:=]\s*["']?)([^\s"',&]+)"#,
            r"(?i)(authorization:\s*(?:basic|bearer)\s+)(\S+)",
            r"(?i)(cookie:\s*)(.+)",
            r"(?i)(https?://)([^/\s:@]+:[^/\s@]+)@",
        ]
        .iter()
        .map(|p| regex::Regex::new(p).expect("valid redaction regex"))
        .collect();
        Self {
            patterns,
            extra: extra.into_iter().filter(|s| s.len() >= 6).collect(),
        }
    }

    pub fn redact(&self, line: &str) -> String {
        let mut out = line.to_string();
        for value in &self.extra {
            if out.contains(value.as_str()) {
                out = out.replace(value.as_str(), "[redacted]");
            }
        }
        for (i, re) in self.patterns.iter().enumerate() {
            out = if i == 3 {
                re.replace_all(&out, "${1}[redacted]@").into_owned()
            } else {
                re.replace_all(&out, "${1}[redacted]").into_owned()
            };
        }
        out
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn kebab_matches_registry_ids() {
        assert_eq!(kebab_case("NetworkHero"), "network-hero");
        assert_eq!(kebab_case("BlogIndex"), "blog-index");
        assert_eq!(kebab_case("Phil"), "phil");
        assert_eq!(kebab_case("ZzPhase0Probe"), "zz-phase0-probe");
        assert_eq!(kebab_case("FAQSection"), "faq-section");
        assert_eq!(pascal_case("network-hero"), "NetworkHero");
    }

    #[test]
    fn redaction() {
        let r = Redactor::new(vec!["supersecretvalue".into()]);
        assert_eq!(r.redact("COUCHDB_ADMIN_AUTH=abc123"), "COUCHDB_ADMIN_AUTH=[redacted]");
        assert_eq!(r.redact("token: xyz"), "token: [redacted]");
        assert_eq!(r.redact("Authorization: Basic Zm9vOmJhcg=="), "Authorization: Basic [redacted]");
        assert_eq!(r.redact("http://radu:pw@127.0.0.1:5984/x"), "http://[redacted]@127.0.0.1:5984/x");
        assert_eq!(r.redact("value supersecretvalue here"), "value [redacted] here");
        assert_eq!(r.redact("plain line"), "plain line");
    }

    #[test]
    fn constant_time_compare() {
        assert!(ct_eq("abc", "abc"));
        assert!(!ct_eq("abc", "abd"));
        assert!(!ct_eq("abc", "abcd"));
    }
}
