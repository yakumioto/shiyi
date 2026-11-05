#!/usr/bin/env bash
# Real service / container + real TLS proxy + Cookie jar. Only the model is fake.
# No production credentials, deployment mutation, fixed ports or blind sleeps.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
python3 - "$ROOT" "${1:-process}" "${2:-shiyi:latest}" <<'PY'
import copy
import http.client
import http.cookiejar
import json
import os
from pathlib import Path
import re
import select
import signal
import socket
import ssl
import stat
import subprocess
import sys
import tempfile
import termios
import threading
import time
import urllib.error
import urllib.request
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ROOT, MODE, IMAGE = Path(sys.argv[1]), sys.argv[2], sys.argv[3]
assert MODE in ('process', '--image'), 'usage: smoke.sh [--image IMAGE]'
PASSWORD = ' smoke-密码-marker '
KEY = 'smoke-model-key-marker'
ORIGINAL = '我喜欢喝茶 smoke-private-original-marker'
SECRETS = [PASSWORD, KEY, ORIGINAL]
MODEL = {'calls': 0, 'unavailable': False}


def check(condition, message):
    if not condition:
        raise AssertionError(message)


def command(args, **kwargs):
    result = subprocess.run(args, capture_output=True, timeout=120, **kwargs)
    check(result.returncode == 0, 'fixture command failed: ' + str(args[0]))
    return result.stdout.decode().strip()


def eventually(probe, message, timeout=20):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if probe():
            return
        # Bounded readiness polling, never an assumed startup delay.
        threading.Event().wait(0.025)
    raise AssertionError(message)


class QuietHandler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass


class FakeModel(QuietHandler):
    def do_POST(self):
        MODEL['calls'] += 1
        if MODEL['unavailable']:
            self.send_error(503)
            return
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        messages = body['messages']
        system = next((m.get('content', '') for m in messages if m['role'] == 'system'), '')
        user = next((m.get('content', '') for m in reversed(messages) if m['role'] == 'user'), '')
        if '意图判定器' in system:
            kind = 'query' if user.startswith('QUERY:') else 'unrecognized' if user.startswith('SKIP:') else 'record'
            message = {'role': 'assistant', 'content': json.dumps({'kind': kind})}
        else:
            tools = [m for m in messages if m['role'] == 'tool']
            if not tools:
                message = {'role': 'assistant', 'content': '', 'tool_calls': [{'id': 'call_1', 'type': 'function', 'function': {'name': 'search_memory', 'arguments': json.dumps({'keywords': ['茶'], 'limit': 5})}}]}
            else:
                hits = json.loads(tools[-1]['content'] or '[]')
                message = {'role': 'assistant', 'content': json.dumps({'answer': '根据记录，你提到过茶。', 'citations': [h['time'] for h in hits]}, ensure_ascii=False)}
        payload = json.dumps({'model': 'fake', 'choices': [{'index': 0, 'message': message, 'finish_reason': 'tool_calls' if 'tool_calls' in message else 'stop'}]}, ensure_ascii=False).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)


class TLSProxy(QuietHandler):
    def forward(self):
        connection = http.client.HTTPConnection('127.0.0.1', self.server.upstream, timeout=5)
        try:
            body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
            connection.request(self.command, self.path, body, dict(self.headers))
            response = connection.getresponse()
            data = response.read()
            self.send_response(response.status)
            for key, value in response.getheaders():
                if key.lower() not in ('connection', 'transfer-encoding', 'content-length'):
                    self.send_header(key, value)
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        finally:
            connection.close()
    do_GET = forward
    do_POST = forward


def serve_thread(server):
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server


def hash_password(binary, env, first=PASSWORD, second=PASSWORD, interrupt=False):
    # os.openpty preserves EACCES rather than pty.openpty's misleading fallback
    # error "out of pty devices". A denial is a hard failure, never a skip.
    master, slave = os.openpty()
    process = subprocess.Popen([str(binary), 'hash-password'], stdin=slave, stdout=subprocess.PIPE, stderr=slave, env=env)
    transcript = bytearray()
    try:
        def prompt(text):
            deadline = time.monotonic() + 10
            while text.encode() not in transcript:
                check(process.poll() is None, 'hash-password exited before prompt')
                remaining = deadline - time.monotonic()
                check(remaining > 0, 'hash-password prompt timed out')
                ready, _, _ = select.select([master], [], [], remaining)
                check(ready, 'hash-password terminal timed out')
                transcript.extend(os.read(master, 4096))
            eventually(lambda: not (termios.tcgetattr(slave)[3] & termios.ECHO), 'password input still echoes')
        prompt('密码: ')
        if interrupt:
            process.send_signal(signal.SIGTERM)
        else:
            os.write(master, first.encode() + b'\n')
            if first and len(first.encode()) <= 72:
                prompt('再次输入密码: ')
                os.write(master, second.encode() + b'\n')
        output, _ = process.communicate(timeout=10)
        while select.select([master], [], [], 0)[0]:
            transcript.extend(os.read(master, 4096))
        for value in (first, second):
            if value:
                check(value.encode() not in transcript, 'terminal echoed password')
        check(termios.tcgetattr(slave)[3] & termios.ECHO, 'terminal echo not restored')
        success = not interrupt and first and len(first.encode()) <= 72 and first == second
        if success:
            check(process.returncode == 0 and re.fullmatch(rb'\$2[aby]\$12\$[./A-Za-z0-9]{53}\n', output), 'hash-password did not produce only a cost-12 bcrypt hash')
            result = output.decode().strip()
            SECRETS.append(result)
            return result
        check(process.returncode != 0 and not output, 'invalid/aborted password produced a hash')
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(master)
        os.close(slave)


with tempfile.TemporaryDirectory(prefix='shiyi-smoke-') as directory:
    work = Path(directory)
    binary = work / 'shiyi'
    command(['go', 'build', '-o', str(binary), './cmd/shiyi'], cwd=ROOT)
    # Ignore ambient deployment settings; all fixtures are test-only.
    env = {k: v for k, v in os.environ.items() if not k.startswith(('SHIYI_', 'DEEPSEEK_'))}
    env['SHIYI_CONFIG'] = str(work / 'nonexistent-config')
    password_hash = hash_password(binary, env)
    hash_password(binary, env, second='different-password-marker')
    hash_password(binary, env, first='')
    hash_password(binary, env, first='密' * 25)
    hash_password(binary, env, interrupt=True)
    for args in ([], ['private-input-marker'], ['--server', 'private-input-marker'], ['hash-password', PASSWORD], ['hash-password', '--password=' + PASSWORD], ['hash-password']):
        result = subprocess.run([str(binary), *args], input=PASSWORD.encode(), capture_output=True, env=env, timeout=5)
        check(result.returncode == 2, 'removed client or non-TTY hashing did not fail')
        check(PASSWORD.encode() not in result.stdout + result.stderr and b'private-input-marker' not in result.stdout + result.stderr, 'management error leaked input')
    for option in ('--help', '--version'):
        command([str(binary), option], env=env)
    print('PASS terminal hashing: no echo, confirmation, limits, signal restore, no serve config; old CLI refused', flush=True)

    cert, key = work / 'cert.pem', work / 'key.pem'
    command(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-keyout', str(key), '-out', str(cert), '-subj', '/CN=127.0.0.1', '-addext', 'subjectAltName=IP:127.0.0.1'])
    proxy = ThreadingHTTPServer(('127.0.0.1', 0), TLSProxy)
    tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    tls.load_cert_chain(cert, key)
    proxy.socket = tls.wrap_socket(proxy.socket, server_side=True)
    serve_thread(proxy)
    origin = 'https://127.0.0.1:' + str(proxy.server_port)
    client_tls = ssl.create_default_context(cafile=str(cert))
    model = serve_thread(ThreadingHTTPServer(('127.0.0.1', 0), FakeModel))
    data, state = work / 'memory', work / 'auth'
    data.mkdir(mode=0o700)
    env.update(SHIYI_AUTH_PASSWORD_HASH=password_hash, SHIYI_AUTH_PUBLIC_ORIGIN=origin,
               SHIYI_AUTH_STATE_DIR=str(state), SHIYI_DATA_DIR=str(data),
               DEEPSEEK_API_KEY=KEY, SHIYI_API_KEY=KEY)
    source = (ROOT / 'config.example.toml').read_text().replace('https://api.deepseek.com', 'http://127.0.0.1:' + str(model.server_port))
    config = work / 'config.toml'
    config.write_text(source)
    env['SHIYI_CONFIG'] = str(config)
    process, container = None, None
    logs = work / 'serve.log'
    log_file = logs.open('ab')
    volumes = []
    docker_logs = []
    prefix = 'shiyi-smoke-' + uuid.uuid4().hex

    def docker(*args, check_ok=True, custom_env=None):
        result = subprocess.run(['docker', *args], capture_output=True, env=custom_env or env, timeout=120)
        if check_ok:
            check(result.returncode == 0, 'docker fixture operation failed: ' + args[0])
        return result

    def health():
        if MODE == 'process':
            check(process.poll() is None, 'service exited before readiness (check safe configuration)')
        else:
            running = docker('inspect', '--format', '{{.State.Running}}', container).stdout.strip()
            check(running == b'true', 'container exited before readiness')
        try:
            connection = http.client.HTTPConnection('127.0.0.1', proxy.upstream, timeout=0.5)
            connection.request('GET', '/healthz')
            response = connection.getresponse()
            return response.status == 200 and json.loads(response.read()) == {'status': 'ok'}
        except (OSError, http.client.HTTPException):
            return False
        finally:
            connection.close()

    def start():
        global process, container
        if MODE == 'process':
            with socket.socket() as reservation:
                reservation.bind(('127.0.0.1', 0))
                port = reservation.getsockname()[1]
            env['SHIYI_LISTEN'] = '127.0.0.1:' + str(port)
            proxy.upstream = port
            process = subprocess.Popen([str(binary), 'serve'], stdout=log_file, stderr=log_file, env=env)
        else:
            # No anonymous switch, no test hash baked into the image. Docker
            # assigns the host port, named volumes survive the restart test.
            container = prefix
            docker('run', '-d', '--name', container, '-p', '127.0.0.1::8787',
                   '-v', volumes[0] + ':/data', '-v', volumes[1] + ':/auth',
                   '-e', 'SHIYI_API_KEY', '-e', 'SHIYI_AUTH_PASSWORD_HASH', '-e', 'SHIYI_AUTH_PUBLIC_ORIGIN', IMAGE)
            binding = docker('port', container, '8787/tcp').stdout.decode().strip()
            proxy.upstream = int(binding.rsplit(':', 1)[1])
        eventually(health, 'healthz never became ready')

    def stop():
        global process, container
        if process is not None:
            if process.poll() is None:
                process.send_signal(signal.SIGTERM)
                try:
                    code = process.wait(timeout=12)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
                    raise AssertionError('service did not shut down gracefully')
                check(code == 0, 'service shutdown failed')
            process = None
        if container is not None:
            docker('stop', '--time', '12', container)
            check(docker('inspect', '--format', '{{.State.ExitCode}}', container).stdout.strip() == b'0', 'container shutdown failed')
            docker_logs.append(docker('logs', container).stdout + docker('logs', container).stderr)
            docker('rm', '-v', container)
            container = None

    def browser(jar=None):
        if jar is None:
            jar = http.cookiejar.CookieJar()
        return urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=client_tls), urllib.request.HTTPCookieProcessor(jar)), jar

    def request(client, path, expected=200, body=None, token=None, request_origin=origin, extra=None):
        headers = dict(extra or {})
        if body is not None:
            headers.update({'Content-Type': 'application/json', 'Origin': request_origin})
            if token is not None:
                headers['X-CSRF-Token'] = token
            body = body.encode() if isinstance(body, str) else json.dumps(body, ensure_ascii=False).encode()
        req = urllib.request.Request(origin + path, data=body, headers=headers)
        try:
            response = client.open(req, timeout=10)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            check(response.status == expected, path + ': unexpected HTTP status ' + str(response.status))
            payload = response.read()
            if path.startswith('/auth/') or path == '/messages':
                check(response.headers.get('Cache-Control') == 'no-store', 'private response is cacheable')
            if path == '/':
                check('text/html' in response.headers.get('Content-Type', ''), 'page not HTML')
                return None
            for cookie in response.headers.get_all('Set-Cookie', []):
                if cookie.startswith('__Host-'):
                    check(all(flag in cookie for flag in ('Secure', 'HttpOnly', 'SameSite=Strict', 'Path=/')) and 'Domain=' not in cookie, 'unsafe production cookie attributes')
            result = json.loads(payload) if payload else None
            if isinstance(result, dict) and 'csrf_token' in result:
                SECRETS.append(result['csrf_token'])
            return result

    def login(client, jar):
        session = request(client, '/auth/session')
        SECRETS.extend(c.value for c in jar)
        check(session['mode'] == 'password', 'smoke accidentally used anonymous mode')
        session = request(client, '/auth/login', body={'password': PASSWORD}, token=session['csrf_token'])
        check(session['authenticated'], 'correct password did not log in')
        check(any(c.name == '__Host-shiyi-session' and c.secure for c in jar), 'production session cookie not accepted over HTTPS')
        SECRETS.extend(c.value for c in jar)
        return session['csrf_token']

    try:
        # Safe defaults and invalid deployment values must fail, never go live.
        cases = ({'SHIYI_AUTH_PASSWORD_HASH': ''}, {'SHIYI_AUTH_PASSWORD_HASH': 'invalid-hash-marker'},
                 {'SHIYI_AUTH_PUBLIC_ORIGIN': 'http://insecure.example'}, {'SHIYI_AUTH_TOKEN': 'legacy-token-marker'})
        for overrides in cases:
            bad_env = dict(env, **overrides)
            if MODE == 'process':
                result = subprocess.run([str(binary), 'serve'], env=bad_env, capture_output=True, timeout=5)
            else:
                args = ['run', '--rm', '-e', 'SHIYI_API_KEY', '-e', 'SHIYI_AUTH_PASSWORD_HASH', '-e', 'SHIYI_AUTH_PUBLIC_ORIGIN']
                if 'SHIYI_AUTH_TOKEN' in overrides:
                    args += ['-e', 'SHIYI_AUTH_TOKEN']
                result = docker(*args, IMAGE, check_ok=False, custom_env=bad_env)
            check(result.returncode != 0, 'unsafe deployment started')
            for secret in SECRETS + ['invalid-hash-marker', 'legacy-token-marker']:
                check(secret.encode() not in result.stdout + result.stderr, 'startup error leaked credentials')
        if MODE == '--image':
            for name in (prefix + '-memory', prefix + '-auth'):
                docker('volume', 'create', name)
                volumes.append(name)
        start()
        client, jar = browser()
        request(client, '/')
        request(client, '/messages', 401, body='{bad json')
        request(client, '/messages', 401, body={'content': ORIGINAL}, extra={'Authorization': 'Bearer old-token-marker'})
        check(MODEL['calls'] == 0 and not list(data.iterdir()), 'unauthorized request reached model/storage')
        csrf = login(client, jar)
        request(client, '/messages', 403, body={'content': ORIGINAL}, token=csrf, request_origin='https://evil.example')
        request(client, '/messages', 403, body={'content': ORIGINAL}, token='wrong-csrf-marker')
        request(client, '/messages', 400, body='{bad json', token=csrf)
        request(client, '/messages', 400, body={'content': '   '}, token=csrf)
        print('PASS safe startup, TLS login, secure Cookie, unauthorized/Bearer/Origin/CSRF refusal, authorized 400', flush=True)

        if MODE == 'process':
            record = request(client, '/messages', 201, body={'content': ORIGINAL}, token=csrf)
            check(record['kind'] == 'record' and record['version'] == 1, 'record contract changed')
            rows = [json.loads(line) for p in data.glob('*.jsonl') for line in p.read_text().splitlines()]
            check(len(rows) == 1 and set(rows[0]) == {'version', 'content', 'time'} and rows[0]['content'] == ORIGINAL, 'stored original text or row shape changed')
            before = {p.name: p.read_bytes() for p in data.iterdir()}
            skipped = request(client, '/messages', body={'content': 'SKIP:随便聊聊'}, token=csrf)
            check(skipped['kind'] == 'unrecognized', 'unrecognized contract changed')
            query = request(client, '/messages', body={'content': 'QUERY:我喜欢什么茶？'}, token=csrf)
            check(query['kind'] == 'query' and query['status'] == 'ok' and query['citations'] == [{'time': record['time'], 'content': ORIGINAL}], 'query citation lost original evidence')
            check(before == {p.name: p.read_bytes() for p in data.iterdir()}, 'query/unrecognized persisted data')
            MODEL['unavailable'] = True
            failure = request(client, '/messages', 503, body={'content': 'model-failure-original-marker'}, token=csrf)
            check(failure['error']['code'] == 'model_unavailable', 'model failure mapping changed')
            check(before == {p.name: p.read_bytes() for p in data.iterdir()}, 'model failure persisted unclassified input')
            check(stat.S_IMODE(state.stat().st_mode) == 0o700, 'auth directory permissions not 0700')
            check(all(stat.S_IMODE(p.stat().st_mode) == 0o600 for p in state.iterdir() if p.is_file()), 'auth file permissions not 0600')
            print('PASS original record/query/citation/no-write and model failure boundaries; independent private auth state', flush=True)
        else:
            # Inspect permissions as the image's non-root runtime user; no root workaround.
            check(docker('exec', container, 'id', '-u').stdout.strip() == b'1000', 'container is not running as uid 1000')
            check(docker('exec', container, 'stat', '-c', '%a:%u', '/auth').stdout.strip() == b'700:1000', 'auth volume has unsafe permissions')
            check(docker('exec', container, 'sh', '-c', 'for f in /auth/*; do test "$(stat -c %a "$f")" = 600 || exit 1; done').returncode == 0, 'auth file permission check failed')
            check(docker('exec', container, 'sh', '-c', 'test -z "$(ls -A /data)"').returncode == 0, 'authentication polluted memory volume')
            docker('exec', container, 'wget', '-q', '-O', '/dev/null', 'http://127.0.0.1:8787/healthz')
            print('PASS non-root container, independent auth volume 0700/files 0600, internal healthcheck', flush=True)

        stop()
        start()
        check(request(client, '/auth/session')['authenticated'], 'normal restart lost login or failed to release state lock')
        revoked_client, _ = browser(copy.deepcopy(jar))
        request(client, '/auth/logout', 204, body='', token=csrf)
        request(revoked_client, '/messages', 401, body={'content': ORIGINAL}, token=csrf)
        stop()
        start()
        check(not request(revoked_client, '/auth/session')['authenticated'], 'copied revoked cookie revived after restart')
        csrf = login(client, jar)  # Still works while the fake model is unavailable.
        request(client, '/auth/logout-all', 204, body={'password': PASSWORD}, token=csrf)
        check(not request(client, '/auth/session')['authenticated'], 'logout-all did not revoke access')
        login(client, jar)
        stop()
        replacement = hash_password(binary, env)
        check(replacement != password_hash, 'rehash did not generate a new salt')
        env['SHIYI_AUTH_PASSWORD_HASH'] = replacement
        start()
        check(not request(client, '/auth/session')['authenticated'], 'same-password rehash did not invalidate old login')
        stop()
        log_file.flush()
        output = logs.read_bytes() + b''.join(docker_logs)
        for secret in SECRETS + ['model-failure-original-marker']:
            check(secret.encode() not in output, 'service log leaked private data')
        print('PASS graceful restart, persistent login/revocation, logout-all, same-password rehash invalidation, secret-free logs', flush=True)
        print('ALL AUTHENTICATED ' + ('CONTAINER' if MODE == '--image' else 'PROCESS') + ' SMOKE CHECKS PASSED', flush=True)
    finally:
        if process is not None and process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=12)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        if container is not None:
            docker('rm', '-f', '-v', container, check_ok=False)
        for name in volumes:
            check(name.startswith(prefix + '-'), 'refusing to remove unrelated volume')
            docker('volume', 'rm', name, check_ok=False)
        log_file.close()
        proxy.shutdown()
        proxy.server_close()
        model.shutdown()
        model.server_close()
PY
