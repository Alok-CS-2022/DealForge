import { type FormEvent, useRef, useState } from 'react';

type CreatedWorkspace = {
  company: { id: string; name: string };
  team: { id: string; company_id: string; name: string };
};

type CompanyOnboardingProps = {
  userID: string;
  sessionSignal: AbortSignal;
  authPending: boolean;
  onCreatingChange: (creating: boolean) => void;
  onCreated: (companyID: string, teamName: string) => void;
  onSessionExpired: () => void;
  onSessionChanged: () => void;
};

const unconfirmedCreation = 'Could not confirm whether the company was created. Retrying could create another company.';

function focusHeading(heading: HTMLHeadingElement | null) {
  heading?.focus();
}

export function CompanyOnboarding({
  userID, sessionSignal, authPending, onCreatingChange, onCreated, onSessionExpired, onSessionChanged,
}: CompanyOnboardingProps) {
  const inFlight = useRef(false);
  const [creating, setCreating] = useState(false);
  const [nameError, setNameError] = useState('');
  const [error, setError] = useState('');

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (sessionSignal.aborted || inFlight.current || authPending) return;

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
    onCreatingChange(true);
    try {
      const response = await fetch('/api/v1/companies', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-DealForge-Expected-User-ID': userID },
        body: JSON.stringify({ name }),
        signal: sessionSignal,
      });
      if (sessionSignal.aborted) return;
      if (response.status === 401) {
        onSessionExpired();
      } else if (response.status === 412) {
        onSessionChanged();
      } else if (response.status === 400) {
        setNameError('Check that the company name is 1–100 characters.');
      } else if (response.status === 201) {
        const data: Partial<CreatedWorkspace> | null = await response.json();
        if (sessionSignal.aborted) return;
        if (typeof data?.company?.id === 'string' && typeof data.company.name === 'string' &&
            typeof data.team?.id === 'string' && data.team.company_id === data.company.id &&
            typeof data.team.name === 'string') {
          onCreated(data.company.id, data.team.name);
        } else {
          setError(unconfirmedCreation);
        }
      } else {
        setError('The server did not confirm company creation. Retrying could create another company.');
      }
    } catch {
      if (!sessionSignal.aborted) setError(unconfirmedCreation);
    } finally {
      if (!sessionSignal.aborted) {
        inFlight.current = false;
        setCreating(false);
        onCreatingChange(false);
      }
    }
  }

  return (
    <>
      <h1 id="auth-title" tabIndex={-1} ref={focusHeading}>Create your company</h1>
      <p className="intro">Your new workspace starts with a General team.</p>
      <form method="post" noValidate onSubmit={submit} aria-busy={creating}>
        {error && <p className="form-error" role="alert">{error}</p>}
        <fieldset disabled={creating || authPending} aria-label="Company details">
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
  );
}
