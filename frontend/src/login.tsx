import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react';
import { AuthPage } from './auth-page';
import { CompanyWorkspace } from './company-workspace';

type Session =
  | { status: 'checking' | 'unavailable' | 'signed-out' }
  | { status: 'signed-in'; userID: string; displayName: string; signal: AbortSignal };

const authChannelName = 'dealforge.auth';
const sessionChanged = 'session-changed';
type AuthAction = 'login' | 'logout';

function focusHeading(heading: HTMLHeadingElement | null) {
  heading?.focus();
}

async function readSession(signal: AbortSignal): Promise<Session> {
  try {
    const response = await fetch('/api/v1/auth/me', { credentials: 'same-origin', signal });
    if (response.status === 401) return { status: 'signed-out' };
    if (response.status === 200) {
      const profile: { id?: unknown; display_name?: unknown } | null = await response.json();
      if (typeof profile?.id === 'string' && profile.id && typeof profile.display_name === 'string') {
        return { status: 'signed-in', userID: profile.id, displayName: profile.display_name, signal };
      }
    }
  } catch {
    return { status: 'unavailable' };
  }
  return { status: 'unavailable' };
}

export function Login() {
  const inFlight = useRef(false);
  const authScope = useRef<AbortController | null>(null);
  const authChannel = useRef<BroadcastChannel | null>(null);
  const [session, setSession] = useState<Session>({ status: 'checking' });
  const [pending, setPending] = useState<AuthAction | null>(null);
  const [signOutReason, setSignOutReason] = useState<'logout' | 'expired' | null>(null);
  const [error, setError] = useState('');

  const renewAuthScope = useCallback(() => {
    // Invalidate old requests synchronously, before React removes the previous workspace.
    authScope.current?.abort();
    const controller = new AbortController();
    authScope.current = controller;
    return controller.signal;
  }, []);

  const retrySession = useCallback(async (action?: AuthAction) => {
    const signal = renewAuthScope();
    setSession({ status: 'checking' });
    setError('');
    setSignOutReason(null);
    const result = await readSession(signal);
    if (signal.aborted) return;
    if (result.status === 'signed-out') {
      if (action === 'logout') setSignOutReason('logout');
      if (action === 'login') setError('Could not confirm your session. Make sure cookies are enabled and try again.');
    }
    setSession(result);
  }, [renewAuthScope]);

  useEffect(() => {
    const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(authChannelName);
    authChannel.current = channel;
    if (channel) {
      channel.onmessage = (event) => {
        if (event.data === sessionChanged) void retrySession();
      };
    }
    function restoreCachedPage(event: PageTransitionEvent) {
      if (event.persisted) void retrySession();
    }
    window.addEventListener('pageshow', restoreCachedPage);
    void retrySession();
    return () => {
      window.removeEventListener('pageshow', restoreCachedPage);
      channel?.close();
      authChannel.current = null;
      authScope.current?.abort();
      authScope.current = null;
    };
  }, [retrySession]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const signal = authScope.current?.signal;
    if (inFlight.current || !signal || signal.aborted) return;

    const form = event.currentTarget;
    const username = (form.elements.namedItem('username') as HTMLInputElement).value;
    const passwordInput = form.elements.namedItem('password') as HTMLInputElement;
    const body = JSON.stringify({ username, password: passwordInput.value });
    passwordInput.value = '';
    inFlight.current = true;
    setPending('login');
    setSignOutReason(null);
    setError('');
    try {
      const response = await fetch('/api/v1/auth/login', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body,
      });
      if (!authScope.current) return;
      // A successful POST can change the shared cookie even after its UI scope was invalidated.
      if (response.status === 204) {
        authChannel.current?.postMessage(sessionChanged);
        await retrySession('login');
      } else if (signal.aborted) {
        return;
      } else if (response.status === 401) {
        setError('Invalid username or password.');
      } else if (response.status === 400) {
        setError('Check your username and password and try again.');
      } else if (response.status === 503) {
        setError('Sign-in is busy. Wait a moment, then try again.');
      } else {
        setError('The server could not log you in. Try again in a moment.');
      }
    } catch {
      if (!signal.aborted) setError('Could not reach DealForge. Check your connection and try again.');
    } finally {
      inFlight.current = false;
      if (authScope.current) setPending(null);
    }
  }

  async function logout() {
    const signal = authScope.current?.signal;
    if (inFlight.current || !signal || signal.aborted) return;
    inFlight.current = true;
    setPending('logout');
    setError('');
    try {
      const response = await fetch('/api/v1/auth/logout', { method: 'POST', credentials: 'same-origin' });
      if (!authScope.current) return;
      if (response.status === 204) {
        authChannel.current?.postMessage(sessionChanged);
        await retrySession('logout');
      } else if (!signal.aborted) {
        setError('The server could not confirm logout. Try again in a moment.');
      }
    } catch {
      if (!signal.aborted) setError('Could not confirm logout. Check your connection and try again.');
    } finally {
      inFlight.current = false;
      if (authScope.current) setPending(null);
    }
  }

  const sessionExpired = useCallback(() => {
    renewAuthScope();
    setError('');
    setSignOutReason('expired');
    setSession({ status: 'signed-out' });
  }, [renewAuthScope]);

  return (
    <AuthPage>
      {session.status === 'signed-in' ? (
        <CompanyWorkspace key={session.userID} userID={session.userID} displayName={session.displayName}
          sessionSignal={session.signal} authPending={pending} logoutError={error}
          onLogout={logout} onSessionExpired={sessionExpired} onSessionChanged={retrySession} />
      ) : (
        <>
          <h1 id="auth-title" tabIndex={signOutReason ? -1 : undefined}
            ref={signOutReason ? focusHeading : undefined}>Log in</h1>
          {session.status === 'checking' && <p className="intro" role="status">Checking your session…</p>}
          {session.status === 'unavailable' && (
            <div className="auth-actions">
              <p className="form-error" role="alert">Could not check your session. Check your connection and try again.</p>
              <button type="button" onClick={() => void retrySession()}>Retry session check</button>
            </div>
          )}
          {session.status === 'signed-out' && (
            <>
              {signOutReason === 'logout' && <p className="intro" role="status">You are logged out.</p>}
              {signOutReason === 'expired' && <p className="form-error" role="alert">Your session expired. Log in again to continue.</p>}
              <form method="post" onSubmit={submit} aria-busy={pending !== null}>
                {error && <p className="form-error" role="alert">{error}</p>}
                <fieldset disabled={pending !== null} aria-label="Login details">
                  <div className="field">
                    <label htmlFor="username">Username</label>
                    <input id="username" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} required />
                  </div>
                  <div className="field">
                    <label htmlFor="password">Password</label>
                    <input id="password" name="password" type="password" autoComplete="current-password" required />
                  </div>
                  <button type="submit">{pending === 'login' ? 'Logging in…' : 'Log in'}</button>
                </fieldset>
              </form>
              <p className="auth-link">Need an account? <a href="/">Create an account</a></p>
            </>
          )}
        </>
      )}
    </AuthPage>
  );
}
