printf '\033[36mworker\033[0m ready, waiting for jobs\n'
n=1040
while :; do
  sleep 1.3
  n=$((n + 1))
  case $((n % 3)) in
    0) job=send-receipt ;;
    1) job=resize-image ;;
    *) job=sync-stock ;;
  esac
  printf '\033[90m%s\033[0m job #%s \033[35m%-13s\033[0m \033[32mdone\033[0m\n' "$(date +%H:%M:%S)" "$n" "$job"
done
