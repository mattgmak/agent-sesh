#!/usr/bin/env python3
"""Hold an advisory flock on lock_path until stdin closes. Used by registry-lock.ts."""

import fcntl
import os
import sys
import time

lock_path = sys.argv[1]
timeout_s = float(sys.argv[2])
poll_s = float(sys.argv[3])

fd = os.open(lock_path, os.O_CREAT | os.O_RDWR)
deadline = time.time() + timeout_s
while time.time() < deadline:
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        break
    except BlockingIOError:
        time.sleep(poll_s)
else:
    raise SystemExit(1)

sys.stdout.write("ready\n")
sys.stdout.flush()
sys.stdin.buffer.read(1)
fcntl.flock(fd, fcntl.LOCK_UN)
