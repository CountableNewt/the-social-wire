#!/usr/bin/env python3
"""Emit a complete buffered test report through a potentially nonblocking CI pipe."""

import os
import shutil
import sys

# Docker/Bun can leave stdout nonblocking. Wait for pipe capacity instead of
# accepting a truncated report when the runner's log consumer applies pressure.
os.set_blocking(sys.stdout.fileno(), True)
with open(sys.argv[1], "rb") as source:
    shutil.copyfileobj(source, sys.stdout.buffer)
sys.stdout.buffer.flush()
