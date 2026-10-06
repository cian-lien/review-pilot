import { spawnSync } from 'node:child_process';
const steps=[
 ['contracts:check','--filter=@reviewpilot/contracts'],
 ['typecheck','lint','build','--filter=@reviewpilot/api','--filter=@reviewpilot/web','--filter=@reviewpilot/tooling','--filter=@reviewpilot/observability','--filter=@reviewpilot/worker-runtime'],
 ['test:unit','--filter=@reviewpilot/api','--filter=@reviewpilot/web','--filter=@reviewpilot/worker-runtime','--filter=@reviewpilot/observability','--filter=@reviewpilot/contracts'],
 ['infra:check','--filter=@reviewpilot/tooling'],
 ['test:migration','--filter=@reviewpilot/tooling','--','--case','all'],
];
for(const args of steps){
 const result=spawnSync('pnpm',['exec','turbo','run',...args],{stdio:'inherit',env:process.env});
 if(result.status!==0) process.exit(result.status??1);
}
console.log('自动迁移验证通过。手工仓库页清单见 docs/runbooks/s1-go-migration.md；真实 GitHub、TLS 和生产部署仍待外部验收，不算本次自动通过项。');
