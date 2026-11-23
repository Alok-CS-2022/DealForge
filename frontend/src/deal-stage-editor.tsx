import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { isDealStage, stageLabel, stages, type Deal, type DealStageInput } from './deal-workspace-api';

export type StageEdit = { dealID: string; status: 'editing' | 'saving' | 'saved'; error?: string };

type Props = {
  deal: Deal;
  edit: StageEdit | null;
  disabled: boolean;
  onToggle: () => void;
  onSave: (input: DealStageInput) => void;
};

export function DealStageEditor({ deal, edit, disabled, onToggle, onSave }: Props) {
  const id = useId();
  const toggle = useRef<HTMLButtonElement>(null);
  const open = edit !== null && edit.status !== 'saved';
  const saved = edit?.status === 'saved';
  const saving = edit?.status === 'saving';

  useEffect(() => {
    if (saved) toggle.current?.focus();
  }, [saved]);

  return (
    <div className="deal-stage-editor">
      <button type="button" ref={toggle} aria-expanded={open} aria-controls={id}
        aria-label={open ? `Cancel stage change for ${deal.title}` : `Change stage for ${deal.title}`}
        disabled={disabled} onClick={onToggle}>
        {open ? 'Cancel stage change' : 'Change stage'}
      </button>
      {saved && <p role="status">Stage updated.</p>}
      <div id={id} hidden={!open}>
        {open && (
          <StageForm deal={deal} disabled={disabled} saving={saving} error={edit.error} onSave={onSave} />
        )}
        {saving && <p role="status">Saving stage…</p>}
      </div>
    </div>
  );
}

function StageForm({ deal, disabled, saving, error, onSave }: {
  deal: Deal;
  disabled: boolean;
  saving: boolean;
  error?: string;
  onSave: Props['onSave'];
}) {
  const id = useId();
  const [stage, setStage] = useState(deal.stage);
  const [reason, setReason] = useState('');
  const [validation, setValidation] = useState('');

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (disabled) return;
    const lostReason = stage === 'lost' ? reason.trim() : '';
    if (Array.from(lostReason).length > 500 || lostReason.includes('\0')) {
      setValidation('Enter a lost reason of up to 500 characters.');
      return;
    }
    setValidation('');
    onSave({ stage, lost_reason: lostReason || undefined });
  }

  return (
    <form method="post" onSubmit={submit} aria-label={`Update stage for ${deal.title}`} aria-busy={saving}>
      {(validation || error) && <p className="form-error" role="alert">{validation || error}</p>}
      <fieldset disabled={disabled}>
        <div className="field">
          <label htmlFor={`${id}-stage`}>Stage</label>
          <select id={`${id}-stage`} value={stage} onChange={(event) => {
            const value = event.currentTarget.value;
            if (isDealStage(value)) {
              setStage(value);
              setValidation('');
            }
          }}>
            {stages.map((value) => <option value={value} key={value}>{stageLabel(value)}</option>)}
          </select>
        </div>
        {stage === 'lost' && (
          <div className="field">
            <label htmlFor={`${id}-reason`}>Lost reason (optional)</label>
            <textarea id={`${id}-reason`} rows={2} value={reason}
              aria-describedby={deal.lost_reason ? `${id}-hint` : undefined}
              onChange={(event) => { setReason(event.currentTarget.value); setValidation(''); }} />
            {deal.lost_reason && <p id={`${id}-hint`} className="field-hint">Leave blank to keep the existing reason.</p>}
          </div>
        )}
        <button type="submit">{saving ? 'Saving stage…' : 'Save stage'}</button>
      </fieldset>
    </form>
  );
}
