printf '\033[1m> pnpm install\033[0m\n'
for step in 'Resolving packages' 'Fetching packages' 'Linking dependencies'; do
  printf '\033[36m•\033[0m %s...\n' "$step"
  sleep 0.6
done
printf '\033[32m✔\033[0m 412 packages installed\n'
