#!/usr/bin/env python3
import json
import os
import socket
import subprocess
import tempfile
import time
import urllib.request

with tempfile.TemporaryDirectory() as temp:
    binary = os.path.join(temp, 'server')
    wrapper = os.path.join(temp, 'slow-ffmpeg')
    ffmpeg = os.environ.get('FFMPEG_PATH', 'ffmpeg')
    with open(wrapper, 'w') as f:
        f.write('#!/usr/bin/env python3\nimport os, time\ntime.sleep(2)\nos.execvp(os.environ["REAL_FFMPEG"], [os.environ["REAL_FFMPEG"]] + __import__("sys").argv[1:])\n')
    os.chmod(wrapper, 0o700)
    subprocess.run(['go', 'build', '-o', binary, './cmd/audioserve'], check=True)
    source = os.path.join(temp, 'source.wav')
    subprocess.run([ffmpeg, '-v', 'error', '-f', 'lavfi', '-i', 'sine=frequency=440', '-t', '1', source], check=True)
    with open(source, 'rb') as f:
        media = f.read()
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        port = s.getsockname()[1]
    base = 'http://127.0.0.1:%d' % port
    env = dict(os.environ, AUDIO_LISTEN='127.0.0.1:%d' % port, AUDIO_DATA_DIR=os.path.join(temp, 'data'), FFMPEG_PATH=wrapper, REAL_FFMPEG=ffmpeg)
    def request(path, data=None, method=None):
        with urllib.request.urlopen(urllib.request.Request(base + path, data=data, method=method), timeout=5) as response:
            return response.read()
    def start():
        p = subprocess.Popen([binary], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for _ in range(100):
            if p.poll() is not None:
                raise RuntimeError('server exited')
            try:
                request('/healthz')
                return p
            except OSError:
                time.sleep(.05)
        raise RuntimeError('server did not start')
    def wait_for(job_id, state):
        for _ in range(100):
            job = json.loads(request('/v1/jobs/' + job_id))
            if job['state'] == state:
                return job
            time.sleep(.05)
        raise RuntimeError('state %s not reached: %r' % (state, job))
    p = start()
    try:
        job = json.loads(request('/v1/jobs?operation=audio-m4a', media))
        wait_for(job['id'], 'running')
        request('/v1/jobs/' + job['id'], method='DELETE')
        wait_for(job['id'], 'cancelled')
        other = json.loads(request('/v1/jobs?operation=audio-m4a', media))
        wait_for(other['id'], 'running')
        p.kill()
        p.wait(timeout=5)
        p = start()
        assert wait_for(other['id'], 'failed')['error'] == 'interrupted by service restart'
        assert wait_for(job['id'], 'cancelled')['state'] == 'cancelled'
        print('job-control: passed')
    finally:
        if p.poll() is None:
            p.terminate()
            p.wait(timeout=10)
