import itertools
import os
import socket
import time

port = int(os.environ.get("PORT", "4000"))
server = socket.socket()
server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
try:
    server.bind(("127.0.0.1", port))
    server.listen()
except OSError as error:
    print(f"\033[33mWARN\033[0m  cannot listen on {port}: {error}")

REQUESTS = [
    ("GET", "/products", 200, 12),
    ("GET", "/products/42", 200, 8),
    ("POST", "/cart", 201, 23),
    ("GET", "/cart", 200, 6),
    ("POST", "/checkout", 201, 87),
    ("GET", "/products/999", 404, 4),
    ("GET", "/orders", 200, 31),
    ("POST", "/payments", 502, 1204),
    ("GET", "/health", 200, 1),
]
COLORS = {2: 32, 4: 33, 5: 31}


def log(level, color, message):
    print(f"\033[90m{time.strftime('%H:%M:%S')}\033[0m \033[{color}m{level:<5}\033[0m {message}")


log("INFO", 36, f"API listening on http://localhost:{port}")
log("INFO", 36, "connected to postgres")
for method, path, status, ms in itertools.cycle(REQUESTS):
    color = COLORS[status // 100]
    log("HTTP", 35, f"{method:<4} {path:<16} \033[{color}m{status}\033[0m  {ms}ms")
    if status >= 500:
        log("ERROR", 31, "payment provider timeout, retrying in 2s")
    time.sleep(0.45)
