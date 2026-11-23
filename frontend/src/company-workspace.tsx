import { useEffect, useState } from 'react';
import { CompanyOnboarding } from './company-onboarding';
import { Contacts } from './contacts';

type Company = { id: string; name: string; role: 'admin' | 'member' };
type CreatedCompany = { id: string; teamName: string };
type CompanyList =
  | { status: 'loading' | 'error' }
  | { status: 'ready'; companies: Company[]; selection: Company | null };

type CompanyWorkspaceProps = {
  userID: string;
  displayName: string;
  sessionSignal: AbortSignal;
  authPending: 'login' | 'logout' | null;
  logoutError: string;
  onLogout: () => void;
  onSessionExpired: () => void;
  onSessionChanged: () => void;
};

function readSelection(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function saveSelection(key: string, id: string | null) {
  try {
    if (id === null) localStorage.removeItem(key);
    else localStorage.setItem(key, id);
  } catch {
    // Company selection remains usable when browser storage is unavailable.
  }
}

function isCompany(value: unknown): value is Company {
  const company = value as Partial<Company> | null;
  return typeof company?.id === 'string' && company.id !== '' &&
    typeof company.name === 'string' && (company.role === 'admin' || company.role === 'member');
}

function focusHeading(heading: HTMLHeadingElement | null) {
  heading?.focus();
}

export function CompanyWorkspace({
  userID, displayName, sessionSignal, authPending, logoutError, onLogout, onSessionExpired, onSessionChanged,
}: CompanyWorkspaceProps) {
  const preferenceKey = `dealforge.selected-company.${userID}`;
  const [list, setList] = useState<CompanyList>({ status: 'loading' });
  const [request, setRequest] = useState<{ created?: CreatedCompany }>({});
  const [creating, setCreating] = useState(false);
  const created = request.created;

  useEffect(() => {
    const controller = new AbortController();
    const signal = AbortSignal.any([sessionSignal, controller.signal]);
    async function loadCompanies() {
      try {
        const response = await fetch('/api/v1/companies', {
          credentials: 'same-origin',
          headers: { 'X-DealForge-Expected-User-ID': userID },
          signal,
        });
        if (signal.aborted) return;
        if (response.status === 401) {
          onSessionExpired();
          return;
        }
        if (response.status === 412) {
          onSessionChanged();
          return;
        }
        if (response.status !== 200) {
          setList({ status: 'error' });
          return;
        }
        const companies: unknown = await response.json();
        if (signal.aborted) return;
        if (!Array.isArray(companies) || !companies.every(isCompany)) {
          setList({ status: 'error' });
          return;
        }
        const preferredID = request.created?.id ?? readSelection(preferenceKey);
        const preferred = companies.find((company) => company.id === preferredID);
        if (request.created && !preferred) {
          setList({ status: 'error' });
          return;
        }
        const selection = preferred ?? companies[0] ?? null;
        saveSelection(preferenceKey, selection?.id ?? null);
        setList({ status: 'ready', companies, selection });
      } catch {
        if (!signal.aborted) setList({ status: 'error' });
      }
    }
    void loadCompanies();
    return () => controller.abort();
  }, [onSessionChanged, onSessionExpired, preferenceKey, request, sessionSignal, userID]);

  function reload(created?: CreatedCompany) {
    setList({ status: 'loading' });
    setRequest({ created });
  }

  function selectCompany(id: string) {
    if (sessionSignal.aborted || list.status !== 'ready') return;
    const selection = list.companies.find((company) => company.id === id);
    if (!selection) return;
    saveSelection(preferenceKey, selection.id);
    setList({ ...list, selection });
  }

  const selection = list.status === 'ready' ? list.selection : null;
  return (
    <>
      {list.status === 'loading' && (
        <>
          <h1 id="auth-title">Companies</h1>
          <p className="intro" role="status">Loading your companies…</p>
        </>
      )}
      {list.status === 'error' && (
        <div className="auth-actions">
          <h1 id="auth-title">Companies</h1>
          <p className="form-error" role="alert">{created
            ? 'Your company was created, but its workspace could not be loaded.'
            : 'Could not load your companies. Check your connection and try again.'}</p>
          <button type="button" disabled={authPending !== null} onClick={() => reload(created)}>Reload companies</button>
        </div>
      )}
      {list.status === 'ready' && (selection ? (
        <>
          {list.companies.length > 1 && (
            <div className="field company-selector">
              <label htmlFor="active-company">Company</label>
              <select id="active-company" value={selection.id} disabled={authPending !== null}
                onChange={(event) => selectCompany(event.currentTarget.value)}>
                {list.companies.map((company) => (
                  <option key={company.id} value={company.id}>{company.name}</option>
                ))}
              </select>
            </div>
          )}
          <h1 id="auth-title" tabIndex={-1} ref={focusHeading}>{selection.name}</h1>
          {created?.id === selection.id && <p className="intro" role="status">Workspace created.</p>}
          <dl className="workspace-details">
            <div><dt>Company role</dt><dd>{selection.role === 'admin' ? 'Admin' : 'Member'}</dd></div>
            {created?.id === selection.id && <div><dt>Initial team</dt><dd>{created.teamName}</dd></div>}
          </dl>
          <Contacts key={selection.id} companyID={selection.id} userID={userID}
            sessionSignal={sessionSignal} authPending={authPending !== null}
            onSessionExpired={onSessionExpired} onSessionChanged={onSessionChanged} />
        </>
      ) : (
        <CompanyOnboarding userID={userID} sessionSignal={sessionSignal} authPending={authPending !== null}
          onCreatingChange={setCreating} onCreated={(id, teamName) => reload({ id, teamName })}
          onSessionExpired={onSessionExpired} onSessionChanged={onSessionChanged} />
      ))}
      {logoutError && <p className="form-error company-logout-error" role="alert">{logoutError}</p>}
      <div className="company-account">
        <span>Signed in as {displayName}</span>
        <button type="button" disabled={creating || authPending !== null} onClick={onLogout}>
          {authPending === 'logout' ? 'Logging out…' : 'Logout'}
        </button>
      </div>
    </>
  );
}
