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
          <span className="phase">S1 · GitHub 接入</span>
        </header>
        <div className="shell">
          <aside>
            <p className="nav-label">工作空间</p>
            <Link href="/repositories" className="nav-active">
              仓库与任务
            </Link>
            <p className="nav-pending">审查详情 · S2</p>
            <p className="nav-pending">规则与记忆 · S4</p>
            <p className="aside-note">
              目前仅验证 GitHub 接入。
              <br />
              不会调用模型或执行 PR 代码。
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
