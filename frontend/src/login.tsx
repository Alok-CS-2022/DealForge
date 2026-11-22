import { type FormEvent, useEffect, useRef, useState } from 'react';
import { AuthPage } from './auth-page';

type Session =
  | { status: 'checking' | 'unavailable' | 'signed-out' }
  | { status: 'signed-in'; displayName: string };

function focusHeading(heading: HTMLHeadingElement | null) {
  heading?.focus();
}

async function readSession(signal?: AbortSignal): Promise<Session> {
  try {
    const response = await fetch('/api/v1/auth/me', { credentials: 'same-origin', signal });
    if (response.status === 401) return { status: 'signed-out' };
    if (response.status === 200) {
      const profile: { display_name?: unknown } | null = await response.json();
      if (typeof profile?.display_name === 'string') {
        return { status: 'signed-in', displayName: profile.display_name };
      }
    }
  } catch {
    return { status: 'unavailable' };
  }
  return { status: 'unavailable' };
}

export function Login() {
  const inFlight = useRef(false);
  const [session, setSession] = useState<Session>({ status: 'checking' });
  const [pending, setPending] = useState(false);
  const [signedOut, setSignedOut] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    const controller = new AbortController();
    void readSession(controller.signal).then((result) => {
      if (!controller.signal.aborted) setSession(result);
    });
    return () => controller.abort();
  }, []);

  async function retrySession() {
    setSession({ status: 'checking' });
    setSession(await readSession());
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current) return;

    const form = event.currentTarget;
    const username = (form.elements.namedItem('username') as HTMLInputElement).value;
    const passwordInput = form.elements.namedItem('password') as HTMLInputElement;
    const body = JSON.stringify({ username, password: passwordInput.value });
    passwordInput.value = '';
    inFlight.current = true;
    setPending(true);
    setSignedOut(false);
    setError('');
    try {
      const response = await fetch('/api/v1/auth/login', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body,
      });
      if (response.status === 204) {
        const result = await readSession();
        if (result.status === 'signed-out') {
          setError('Could not confirm your session. Make sure cookies are enabled and try again.');
        }
        setSession(result);
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
      setError('Could not reach DealForge. Check your connection and try again.');
    } finally {
      inFlight.current = false;
      setPending(false);
    }
  }

  async function logout() {
    if (inFlight.current) return;
    inFlight.current = true;
    setPending(true);
    setError('');
    try {
      const response = await fetch('/api/v1/auth/logout', { method: 'POST', credentials: 'same-origin' });
      if (response.status === 204) {
        setSignedOut(true);
        setSession({ status: 'signed-out' });
      } else {
        setError('The server could not confirm logout. Try again in a moment.');
      }
    } catch {
      setError('Could not confirm logout. Check your connection and try again.');
    } finally {
      inFlight.current = false;
      setPending(false);
    }
  }

  return (
    <AuthPage>
      {session.status === 'signed-in' ? (
        <>
          <h1 key="welcome" id="auth-title" tabIndex={-1} ref={focusHeading}>Welcome, {session.displayName}</h1>
          <div className="auth-actions">
            {error && <p className="form-error" role="alert">{error}</p>}
            <button type="button" disabled={pending} onClick={logout}>
              {pending ? 'Logging out…' : 'Logout'}
            </button>
          </div>
        </>
      ) : (
        <>
          <h1 key="login" id="auth-title" tabIndex={signedOut ? -1 : undefined}
            ref={signedOut ? focusHeading : undefined}>Log in</h1>
          {session.status === 'checking' && <p className="intro" role="status">Checking your session…</p>}
          {session.status === 'unavailable' && (
            <div className="auth-actions">
              <p className="form-error" role="alert">Could not check your session. Check your connection and try again.</p>
              <button type="button" onClick={retrySession}>Retry session check</button>
            </div>
          )}
          {session.status === 'signed-out' && (
            <>
              {signedOut && <p className="intro" role="status">You are logged out.</p>}
              <form method="post" onSubmit={submit} aria-busy={pending}>
                {error && <p className="form-error" role="alert">{error}</p>}
                <fieldset disabled={pending} aria-label="Login details">
                  <div className="field">
                    <label htmlFor="username">Username</label>
                    <input id="username" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} required />
                  </div>
                  <div className="field">
                    <label htmlFor="password">Password</label>
                    <input id="password" name="password" type="password" autoComplete="current-password" required />
                  </div>
                  <button type="submit">{pending ? 'Logging in…' : 'Log in'}</button>
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
