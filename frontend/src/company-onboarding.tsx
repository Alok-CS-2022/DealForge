import { type FormEvent, useRef, useState } from 'react';

type CreatedWorkspace = {
  company: { id: string; name: string };
  team: { id: string; company_id: string; name: string };
};

type CompanyOnboardingProps = {
  displayName: string;
  loggingOut: boolean;
  logoutError: string;
  onLogout: () => void;
  onSessionExpired: () => void;
};

const unconfirmedCreation = 'Could not confirm whether the company was created. Retrying could create another company.';

function focusHeading(heading: HTMLHeadingElement | null) {
  heading?.focus();
}

export function CompanyOnboarding({
  displayName, loggingOut, logoutError, onLogout, onSessionExpired,
}: CompanyOnboardingProps) {
  const inFlight = useRef(false);
  const [creating, setCreating] = useState(false);
  const [workspace, setWorkspace] = useState<CreatedWorkspace | null>(null);
  const [nameError, setNameError] = useState('');
  const [error, setError] = useState('');

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || loggingOut) return;

    const input = event.currentTarget.elements.namedItem('name') as HTMLInputElement;
    const name = input.value.trim();
    setNameError('');
    setError('');
    if (!name || Array.from(name).length > 100 || name.includes('\u0000')) {
      setNameError('Enter a company name of 1–100 characters.');
      input.focus();
      return;
    }

    inFlight.current = true;
    setCreating(true);
    try {
      const response = await fetch('/api/v1/companies', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      });
      if (response.status === 401) {
        onSessionExpired();
      } else if (response.status === 400) {
        setNameError('Check that the company name is 1–100 characters.');
      } else if (response.status === 201) {
        const data: Partial<CreatedWorkspace> | null = await response.json();
        if (typeof data?.company?.id === 'string' && typeof data.company.name === 'string' &&
            typeof data.team?.id === 'string' && data.team.company_id === data.company.id &&
            typeof data.team.name === 'string') {
          setWorkspace({ company: data.company, team: data.team });
        } else {
          setError(unconfirmedCreation);
        }
      } else {
        setError('The server did not confirm company creation. Retrying could create another company.');
      }
    } catch {
      setError(unconfirmedCreation);
    } finally {
      inFlight.current = false;
      setCreating(false);
    }
  }

  return (
    <>
      {workspace ? (
        <div className="confirmation">
          <span className="confirmation-mark" aria-hidden="true">✓</span>
          <h1 id="auth-title" tabIndex={-1} ref={focusHeading}>Workspace created</h1>
          <dl className="workspace-details">
            <div><dt>Company</dt><dd>{workspace.company.name}</dd></div>
            <div><dt>Initial team</dt><dd>{workspace.team.name}</dd></div>
          </dl>
        </div>
      ) : (
        <>
          <h1 id="auth-title" tabIndex={-1} ref={focusHeading}>Create your company</h1>
          <p className="intro">Your new workspace starts with a General team.</p>
          <form method="post" noValidate onSubmit={submit} aria-busy={creating}>
            {error && <p className="form-error" role="alert">{error}</p>}
            <fieldset disabled={creating || loggingOut} aria-label="Company details">
              <div className="field">
                <label htmlFor="company-name">Company name</label>
                <input id="company-name" name="name" required
                  aria-invalid={Boolean(nameError)}
                  aria-describedby={nameError ? 'company-name-error' : undefined} />
                {nameError && <p className="field-error" id="company-name-error" role="alert">{nameError}</p>}
              </div>
              <button type="submit">{creating ? 'Creating company…' : 'Create company'}</button>
            </fieldset>
          </form>
        </>
      )}
      {logoutError && <p className="form-error company-logout-error" role="alert">{logoutError}</p>}
      <div className="company-account">
        <span>Signed in as {displayName}</span>
        <button type="button" disabled={creating || loggingOut} onClick={() => {
          if (!inFlight.current) onLogout();
        }}>
          {loggingOut ? 'Logging out…' : 'Logout'}
        </button>
      </div>
    </>
  );
}
