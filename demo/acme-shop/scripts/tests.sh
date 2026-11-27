printf '\033[1m> pnpm test\033[0m\n'
sleep 0.5
printf ' \033[32m✓\033[0m cart.test.ts (8 tests)\n'
sleep 0.5
printf ' \033[32m✓\033[0m products.test.ts (14 tests)\n'
sleep 0.5
printf ' \033[31m✗\033[0m checkout.test.ts > applies the discount code\n'
printf '   \033[31mexpected 90, received 100\033[0m\n\n'
printf ' Tests  \033[31m1 failed\033[0m | \033[32m22 passed\033[0m\n'
exit 1
