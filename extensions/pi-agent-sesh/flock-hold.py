#!/usr/bin/env python3
"""Hold an advisory flock on lock_path until stdin closes. Used by registry-lock.ts."""

import fcntl
import os
import signal
import sys
import time

lock_path = sys.argv[1]
timeout_s = float(sys.argv[2])
poll_s = float(sys.argv[3])

fd = os.open(lock_path, os.O_CREAT | os.O_RDWR)
held = False


def release(_signum=None, _frame=None) -> None:
    global held
    if held:
        fcntl.flock(fd, fcntl.LOCK_UN)
        held = False
    os.close(fd)
    try:
        os.remove(lock_path)
    except OSError:
        pass
    raise SystemExit(0)


signal.signal(signal.SIGTERM, release)
signal.signal(signal.SIGHUP, release)
signal.signal(signal.SIGINT, release)

deadline = time.time() + timeout_s
while time.time() < deadline:
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        held = True
        break
    except BlockingIOError:
        time.sleep(poll_s)
else:
    os.close(fd)
    raise SystemExit(1)

sys.stdout.write("ready\n")
sys.stdout.flush()
sys.stdin.buffer.read()
release()
