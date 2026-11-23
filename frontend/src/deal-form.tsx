import { type FormEvent, useState } from 'react';
import { isDealStage, stageLabel, stages, type DealContact, type DealInput } from './deal-workspace-api';

type DealFormProps = {
  contacts: DealContact[];
  disabled: boolean;
  saving: boolean;
  error: string;
  onSubmit: (input: DealInput) => void;
};

export function DealForm({ contacts, disabled, saving, error, onSubmit }: DealFormProps) {
  const [stage, setStage] = useState('new');
  const [validation, setValidation] = useState('');

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (disabled) return;
    const data = new FormData(event.currentTarget);
    const value = (name: string) => String(data.get(name) ?? '').trim();
    const title = value('title');
    const contactID = value('contact_id');
    const amount = value('amount');
    const currency = value('currency');
    const closeDate = value('expected_close_date');
    const lostReason = value('lost_reason');
    if (!title || Array.from(title).length > 100) {
      setValidation('Enter a deal title of 1–100 characters.');
    } else if (!contacts.some((contact) => contact.id === contactID)) {
      setValidation('Select a contact from this company.');
    } else if (!/^[0-9]{1,15}(\.[0-9]{1,4})?$/.test(amount)) {
      setValidation('Enter a nonnegative amount with up to 15 whole digits and 4 decimal places.');
    } else if (!/^[a-zA-Z]{3}$/.test(currency)) {
      setValidation('Enter a three-letter currency code.');
    } else if (!isDealStage(stage)) {
      setValidation('Select a valid stage.');
    } else if (closeDate && !/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(closeDate)) {
      setValidation('Enter a closing date in YYYY-MM-DD format.');
    } else if (stage === 'lost' && Array.from(lostReason).length > 500) {
      setValidation('Enter a lost reason of at most 500 characters.');
    } else {
      setValidation('');
      onSubmit({
        title, contact_id: contactID, amount, currency: currency.toUpperCase(),
        stage: stage === 'new' ? undefined : stage,
        expected_close_date: closeDate || undefined,
        lost_reason: stage === 'lost' ? lostReason || undefined : undefined,
      });
    }
  }

  return (
    <form method="post" onSubmit={submit} aria-labelledby="create-deal-title" aria-busy={saving}>
      <h3 id="create-deal-title">Create deal</h3>
      {(validation || error) && <p className="form-error" role="alert">{validation || error}</p>}
      <fieldset disabled={disabled}>
        <div className="field">
          <label htmlFor="deal-title">Deal title</label>
          <input id="deal-title" name="title" required />
        </div>
        <div className="field">
          <label htmlFor="deal-contact">Contact</label>
          <select id="deal-contact" name="contact_id" defaultValue="" required>
            <option value="" disabled>Select a contact</option>
            {contacts.map((contact) => <option key={contact.id} value={contact.id}>{contact.name} ({contact.email})</option>)}
          </select>
        </div>
        <div className="field">
          <label htmlFor="deal-amount">Amount</label>
          <input id="deal-amount" name="amount" inputMode="decimal" required />
        </div>
        <div className="field">
          <label htmlFor="deal-currency">Currency code</label>
          <input id="deal-currency" name="currency" autoCapitalize="characters" placeholder="USD" required />
        </div>
        <div className="field">
          <label htmlFor="deal-stage">Stage</label>
          <select id="deal-stage" name="stage" value={stage} onChange={(event) => setStage(event.currentTarget.value)}>
            {stages.map((value) => <option value={value} key={value}>{stageLabel(value)}</option>)}
          </select>
        </div>
        <div className="field">
          <label htmlFor="deal-close-date">Expected closing date (optional)</label>
          <input id="deal-close-date" name="expected_close_date" type="date" />
        </div>
        {stage === 'lost' && (
          <div className="field">
            <label htmlFor="deal-lost-reason">Lost reason (optional)</label>
            <textarea id="deal-lost-reason" name="lost_reason" rows={3} />
          </div>
        )}
        <button type="submit">{saving ? 'Creating deal…' : 'Create deal'}</button>
      </fieldset>
    </form>
  );
}
