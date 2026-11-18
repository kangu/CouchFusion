//! One-shot transport to the isolated runner. Only the socket name crosses the filesystem.
use std::io::{BufRead, BufReader, Read, Write};
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::Duration;

use anyhow::{Context, Result, bail, ensure};
use base64::{Engine, engine::general_purpose::STANDARD};
use serde_json::{Value, json};
use tokio::{io::AsyncWriteExt, net::UnixListener};

use crate::{
    agent::{RunSpec, RunnerArgs, build_request_prompt},
    config::RunnerKind,
    error::ApiError,
};

const MAX_BYTES: usize = 10 * 1024 * 1024;

pub fn validate(data: &str) -> Result<(), ApiError> {
    let invalid = || ApiError::validation("Attach one PNG, JPEG or WebP screenshot up to 10 MiB.");
    let (kind, encoded) = data.split_once(',').ok_or_else(invalid)?;
    if encoded.len() > MAX_BYTES.div_ceil(3) * 4 {
        return Err(invalid());
    }
    let bytes = STANDARD.decode(encoded).map_err(|_| invalid())?;
    let valid = match kind {
        "data:image/png;base64" => bytes.starts_with(b"\x89PNG\r\n\x1a\n"),
        "data:image/jpeg;base64" => bytes.starts_with(b"\xff\xd8\xff"),
        "data:image/webp;base64" => bytes.starts_with(b"RIFF") && bytes.get(8..12) == Some(b"WEBP"),
        _ => false,
    };
    if !valid || bytes.len() > MAX_BYTES {
        return Err(invalid());
    }
    Ok(())
}

pub struct Handoff {
    listener: UnixListener,
    path: PathBuf,
    data: String,
    uid: u32,
}
impl Handoff {
    pub fn bind(spec: &Path, id: &str, data: String, runner: RunnerKind) -> Result<Self> {
        let uid = match runner {
            RunnerKind::ProcessGroup => nix::unistd::geteuid(),
            RunnerKind::Systemd => {
                nix::unistd::User::from_name("cfagent")?
                    .context("cfagent user is missing")?
                    .uid
            }
        };
        let path = spec.with_file_name(format!("{id}.sock"));
        std::fs::create_dir_all(path.parent().context("missing socket directory")?)?;
        let listener = UnixListener::bind(&path)?;
        let this = Self {
            listener,
            path,
            data,
            uid: uid.as_raw(),
        };
        std::fs::set_permissions(&this.path, std::fs::Permissions::from_mode(0o666))?;
        Ok(this)
    }
    pub async fn send(self) -> Result<()> {
        tokio::time::timeout(Duration::from_secs(30), async {
            loop {
                let (mut stream, _) = self.listener.accept().await?;
                if stream.peer_cred()?.uid() == self.uid {
                    stream.write_all(self.data.as_bytes()).await?;
                    return stream.shutdown().await;
                }
            }
        })
        .await??;
        Ok(())
    }
}
impl Drop for Handoff {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.path);
    }
}

// Use the native image input on an ephemeral app-server thread. Never forward raw protocol
// traffic to stdout or stderr: user-message notifications may include the entire image.
pub fn run(args: &RunnerArgs, spec: &RunSpec, cwd: &Path) -> Result<i32> {
    let socket = std::os::unix::net::UnixStream::connect(
        args.spec_dir.join(format!("{}.sock", spec.job_id)),
    )?;
    let mut image = String::new();
    socket.take((15 << 20) + 1).read_to_string(&mut image)?;
    ensure!(image.len() <= 15 << 20, "screenshot too large");
    let mut child = Command::new(&args.codex)
        .arg("app-server")
        .args([
            "-c",
            "notify=[]",
            "-c",
            "sandbox_workspace_write.network_access=false",
        ])
        .current_dir(cwd)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()?;
    let outcome = turn(
        child.stdin.take().unwrap(),
        child.stdout.take().unwrap(),
        spec,
        cwd,
        image,
    );
    let _ = child.kill();
    let _ = child.wait();
    outcome
}

fn send(writer: &mut impl Write, msg: Value) -> Result<()> {
    serde_json::to_writer(&mut *writer, &msg)?;
    writer.write_all(b"\n")?;
    writer.flush()?;
    Ok(())
}

fn turn(
    mut stdin: impl Write,
    stdout: impl Read,
    spec: &RunSpec,
    cwd: &Path,
    image: String,
) -> Result<i32> {
    let mut image = Some(image);
    send(
        &mut stdin,
        json!({"id":1,"method":"initialize","params":{"clientInfo":{"name":"couchfusion","version":"1"}}}),
    )?;
    let mut usage = Value::Null;
    for line in BufReader::new(stdout).lines() {
        let msg: Value = serde_json::from_str(&line?)?;
        if msg.get("error").is_some() {
            bail!("Codex rejected the screenshot request");
        }
        match msg["id"].as_u64() {
            Some(1) if msg.get("result").is_some() => {
                send(&mut stdin, json!({"method":"initialized"}))?;
                send(
                    &mut stdin,
                    json!({"id":2,"method":"thread/start","params":{"cwd":cwd,"ephemeral":true,
                    "sandbox":"workspace-write","approvalPolicy":"never","model":spec.model}}),
                )?;
            }
            Some(2) if msg.get("result").is_some() => {
                let thread = msg
                    .pointer("/result/thread/id")
                    .and_then(Value::as_str)
                    .context("missing thread ID")?;
                send(
                    &mut stdin,
                    json!({"id":3,"method":"turn/start","params":{"threadId":thread,"effort":spec.reasoning_effort,
                    "input":[{"type":"text","text":build_request_prompt(&spec.prompt)},{"type":"image","url":image.take().context("duplicate thread response")?}]}}),
                )?;
            }
            _ if msg.get("method").is_some() && msg.get("id").is_some() => {
                send(
                    &mut stdin,
                    json!({"id":msg["id"],"error":{"code":-32601,"message":"Interactive requests are unavailable"}}),
                )?;
            }
            _ => match msg["method"].as_str() {
                Some("turn/started") => println!("{}", json!({"type":"turn.started"})),
                Some("item/completed")
                    if msg.pointer("/params/item/type") == Some(&json!("agentMessage")) =>
                {
                    println!(
                        "{}",
                        json!({"type":"item.completed","item":{"type":"agent_message","text":msg.pointer("/params/item/text")}})
                    )
                }
                Some("thread/tokenUsage/updated") => {
                    if let Some(u) = msg.pointer("/params/tokenUsage/total") {
                        usage = json!({"input_tokens":u["inputTokens"],"cached_input_tokens":u["cachedInputTokens"],"output_tokens":u["outputTokens"]});
                    }
                }
                Some("turn/completed") => {
                    if msg.pointer("/params/turn/status") == Some(&json!("completed")) {
                        println!("{}", json!({"type":"turn.completed","usage":usage}));
                        return Ok(0);
                    }
                    println!(
                        "{}",
                        json!({"type":"turn.failed","message":"Codex could not complete the screenshot request."})
                    );
                    return Ok(1);
                }
                _ => {}
            },
        }
    }
    bail!("Codex closed before completing the screenshot request")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn image_is_a_native_input_on_an_ephemeral_thread() {
        let data = "data:image/png;base64,iVBORw0KGgo=";
        assert!(validate(data).is_ok());
        assert!(validate("data:image/jpeg;base64,iVBORw0KGgo=").is_err());
        let spec: RunSpec = serde_json::from_value(json!({
            "kind":"request", "jobId":"abc", "screenshot":true, "prompt":"Inspect this layout"
        }))
        .unwrap();
        let replies = [
            json!({"id":1,"result":{}}),
            json!({"id":2,"result":{"thread":{"id":"test-thread"}}}),
            json!({"method":"turn/completed","params":{"turn":{"status":"completed"}}}),
        ]
        .map(|v| v.to_string())
        .join("\n");
        let mut requests = Vec::new();
        assert_eq!(
            turn(
                &mut requests,
                replies.as_bytes(),
                &spec,
                Path::new("/app"),
                data.into()
            )
            .unwrap(),
            0
        );
        let messages: Vec<Value> = serde_json::Deserializer::from_slice(&requests)
            .into_iter()
            .collect::<Result<_, _>>()
            .unwrap();
        assert_eq!(messages[2]["params"]["ephemeral"], true);
        assert_eq!(messages[2]["params"]["sandbox"], "workspace-write");
        assert_eq!(
            messages[3]["params"]["input"][1],
            json!({"type":"image","url":data})
        );
        assert!(
            messages[3]["params"]["input"][0]["text"]
                .as_str()
                .unwrap()
                .contains("Inspect this layout")
        );
    }
}
