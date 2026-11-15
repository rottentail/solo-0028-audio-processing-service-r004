#!/usr/bin/env python3
import io
import json
import os
import socket
import struct
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import wave

action = sys.argv[1]
with tempfile.TemporaryDirectory() as temp:
    binary = os.path.join(temp, 'audioserve')
    subprocess.run(['go', 'build', '-o', binary, './cmd/audioserve'], check=True)
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    env = dict(os.environ, AUDIO_LISTEN='127.0.0.1:%d' % port, AUDIO_DATA_DIR=os.path.join(temp, 'data'))
    process = subprocess.Popen([binary], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    base = 'http://127.0.0.1:%d' % port
    try:
        for _ in range(100):
            if process.poll() is not None:
                raise RuntimeError('server exited')
            try:
                urllib.request.urlopen(base + '/healthz', timeout=1).close()
                break
            except urllib.error.URLError:
                time.sleep(.05)
        else:
            raise RuntimeError('server did not start')
        stream = io.BytesIO()
        with wave.open(stream, 'wb') as wav:
            wav.setnchannels(2)
            wav.setsampwidth(2)
            wav.setframerate(8000)
            wav.writeframes(struct.pack('<hhhhhh', 1000, -1000, 20000, -20000, 5, -5))
        def post(path, data):
            return urllib.request.urlopen(urllib.request.Request(base + path, data=data), timeout=3).read()
        source = stream.getvalue()
        if action == 'inspect':
            result = json.loads(post('/v1/inspect', source))
            assert result['frames'] == 3 and result['channels'] == 2 and result['sample_rate'] == 8000
            bad_path = '/v1/inspect'
            bad_data = source[:-1]
        elif action == 'clip':
            with wave.open(io.BytesIO(post('/v1/clip?start=1&end=2', source)), 'rb') as wav:
                assert wav.getnframes() == 1 and struct.unpack('<hh', wav.readframes(1)) == (20000, -20000)
            bad_path, bad_data = '/v1/clip?start=2&end=4', source
        elif action == 'gain':
            with wave.open(io.BytesIO(post('/v1/gain?factor=2', source)), 'rb') as wav:
                assert struct.unpack('<hhhhhh', wav.readframes(3)) == (2000, -2000, 32767, -32768, 10, -10)
            bad_path, bad_data = '/v1/gain?factor=NaN', source
        else:
            raise ValueError('unknown workflow')
        try:
            post(bad_path, bad_data)
            raise AssertionError('invalid request accepted')
        except urllib.error.HTTPError as exc:
            assert exc.code == 400
        print(action + ': passed')
    finally:
        process.terminate()
        try:
            process.wait(timeout=7)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
