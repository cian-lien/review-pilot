import type { Metadata } from 'next';
import type { ReactNode } from 'react';
import Link from 'next/link';
import './globals.css';
import { Providers } from '@/components/providers';
export const metadata: Metadata = {
  title: 'ReviewPilot · 仓库接入',
  description: '公共仓库的 AI 审查与可治理规则记忆。',
};
export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="zh-CN">
      <body>
        <header className="topbar">
          <Link href="/repositories" className="brand">
            ReviewPilot<span>代码审查助手</span>
          </Link>
          <span className="phase">S2 · 证据优先审查</span>
        </header>
        <div className="shell">
          <aside>
            <p className="nav-label">工作空间</p>
            <Link href="/repositories" className="nav-active">
              仓库与任务
            </Link>
            <p className="nav-pending">规则与记忆 · S4</p>
            <p className="aside-note">
              AI 审查默认关闭。
              <br />
              模拟与真实结果明确区分；不执行 PR 代码。
            </p>
          </aside>
          <main>
            <Providers>{children}</Providers>
          </main>
        </div>
      </body>
    </html>
  );
}
