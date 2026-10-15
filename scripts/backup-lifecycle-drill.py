#!/usr/bin/env python3
"""Run generated backup lifecycle acceptance in uniquely owned Docker resources.

Requires Docker, Go, and network access to build the disposable image. Never
reads .env.local, development media, or development database credentials. The
second directory is a local replica simulation, not evidence of offsite safety.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import struct
import subprocess
import tempfile
import time
import zlib
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

REPO = Path(__file__).resolve().parents[1]

DELAY_SCRIPT = '''import importlib.util, pathlib, time
spec = importlib.util.spec_from_file_location("proof", "/opt/kura/scripts/online_backup_proof.py")
real = importlib.util.module_from_spec(spec)
spec.loader.exec_module(real)
open_postgres_snapshot = real.open_postgres_snapshot
def run_online_backup(media, output, opener):
    def wait(ready):
        if pathlib.Path("/state/slow-enabled").exists():
            pathlib.Path("/state/snapshot-open").write_text("open")
            while not pathlib.Path("/state/release-gate").exists():
                time.sleep(0.05)
    return real.run_online_backup(media, output, opener, after_ready=wait)
'''


def png(width=16, height=16, level=6):
    def chunk(kind, data):
        return struct.pack('!I', len(data)) + kind + data + struct.pack('!I', zlib.crc32(kind + data))
    pixels = (b'\x00' + b'\x31\x62\x93' * width) * height
    return b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('!2I5B', width, height, 8, 2, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(pixels, level)) + chunk(b'IEND', b'')


class Drill:
    def __init__(self):
        self.name = 'kura_backup_drill_' + secrets.token_hex(6)
        self.image = self.name + ':fixture'
        self.network = self.name + '_network'
        self.volume = self.name + '_state'
        self.db = self.name + '_db'
        self.apis = [self.name + '_a', self.name + '_b']
        self.password = secrets.token_urlsafe(24)
        self.token = secrets.token_urlsafe(24)
        self.containers = []
        self.urls = []
        self.fixture = tempfile.TemporaryDirectory(prefix=self.name + '_')
        self.environment = os.environ.copy()
        self.environment.update(POSTGRES_USER='kura', POSTGRES_PASSWORD=self.password, POSTGRES_DB='kura_drill',
                                DATABASE_URL='postgresql://kura:' + self.password + '@' + self.db + ':5432/kura_drill?sslmode=disable',
                                KURA_API_TOKEN=self.token)

    def run(self, args, *, data=None, env=None, timeout=120, check=True):
        result = subprocess.run(args, input=data, env=env or self.environment, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=timeout)
        if check and result.returncode:
            # Arguments/env/tool logs can contain connection data. Keep failures generic.
            detail=result.stderr.decode('utf-8','replace')[-1200:]
            for value in (self.password,self.token,self.environment['DATABASE_URL']):
                detail=detail.replace(value,'[redacted]')
            raise RuntimeError('fixture command failed: ' + args[0] + ' (exit ' + str(result.returncode) + '): ' + detail)
        return result

    def docker(self, *args, **kwargs):
        return self.run(['docker', *args], **kwargs)

    def execute(self, script, *, container=None):
        return self.docker('exec', '-i', container or self.apis[0], 'python3', '-', data=script.encode()).stdout.decode().strip()

    def sql(self, sql, database='kura_drill'):
        return self.docker('exec', '-i', self.db, 'psql', '-X', '-A', '-t', '-v', 'ON_ERROR_STOP=1', '-U', 'kura', '-d', database,
                           data=sql.encode()).stdout.decode().strip()

    def request(self, which, method, path, body=None, content_type='application/json', expected=200):
        if isinstance(body, dict):
            body = json.dumps(body).encode()
        req = Request(self.urls[which] + path, data=body, method=method,
                      headers={'Authorization': 'Bearer ' + self.token, 'Content-Type': content_type})
        try:
            response = urlopen(req, timeout=10)
        except HTTPError as error:
            response = error
        with response:
            data = response.read()
            if response.status != expected:
                raise AssertionError('unexpected fixture HTTP status: ' + str(response.status) + ' for ' + path)
        return json.loads(data) if data else None

    def wait_job(self, id, status=None):
        deadline = time.monotonic() + 40
        while time.monotonic() < deadline:
            job = self.request(1, 'GET', '/api/recovery/backups/' + id)
            if job['status'] != 'running':
                if status is not None and job['status'] != status:
                    raise AssertionError('unexpected terminal fixture job state: ' + job['status'])
                return job
            time.sleep(.1)
        raise AssertionError('fixture backup did not finish within 40s')

    def wait_file(self, name):
        for _ in range(100):
            if self.execute('from pathlib import Path\nprint(Path(' + repr('/state/' + name) + ').exists())') == 'True':
                return
            time.sleep(.05)
        raise AssertionError('fixture snapshot did not open')

    def wait_api(self, index):
        for _ in range(150):
            try:
                with urlopen(self.urls[index] + '/health', timeout=1) as response:
                    if response.status == 200:
                        return
            except (URLError, OSError):
                time.sleep(.1)
        logs=self.docker('logs',self.apis[index],check=False)
        detail=(logs.stdout+logs.stderr).decode('utf-8','replace')[-2000:]
        for value in (self.password,self.token,self.environment['DATABASE_URL']):detail=detail.replace(value,'[redacted]')
        raise AssertionError('fixture API did not become ready: '+detail)

    def setup(self):
        assert shutil.which('go') and shutil.which('docker'), 'Go and Docker are required'
        architecture = self.docker('info', '--format', '{{.Architecture}}').stdout.decode().strip()
        architecture = {'aarch64': 'arm64', 'x86_64': 'amd64', 'arm64': 'arm64', 'amd64': 'amd64'}.get(architecture)
        assert architecture, 'unsupported Docker fixture architecture'
        context = Path(self.fixture.name)
        (context / 'server').mkdir()
        (context / 'scripts').mkdir()
        build_env = self.environment.copy()
        build_env.update(GOOS='linux', GOARCH=architecture, CGO_ENABLED='0')
        for binary in ('kura-server', 'kura-account'):
            self.run(['go', '-C', str(REPO / 'server'), 'build', '-o', str(context / 'server' / binary), './cmd/' + binary], env=build_env)
        for name in ('library-backup.py', 'online_backup_proof.py'):
            shutil.copy2(REPO / 'scripts' / name, context / 'scripts' / name)
        self.docker('build', '-t', self.image, '-f', str(REPO / 'scripts' / 'backup-lifecycle.Dockerfile'), str(context), timeout=360)
        self.docker('network', 'create', self.network)
        self.docker('volume', 'create', self.volume)
        self.containers.append(self.db)
        self.docker('run', '-d', '--name', self.db, '--network', self.network, '--env', 'POSTGRES_USER', '--env', 'POSTGRES_PASSWORD', '--env', 'POSTGRES_DB', 'postgres:17')
        for _ in range(100):
            if self.docker('exec', self.db, 'pg_isready', '-h', '127.0.0.1', '-U', 'kura', '-d', 'kura_drill', check=False).returncode == 0:
                break
            time.sleep(.1)
        else:
            raise AssertionError('disposable PostgreSQL did not start')
        for migration in sorted((REPO / 'db/migrations').glob('*.sql')):
            self.sql(migration.read_text())
        for index, container in enumerate(self.apis):
            self.containers.append(container)
            self.docker('run', '-d', '--name', container, '--network', self.network, '-p', '127.0.0.1::8080',
                        '--mount', 'type=volume,src=' + self.volume + ',dst=/state',
                        '--env', 'DATABASE_URL', '--env', 'KURA_API_TOKEN', '--env', 'KURA_PRIVATE_LIBRARY=true',
                        '--env', 'KURA_BACKUP_INTERVAL=1h', '--env', 'KURA_BACKUP_KEEP=2',
                        '--env', 'KURA_ONLINE_BACKUP_SCRIPT=/state/delay.py', self.image)
            port = self.docker('port', container, '8080/tcp').stdout.decode().strip().split(':')[-1]
            self.urls.append('http://127.0.0.1:' + str(int(port)))
            self.wait_api(index)
        self.execute('from pathlib import Path\nPath("/state/delay.py").write_text(' + repr(DELAY_SCRIPT) + ')')
        print('Docker fixture ready: two API processes, PostgreSQL, generated-only shared storage.', flush=True)

    def exercise(self):
        data = png()
        boundary = 'kura-generated-fixture-boundary'
        body = ('--' + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="fixture.png"\r\nContent-Type: image/png\r\n\r\n').encode() + data + ('\r\n--' + boundary + '--\r\n').encode()
        self.request(0, 'POST', '/api/uploads', body, 'multipart/form-data; boundary=' + boundary, 201)
        large=png(1024,7000,0)
        assert 16*1024*1024 < len(large) < 25*1024*1024
        large_body=('--'+boundary+'\r\nContent-Disposition: form-data; name="file"; filename="large-fixture.png"\r\nContent-Type: image/png\r\n\r\n').encode()+large+('\r\n--'+boundary+'--\r\n').encode()
        self.request(0,'POST','/api/uploads',large_body,'multipart/form-data; boundary='+boundary,201)
        print('PASS actual upload of generated PNG larger than 16 MiB.',flush=True)
        # Hold an actual exported snapshot while another process discovers/cancels it.
        self.execute('from pathlib import Path\nPath("/state/slow-enabled").touch()')
        first = self.request(0, 'POST', '/api/recovery/backups', expected=202)
        self.wait_file('snapshot-open')
        listing = self.request(1, 'GET', '/api/recovery/backups')
        assert listing['active_job']['id'] == first['id'], 'another API cannot discover running backup'
        self.request(1, 'POST', '/api/recovery/backups', expected=409)
        self.request(1, 'DELETE', '/api/recovery/backups/' + first['id'])
        self.wait_job(first['id'], 'canceled')
        print('PASS shared creation lock, fresh-client job discovery, cross-process cancellation.', flush=True)
        self.execute('from pathlib import Path\nPath("/state/snapshot-open").unlink(missing_ok=True)')
        graceful=self.request(0,'POST','/api/recovery/backups',expected=202)
        self.wait_file('snapshot-open')
        self.docker('stop','-t','20',self.apis[0])
        self.wait_job(graceful['id'],'canceled')
        self.docker('start',self.apis[0])
        port=self.docker('port',self.apis[0],'8080/tcp').stdout.decode().strip().split(':')[-1]
        self.urls[0]='http://127.0.0.1:'+str(int(port))
        self.wait_api(0)
        print('PASS graceful server shutdown cancels backup and releases its tools/locks.',flush=True)
        self.execute('from pathlib import Path\nPath("/state/snapshot-open").unlink(missing_ok=True)')
        interrupted = self.request(0, 'POST', '/api/recovery/backups', expected=202)
        self.wait_file('snapshot-open')
        self.docker('kill', self.apis[0])
        self.execute('from pathlib import Path\nPath("/state/slow-enabled").unlink(missing_ok=True)', container=self.apis[1])
        self.docker('start', self.apis[0])
        port=self.docker('port',self.apis[0],'8080/tcp').stdout.decode().strip().split(':')[-1]
        self.urls[0]='http://127.0.0.1:'+str(int(port))
        self.wait_api(0)
        self.wait_job(interrupted['id'], 'failed')
        print('PASS process interruption, restart reconciliation, and snapshot lock release.', flush=True)
        completed = []
        # Distinct seconds make service IDs and retention ordering unambiguous.
        for _ in range(3):
            job = self.request(0, 'POST', '/api/recovery/backups', expected=202)
            self.wait_job(job['id'], 'succeeded')
            completed.append(job['id'])
            time.sleep(1.05)
        state = json.loads(self.execute('''import json
from pathlib import Path
print(json.dumps({"primary":sorted(p.name for p in Path("/state/backups").iterdir() if p.name.startswith("backup-")),"replica":sorted(p.name for p in Path("/state/replica").iterdir() if p.name.startswith("backup-"))}))'''))
        assert completed[-1] in state['primary'] and len(state['primary']) == 2, 'retention did not preserve exactly two verified primaries'
        assert set(completed) <= set(state['replica']), 'verified replicas are missing'
        latest = completed[-1]
        self.request(1, 'POST', '/api/recovery/backups/' + latest + '/verify')
        self.execute('''import subprocess
subprocess.run(["python3","/opt/kura/scripts/library-backup.py","verify","--input","/state/replica/''' + latest + '''"],check=True,capture_output=True)
''')
        print('PASS verified second-directory copies and guarded KEEP=2 retention.', flush=True)
        # Restore a replica using the actual offline utility into an empty DB and new media root.
        self.sql('CREATE DATABASE kura_restored;', database='postgres')
        restore = '''import os, subprocess
from urllib.parse import urlsplit,urlunsplit
parts=urlsplit(os.environ["DATABASE_URL"])
env=os.environ.copy();env["DATABASE_URL"]=urlunsplit((parts.scheme,parts.netloc,"/kura_restored",parts.query,""))
for key in list(env):
    if key.startswith(("S3_","AWS_")) or key=="MEDIA_URL_PREFIX":env.pop(key,None)
subprocess.run(["python3","/opt/kura/scripts/library-backup.py","restore","--offline","--input","/state/replica/''' + latest + '''","--media-root","/state/restored-copy"],env=env,check=True,capture_output=True)
'''
        self.execute(restore)
        assert self.sql('SELECT count(*) FROM posts;', 'kura_restored') == '2'
        digest = self.execute('''from pathlib import Path
import hashlib,json
files=list(Path("/state/restored-copy/uploads").rglob("*.png"))
assert len(files)==2
print(json.dumps(sorted(hashlib.sha256(path.read_bytes()).hexdigest() for path in files)))''')
        assert json.loads(digest) == sorted([hashlib.sha256(data).hexdigest(),hashlib.sha256(large).hexdigest()]), 'restored original bytes differ'
        print('PASS replica restore to empty database/new media target and original-byte checksum.', flush=True)
        # A second-copy failure must preserve all existing primary backups.
        count_before = len(state['primary'])
        self.execute('from pathlib import Path\nPath("/state/replica").chmod(0o500)')
        copy_failed = self.request(0, 'POST', '/api/recovery/backups', expected=202)
        warning=self.wait_job(copy_failed['id'], 'succeeded')
        assert warning.get('maintenance_error') == 'replication failed; retention was skipped', 'replica failure was not surfaced'
        preserved = json.loads(self.execute('''import json
from pathlib import Path
print(json.dumps(sorted(p.name for p in Path("/state/backups").iterdir() if p.name.startswith("backup-"))))'''))
        assert set(state['primary']) < set(preserved) and len(preserved) == count_before + 1, 'failed replica allowed primary pruning'
        self.execute('from pathlib import Path\nPath("/state/replica").chmod(0o700)')
        print('PASS replica-storage failure leaves source backups and suppresses retention.', flush=True)
        # Corruption makes the same format verifier fail instead of approving a bad restore.
        self.execute('''from pathlib import Path
import subprocess
root=Path("/state/replica/''' + latest + '''")
next((root/"media/uploads").rglob("*.png")).write_bytes(b"generated-corruption")
result=subprocess.run(["python3","/opt/kura/scripts/library-backup.py","verify","--input",str(root)],capture_output=True)
assert result.returncode != 0
''')
        print('PASS corrupted replica fails verification. Scheduler timing is covered by clock-controlled Go tests, not accelerated production timers.', flush=True)

    def cleanup(self):
        for container in reversed(self.containers):
            self.docker('rm', '-f', '-v', container, check=False)
        self.docker('volume', 'rm', self.volume, check=False)
        self.docker('network', 'rm', self.network, check=False)
        self.docker('image', 'rm', self.image, check=False)
        self.fixture.cleanup()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run', action='store_true', help='create, exercise and remove disposable Docker fixtures')
    args = parser.parse_args()
    if not args.run:
        parser.error('--run is required')
    drill = Drill()
    try:
        drill.setup()
        drill.exercise()
    finally:
        drill.cleanup()


if __name__ == '__main__':
    main()
