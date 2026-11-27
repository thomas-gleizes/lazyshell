log() { printf '\033[90m%s\033[0m \033[34mLOG\033[0m  %s\n' "$(date +%H:%M:%S)" "$1"; }
log 'starting PostgreSQL 17.2'
sleep 1
log 'listening on 127.0.0.1, port 5432'
sleep 1
log 'database system is ready to accept connections'
n=1
while :; do
  sleep 4
  log "checkpoint $n complete: wrote $((n * 7 % 40 + 3)) buffers"
  n=$((n + 1))
done
