#!/usr/bin/env python3
"""Fake `nuxt dev` for local smoke tests of couchfusion-server.

* token-guarded /__couchfusion/control/ping and /component-usage (404 without the token)
* emulates the content layer's registry watcher (regenerates component-definitions.{ts,json})
* answers Upgrade: websocket with 101 and echoes bytes (HMR passthrough check)
* everything else: a small HTML page that echoes the request headers it received
"""
import hashlib, http.server, json, os, re, socketserver, sys, threading, time

PORT = int(os.environ.get("CF_DEV_PORT", "14401"))
TOKEN = os.environ.get("COUCHFUSION_CONTROL_TOKEN", "")
APP = os.getcwd()
CONTENT = os.path.join(APP, "app/components/content")
OUT = os.path.join(APP, "app/content-builder")

def kebab(stem):
    s = re.sub(r"([a-z0-9])([A-Z])", r"\1-\2", stem)
    s = re.sub(r"([A-Z])([A-Z][a-z])", r"\1-\2", s)
    return re.sub(r"[\s_-]+", "-", s).lower()

def regenerate():
    files = sorted(f for f in os.listdir(CONTENT) if f.endswith(".vue"))
    comps = sorted(({"id": kebab(f[:-4]), "label": f[:-4], "description": "", "source": "app/components/content/" + f,
                     "propCount": 0, "allowChildren": False} for f in files), key=lambda c: c["id"])
    ts = "export default " + json.dumps([c["id"] for c in comps]) + "\n"
    with open(os.path.join(OUT, "component-definitions.ts.tmp"), "w") as fh: fh.write(ts)
    os.replace(os.path.join(OUT, "component-definitions.ts.tmp"), os.path.join(OUT, "component-definitions.ts"))
    manifest = {"version": 1, "typescriptSha256": hashlib.sha256(ts.encode()).hexdigest(), "components": comps}
    with open(os.path.join(OUT, "component-definitions.json.tmp"), "w") as fh: json.dump(manifest, fh)
    os.replace(os.path.join(OUT, "component-definitions.json.tmp"), os.path.join(OUT, "component-definitions.json"))
    print(f"[registry] Completed ({len(comps)} components).", flush=True)

def watcher():
    seen = None
    while True:
        cur = sorted(f for f in os.listdir(CONTENT) if f.endswith(".vue"))
        if cur != seen:
            if seen is not None:
                time.sleep(0.3)
            regenerate(); seen = cur
        time.sleep(0.2)

class H(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, fmt, *a): print("[dev] " + fmt % a, flush=True)
    def send_json(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    def do_GET(self):
        if self.path.startswith("/__couchfusion/control/"):
            if not TOKEN or self.headers.get("x-couchfusion-control-token") != TOKEN:
                return self.send_json(404, {"error": "not found"})
            if self.path.startswith("/__couchfusion/control/ping"):
                return self.send_json(200, {"ready": True})
            cid = self.path.split("id=", 1)[-1]
            pages = [{"id": "page-/", "path": "/", "title": "Home"}] if cid == "network-hero" else []
            return self.send_json(200, {"pages": pages, "scanned": 3})
        if self.headers.get("Upgrade", "").lower() == "websocket":
            self.send_response(101); self.send_header("Upgrade", "websocket"); self.send_header("Connection", "Upgrade")
            self.end_headers(); self.wfile.flush()
            while True:
                data = self.connection.recv(4096)
                if not data: break
                self.connection.sendall(b"echo:" + data)
            self.close_connection = True; return
        body = ("<!doctype html><html><body><h1>fake dev</h1><pre>" +
                json.dumps({k.lower(): v for k, v in self.headers.items()}) + "</pre></body></html>").encode()
        self.send_response(200); self.send_header("Content-Type", "text/html")
        self.send_header("X-Frame-Options", "DENY"); self.send_header("Content-Length", str(len(body)))
        self.end_headers(); self.wfile.write(body)

class S(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True; allow_reuse_address = True

if __name__ == "__main__":
    if "--once" in sys.argv:
        # Fake `cf-registry@` oneshot: regenerate the registry once and exit.
        regenerate()
        sys.exit(0)
    print(f"fake dev on 127.0.0.1:{PORT} token={'set' if TOKEN else 'missing'}", flush=True)
    threading.Thread(target=watcher, daemon=True).start()
    time.sleep(1.0)  # emulate warm-up
    S(("127.0.0.1", PORT), H).serve_forever()
