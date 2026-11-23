import { useCallback, useEffect, useRef, useState } from 'react';
import { DealForm } from './deal-form';
import {
  isDeal, isDealContact, stageLabel, useDealRequests,
  type Deal, type DealContact, type DealInput, type DealWorkspaceProps, type LoadResult,
} from './deal-workspace-api';

type TeamDealsProps = DealWorkspaceProps & { teamID: string; onReloadTeams: () => void };

export function TeamDeals(props: TeamDealsProps) {
  const { companyID, teamID, sessionSignal, authPending, onReloadTeams } = props;
  const { scope, request } = useDealRequests(props);
  const base = `/api/v1/companies/${encodeURIComponent(companyID)}`;
  const dealURL = `${base}/teams/${encodeURIComponent(teamID)}/deals`;
  const contactURL = `${base}/contacts`;
  const [deals, setDeals] = useState<LoadResult<Deal>>({ status: 'loading' });
  const [contacts, setContacts] = useState<LoadResult<DealContact>>({ status: 'loading' });
  const [contactRevision, setContactRevision] = useState(0);
  const [formOpen, setFormOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [created, setCreated] = useState(false);
  const [formError, setFormError] = useState('');
  const submitting = useRef(false);
  const createButton = useRef<HTMLButtonElement | null>(null);

  const loadDeals = useCallback(async (signal: AbortSignal) => {
    try {
      const response = await request(dealURL, signal);
      if (!response || signal.aborted) return;
      if (response.status === 404 || response.status === 403) {
        setDeals({ status: 'unavailable' });
        setFormOpen(false);
        return;
      }
      if (!response.ok) {
        setDeals({ status: 'error' });
        return;
      }
      const items: unknown = await response.json();
      if (signal.aborted) return;
      if (Array.isArray(items) && items.every((item) => isDeal(item, companyID, teamID))) {
        setDeals({ status: 'ready', items });
      } else {
        setDeals({ status: 'error' });
      }
    } catch {
      if (!signal.aborted) setDeals({ status: 'error' });
    }
  }, [request, dealURL, companyID, teamID]);

  useEffect(() => {
    const signal = scope.current;
    if (signal && !signal.aborted) void loadDeals(signal);
  }, [loadDeals, scope, sessionSignal]);

  useEffect(() => {
    const workspaceSignal = scope.current;
    if (!workspaceSignal || workspaceSignal.aborted) return;
    const controller = new AbortController();
    const signal = AbortSignal.any([workspaceSignal, controller.signal]);
    async function loadContacts() {
      try {
        const response = await request(contactURL, signal);
        if (!response || signal.aborted) return;
        if (response.status === 403 || response.status === 404) {
          setContacts({ status: 'unavailable' });
          return;
        }
        if (!response.ok) {
          setContacts({ status: 'error' });
          return;
        }
        const items: unknown = await response.json();
        if (signal.aborted) return;
        if (Array.isArray(items) && items.every((item) => isDealContact(item, companyID))) {
          setContacts({ status: 'ready', items });
        } else {
          setContacts({ status: 'error' });
        }
      } catch {
        if (!signal.aborted) setContacts({ status: 'error' });
      }
    }
    void loadContacts();
    return () => controller.abort();
  }, [companyID, contactURL, contactRevision, request, scope, sessionSignal]);

  useEffect(() => {
    if (created && !saving) createButton.current?.focus();
  }, [created, saving]);

  function reloadContacts() {
    setContacts({ status: 'loading' });
    setContactRevision((revision) => revision + 1);
  }

  function reloadDeals() {
    const signal = scope.current;
    if (!signal || signal.aborted) return;
    setDeals({ status: 'loading' });
    void loadDeals(signal);
  }

  async function createDeal(input: DealInput) {
    const signal = scope.current;
    if (!signal || signal.aborted || submitting.current || authPending || deals.status !== 'ready') return;
    if (contacts.status !== 'ready' || !contacts.items.some((contact) => contact.id === input.contact_id)) return;
    submitting.current = true;
    setSaving(true);
    setCreated(false);
    setFormError('');
    try {
      const response = await request(dealURL, signal, input);
      if (!response || signal.aborted) return;
      if (response.status === 201) {
        setFormOpen(false);
        setCreated(true);
        await loadDeals(signal);
      } else if (response.status === 400) {
        setFormError('Check the deal details and try again.');
      } else if (response.status === 404 || response.status === 403) {
        setFormError('The team or contact is no longer available.');
        await loadDeals(signal);
        if (!signal.aborted) reloadContacts();
      } else if (response.status === 409) {
        setFormError('The server reported a conflict. Reload the deals before trying again.');
      } else {
        setFormError('Could not confirm that the deal was created. Reload the page before trying again.');
      }
    } catch {
      if (!signal.aborted) {
        setFormError('Could not reach DealForge. Reload the page before trying again.');
      }
    } finally {
      submitting.current = false;
      if (!signal.aborted) setSaving(false);
    }
  }

  const contactNames = new Map(contacts.status === 'ready' ? contacts.items.map((contact) => [contact.id, contact.name]) : []);

  return (
    <>
      {created && <p role="status">Deal created.</p>}
      {deals.status === 'loading' && <p role="status">Loading deals…</p>}
      {deals.status === 'unavailable' && (
        <>
          <p className="form-error" role="alert">This team’s deals are unavailable.</p>
          <button type="button" disabled={authPending} onClick={onReloadTeams}>Reload teams</button>
        </>
      )}
      {deals.status === 'error' && (
        <>
          <p className="form-error" role="alert">Could not load deals. Try again.</p>
          <button type="button" disabled={authPending || saving} onClick={reloadDeals}>Reload deals</button>
        </>
      )}
      {deals.status === 'ready' && (
        <>
          {deals.items.length === 0 ? <p>No deals yet for this team.</p> : (
            <ul className="deals-list" role="list" aria-label="Team deals">
              {deals.items.map((deal) => (
                <li key={deal.id}>
                  <h3>{deal.title}</h3>
                  <dl>
                    <div><dt>Contact</dt><dd>{contactNames.get(deal.contact_id) ?? (contacts.status === 'loading' ? 'Loading…' : 'Unavailable')}</dd></div>
                    <div><dt>Amount</dt><dd>{deal.amount} {deal.currency}</dd></div>
                    <div><dt>Stage</dt><dd>{stageLabel(deal.stage)}</dd></div>
                    {deal.expected_close_date && <div><dt>Expected closing date</dt><dd><time dateTime={deal.expected_close_date}>{deal.expected_close_date}</time></dd></div>}
                    {deal.lost_reason && <div><dt>Lost reason</dt><dd>{deal.lost_reason}</dd></div>}
                  </dl>
                </li>
              ))}
            </ul>
          )}
          <button type="button" ref={createButton} disabled={authPending || saving}
            aria-expanded={formOpen} aria-controls="create-deal-panel"
            onClick={() => {
              if (!formOpen) {
                setCreated(false);
                setFormError('');
                reloadContacts();
              }
              setFormOpen(!formOpen);
            }}>
            {formOpen ? 'Cancel' : 'Create deal'}
          </button>
          <div id="create-deal-panel" hidden={!formOpen}>
            {formOpen && (
              <>
                {contacts.status === 'loading' && <p role="status">Loading company contacts…</p>}
                {(contacts.status === 'error' || contacts.status === 'unavailable') && (
                  <p className="form-error" role="alert">Could not load company contacts.</p>
                )}
                {contacts.status === 'ready' && contacts.items.length === 0 && <p>Add a contact in this company before creating a deal.</p>}
                {contacts.status === 'ready' && contacts.items.length > 0 ? (
                  <DealForm contacts={contacts.items} disabled={saving || authPending} saving={saving}
                    error={formError} onSubmit={createDeal} />
                ) : contacts.status !== 'loading' && (
                  <button type="button" disabled={authPending || saving} onClick={reloadContacts}>Reload contacts</button>
                )}
              </>
            )}
          </div>
        </>
      )}
    </>
  );
}
