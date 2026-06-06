import json
import os
import subprocess
import time
import urllib.parse
from email import policy
from email.parser import BytesParser
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


BASE_DIR = os.path.realpath(os.environ.get("AGENTSCOPE_SANDBOX_WORKDIR", "/home/user"))


def safe_path(raw_path):
    if raw_path.startswith("/"):
        candidate = os.path.realpath(raw_path)
    else:
        candidate = os.path.realpath(os.path.join(BASE_DIR, raw_path))
    if os.path.commonpath([BASE_DIR, candidate]) != BASE_DIR:
        raise ValueError("path must stay within sandbox workspace")
    return candidate


class RuntimeHandler(BaseHTTPRequestHandler):
    server_version = "AgentScopeSandboxRuntime/1.0"

    def log_message(self, fmt, *args):
        print("%s - - [%s] %s" % (self.client_address[0], self.log_date_time_string(), fmt % args), flush=True)

    def send_json(self, status, payload):
        data = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path
        if path == "/":
            self.send_json(HTTPStatus.OK, {"status": "ok", "message": "Sandbox Runtime is active."})
            return
        if path.startswith("/download/"):
            self.handle_download(path[len("/download/") :])
            return
        if path.startswith("/list/"):
            self.handle_list(path[len("/list/") :])
            return
        if path.startswith("/exists/"):
            self.handle_exists(path[len("/exists/") :])
            return
        self.send_json(HTTPStatus.NOT_FOUND, {"message": "not found"})

    def do_POST(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/execute":
            self.handle_execute()
            return
        if parsed.path == "/upload":
            self.handle_upload()
            return
        self.send_json(HTTPStatus.NOT_FOUND, {"message": "not found"})

    def read_body(self):
        length = int(self.headers.get("Content-Length", "0"))
        return self.rfile.read(length)

    def handle_execute(self):
        try:
            payload = json.loads(self.read_body().decode("utf-8"))
            command = payload.get("command", "")
            process = subprocess.run(
                command,
                shell=True,
                executable="/bin/sh",
                capture_output=True,
                text=True,
                cwd=BASE_DIR,
            )
            self.send_json(
                HTTPStatus.OK,
                {"stdout": process.stdout, "stderr": process.stderr, "exit_code": process.returncode},
            )
        except Exception as exc:
            self.send_json(HTTPStatus.OK, {"stdout": "", "stderr": "Failed to execute command: %s" % exc, "exit_code": 1})

    def handle_upload(self):
        try:
            content_type = self.headers.get("Content-Type", "")
            body = self.read_body()
            message = BytesParser(policy=policy.default).parsebytes(
                ("Content-Type: %s\r\n\r\n" % content_type).encode("utf-8") + body
            )
            for part in message.iter_parts():
                filename = part.get_filename()
                if not filename:
                    continue
                target = safe_path(filename)
                os.makedirs(os.path.dirname(target), exist_ok=True)
                with open(target, "wb") as output:
                    output.write(part.get_payload(decode=True) or b"")
                self.send_json(HTTPStatus.OK, {"message": "File '%s' uploaded successfully." % filename})
                return
            self.send_json(HTTPStatus.BAD_REQUEST, {"message": "multipart field 'file' is required"})
        except Exception as exc:
            self.send_json(HTTPStatus.INTERNAL_SERVER_ERROR, {"message": "File upload failed: %s" % exc})

    def handle_download(self, encoded_path):
        try:
            target = safe_path(urllib.parse.unquote(encoded_path))
        except ValueError:
            self.send_json(HTTPStatus.FORBIDDEN, {"message": "Access denied"})
            return
        if not os.path.isfile(target):
            self.send_json(HTTPStatus.NOT_FOUND, {"message": "File not found"})
            return
        size = os.path.getsize(target)
        self.send_response(HTTPStatus.OK)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Content-Length", str(size))
        self.end_headers()
        with open(target, "rb") as source:
            while True:
                chunk = source.read(64 * 1024)
                if not chunk:
                    break
                self.wfile.write(chunk)

    def handle_list(self, encoded_path):
        try:
            target = safe_path(urllib.parse.unquote(encoded_path))
        except ValueError:
            self.send_json(HTTPStatus.FORBIDDEN, {"message": "Access denied"})
            return
        if not os.path.isdir(target):
            self.send_json(HTTPStatus.NOT_FOUND, {"message": "Path is not a directory"})
            return
        entries = []
        for name in sorted(os.listdir(target)):
            entry_path = os.path.join(target, name)
            stat = os.stat(entry_path)
            entries.append(
                {
                    "name": name,
                    "size": stat.st_size,
                    "type": "directory" if os.path.isdir(entry_path) else "file",
                    "mod_time": stat.st_mtime,
                }
            )
        self.send_json(HTTPStatus.OK, entries)

    def handle_exists(self, encoded_path):
        decoded = urllib.parse.unquote(encoded_path)
        try:
            target = safe_path(decoded)
        except ValueError:
            self.send_json(HTTPStatus.FORBIDDEN, {"message": "Access denied"})
            return
        self.send_json(HTTPStatus.OK, {"path": decoded, "exists": os.path.exists(target)})


if __name__ == "__main__":
    os.makedirs(BASE_DIR, exist_ok=True)
    server = ThreadingHTTPServer(("0.0.0.0", 8888), RuntimeHandler)
    print("AgentScope sandbox runtime listening on :8888 at %s" % time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), flush=True)
    server.serve_forever()
