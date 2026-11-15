#!/usr/bin/env python3
import json
import os
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

operation = sys.argv[1]
profiled = operation == 'video-profile'
if profiled:
    operation = 'video-mp4'
query = operation + ('&start=0.2&duration=0.5&width=128&height=64' if profiled else '')
ffmpeg = os.environ.get('FFMPEG_PATH', 'ffmpeg')
with tempfile.TemporaryDirectory() as temp:
    binary = os.path.join(temp, 'server')
    source = os.path.join(temp, 'source.mp4')
    subprocess.run(['go', 'build', '-o', binary, './cmd/audioserve'], check=True)
    subprocess.run([ffmpeg, '-v', 'error', '-f', 'lavfi', '-i', 'testsrc=size=64x64:rate=10', '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=44100', '-t', '1', '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-c:a', 'aac', source], check=True)
    with open(source, 'rb') as f:
        media = f.read()
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    base = 'http://127.0.0.1:%d' % port
    env = dict(os.environ, AUDIO_LISTEN='127.0.0.1:%d' % port, AUDIO_DATA_DIR=os.path.join(temp, 'data'))
    def request(path, data=None, method=None):
        with urllib.request.urlopen(urllib.request.Request(base + path, data=data, method=method), timeout=5) as response:
            return response.read()
    def start():
        process = subprocess.Popen([binary], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for _ in range(100):
            if process.poll() is not None:
                raise RuntimeError('service exited')
            try:
                request('/healthz')
                return process
            except urllib.error.URLError:
                time.sleep(.05)
        raise RuntimeError('service did not start')
    def stop(process):
        process.terminate()
        process.wait(timeout=10)
    def wait_job(job_id):
        for _ in range(200):
            result = json.loads(request('/v1/jobs/' + job_id))
            if result['state'] in ('completed', 'failed', 'cancelled'):
                return result
            time.sleep(.05)
        raise RuntimeError('job did not finish')
    process = start()
    try:
        job = json.loads(request('/v1/jobs?operation=' + query, media))
        result = wait_job(job['id'])
        assert result['state'] == 'completed', result
        output = request('/v1/jobs/' + job['id'] + '/output')
        output_path = os.path.join(temp, 'result')
        with open(output_path, 'wb') as f:
            f.write(output)
        subprocess.run([ffmpeg, '-v', 'error', '-i', output_path, '-f', 'null', '-'], check=True)
        if profiled:
            frames = subprocess.run([ffmpeg, '-v', 'error', '-i', output_path, '-frames:v', '1', '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-'], check=True, stdout=subprocess.PIPE).stdout
            assert len(frames) == 128 * 64 * 3
            audio = subprocess.run([ffmpeg, '-v', 'error', '-i', output_path, '-map', '0:a:0', '-f', 's16le', '-ac', '1', '-ar', '44100', '-'], check=True, stdout=subprocess.PIPE).stdout
            assert 0.4 < len(audio) / (2 * 44100) < 0.6
            assert job['settings']['start_seconds'] == 0.2 and job['settings']['duration_seconds'] == 0.5
            try:
                request('/v1/jobs?operation=video-mp4&width=127&height=64', media)
                raise AssertionError('invalid dimensions accepted')
            except urllib.error.HTTPError as exc:
                assert exc.code == 400
            empty = json.loads(request('/v1/jobs?operation=video-mp4&start=5&duration=1', media))
            assert wait_job(empty['id'])['state'] == 'failed'
        bad = json.loads(request('/v1/jobs?operation=' + query, b'not media'))
        assert wait_job(bad['id'])['state'] == 'failed'
        retried = json.loads(request('/v1/jobs/' + bad['id'] + '/retry', b''))
        assert retried['attempt'] == 2 and wait_job(bad['id'])['state'] == 'failed'
        assert retried['settings'] == bad['settings']
        stop(process)
        process = start()
        restored = json.loads(request('/v1/jobs/' + job['id']))
        assert restored['state'] == 'completed' and restored['settings'] == job['settings']
        assert request('/v1/jobs/' + job['id'] + '/output') == output
        print(operation + ': passed')
    finally:
        if process.poll() is None:
            stop(process)
