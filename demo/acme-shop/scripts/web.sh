printf '\n  \033[1;35mVITE\033[0m \033[35mv6.0.3\033[0m  ready in \033[1m312 ms\033[0m\n\n'
printf '  \033[32m➜\033[0m  Local:   \033[36mhttp://localhost:%s/\033[0m\n\n' "$PORT"
while :; do
  for file in Cart.tsx ProductList.tsx checkout.css App.tsx; do
    sleep 2.5
    printf '\033[90m%s\033[0m \033[36m[vite]\033[0m \033[32mhmr update\033[0m /src/%s\n' "$(date +%H:%M:%S)" "$file"
  done
done
