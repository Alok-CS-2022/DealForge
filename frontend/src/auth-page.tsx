import type { ReactNode } from 'react';

export function AuthPage({ children }: { children: ReactNode }) {
  return (
    <main className="auth-page">
      <div className="auth-shell">
        <div className="wordmark">
          <span className="wordmark-symbol" aria-hidden="true">D</span>
          <span>DealForge</span>
        </div>
        <section className="auth-card" aria-labelledby="auth-title">{children}</section>
      </div>
    </main>
  );
}
