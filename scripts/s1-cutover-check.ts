import { spawnSync } from 'node:child_process';
const result=spawnSync('pnpm',['exec','turbo','run','cutover:check','--filter=@reviewpilot/api','--',...process.argv.slice(2)],{stdio:'inherit',env:process.env});
process.exit(result.status??1);
