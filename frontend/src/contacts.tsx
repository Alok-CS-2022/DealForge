import { type FormEvent, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';

type Contact = {
  id: string;
  company_id: string;
  name: string;
  email: string;
  phone: string | null;
  company_name: string | null;
};

type ContactList =
  | { status: 'loading' | 'error' | 'unavailable' }
  | { status: 'ready'; contacts: Contact[] };

type ContactsProps = {
  companyID: string;
  userID: string;
  sessionSignal: AbortSignal;
  authPending: boolean;
  onSessionExpired: () => void;
  onSessionChanged: () => void;
};

const validationErrors = new Map([
  ['name must be 1-100 characters', 'Enter a name of 1–100 characters.'],
  ['email must be a valid address of at most 254 bytes', 'Enter a valid email address of at most 254 bytes.'],
  ['phone must be at most 50 characters', 'Enter a phone number of at most 50 characters.'],
  ['company_name must be at most 100 characters', 'Enter a company name of at most 100 characters.'],
]);

function isCompanyContact(value: unknown, companyID: string): value is Contact {
  const contact = value as Partial<Contact> | null;
  return contact?.company_id === companyID && typeof contact.id === 'string' && contact.id !== '' &&
    typeof contact.name === 'string' && typeof contact.email === 'string' &&
    (contact.phone === null || typeof contact.phone === 'string') &&
    (contact.company_name === null || typeof contact.company_name === 'string');
}

export function Contacts({
  companyID, userID, sessionSignal, authPending, onSessionExpired, onSessionChanged,
}: ContactsProps) {
  const url = `/api/v1/companies/${encodeURIComponent(companyID)}/contacts`;
  const scope = useRef<AbortSignal | null>(null);
  const submitting = useRef(false);
  const [list, setList] = useState<ContactList>({ status: 'loading' });
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState('');
  const [saved, setSaved] = useState(false);

  // Invalidate this workspace's callbacks as soon as its keyed component unmounts.
  useLayoutEffect(() => {
    const controller = new AbortController();
    scope.current = AbortSignal.any([sessionSignal, controller.signal]);
    return () => {
      controller.abort();
      scope.current = null;
    };
  }, [sessionSignal]);

  const handleAccessError = useCallback((status: number) => {
    if (status === 401) onSessionExpired();
    else if (status === 412) onSessionChanged();
    else if (status === 404 || status === 403) setList({ status: 'unavailable' });
    else return false;
    return true;
  }, [onSessionChanged, onSessionExpired]);

  const loadContacts = useCallback(async (signal: AbortSignal) => {
    try {
      const response = await fetch(url, {
        credentials: 'same-origin',
        headers: { 'X-DealForge-Expected-User-ID': userID },
        signal,
      });
      if (signal.aborted || handleAccessError(response.status)) return;
      if (response.status !== 200) {
        setList({ status: 'error' });
        return;
      }
      const contacts: unknown = await response.json();
      if (signal.aborted) return;
      if (!Array.isArray(contacts) || !contacts.every((contact) => isCompanyContact(contact, companyID))) {
        setList({ status: 'error' });
        return;
      }
      setList({ status: 'ready', contacts });
    } catch {
      if (!signal.aborted) setList({ status: 'error' });
    }
  }, [companyID, handleAccessError, url, userID]);

  useEffect(() => {
    const signal = scope.current;
    if (signal && !signal.aborted) void loadContacts(signal);
  }, [loadContacts, sessionSignal]);

  function reload() {
    const signal = scope.current;
    if (!signal || signal.aborted) return;
    setList({ status: 'loading' });
    void loadContacts(signal);
  }

  async function addContact(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const signal = scope.current;
    if (!signal || signal.aborted || submitting.current || authPending || list.status !== 'ready') return;
    const form = event.currentTarget;
    const fields = new FormData(form);
    const name = String(fields.get('name')).trim();
    const email = String(fields.get('email')).trim();
    const phone = String(fields.get('phone')).trim();
    const companyName = String(fields.get('company_name')).trim();
    setSaved(false);
    setFormError('');
    if (!name || !email) {
      setFormError('Enter a name and email address.');
      return;
    }

    submitting.current = true;
    setSaving(true);
    try {
      const response = await fetch(url, {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-DealForge-Expected-User-ID': userID },
        body: JSON.stringify({ name, email, phone: phone || undefined, company_name: companyName || undefined }),
        signal,
      });
      if (signal.aborted || handleAccessError(response.status)) return;
      if (response.status === 201) {
        form.reset();
        setSaved(true);
        await loadContacts(signal);
      } else if (response.status === 409) {
        setFormError('A contact with this email already exists in this company.');
      } else if (response.status === 400) {
        const body: { error?: string } | null = await response.json().catch(() => null);
        if (signal.aborted) return;
        setFormError(validationErrors.get(body?.error ?? '') ?? 'Check the contact details and try again.');
      } else {
        setFormError('Could not confirm that the contact was added. Refresh this page before trying again.');
      }
    } catch {
      if (!signal.aborted) {
        setFormError('Could not reach DealForge. Refresh this page before trying again.');
      }
    } finally {
      submitting.current = false;
      if (!signal.aborted) setSaving(false);
    }
  }

  return (
    <section className="contacts" aria-labelledby="contacts-title">
      <h2 id="contacts-title">Contacts</h2>
      {saved && <p role="status">Contact added.</p>}
      {list.status === 'loading' && <p role="status">Loading contacts…</p>}
      {list.status === 'unavailable' && <p role="alert">You no longer have access to this company’s contacts.</p>}
      {list.status === 'error' && (
        <>
          <p className="form-error" role="alert">Could not load contacts. Try again.</p>
          <button type="button" disabled={authPending || saving} onClick={reload}>Reload contacts</button>
        </>
      )}
      {list.status === 'ready' && (
        <>
          {list.contacts.length === 0 ? (
            <p>No contacts yet.</p>
          ) : (
            <ul className="contacts-list" role="list" aria-label="Company contacts">
              {list.contacts.map((contact) => (
                <li key={contact.id}>
                  <h3>{contact.name}</h3>
                  <dl>
                    <div><dt>Email</dt><dd>{contact.email}</dd></div>
                    {contact.phone && <div><dt>Phone</dt><dd>{contact.phone}</dd></div>}
                    {contact.company_name && <div><dt>Company</dt><dd>{contact.company_name}</dd></div>}
                  </dl>
                </li>
              ))}
            </ul>
          )}
          <form method="post" onSubmit={addContact} aria-labelledby="add-contact-title" aria-busy={saving}>
            <h3 id="add-contact-title">Add contact</h3>
            {formError && <p className="form-error" role="alert">{formError}</p>}
            <fieldset disabled={saving || authPending}>
              <div className="field"><label htmlFor="contact-name">Name</label><input id="contact-name" name="name" required /></div>
              <div className="field"><label htmlFor="contact-email">Email</label><input id="contact-email" name="email" inputMode="email" autoCapitalize="none" spellCheck={false} required /></div>
              <div className="field"><label htmlFor="contact-phone">Phone (optional)</label><input id="contact-phone" name="phone" type="tel" /></div>
              <div className="field"><label htmlFor="contact-company">Company name (optional)</label><input id="contact-company" name="company_name" /></div>
              <button type="submit">{saving ? 'Adding contact…' : 'Add contact'}</button>
            </fieldset>
          </form>
        </>
      )}
    </section>
  );
}
