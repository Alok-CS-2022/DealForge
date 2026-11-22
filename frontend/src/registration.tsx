import { type FormEvent, useRef, useState } from 'react';

type RegistrationInput = {
  username: string;
  email: string;
  display_name: string;
  password: string;
};

type FieldErrors = Partial<Record<keyof RegistrationInput, string>>;

function validate(input: RegistrationInput, emailInvalid: boolean): FieldErrors {
  const errors: FieldErrors = {};
  const bytes = new TextEncoder();
  if (!/^[a-z0-9][a-z0-9_.-]{2,31}$/i.test(input.username.trim())) {
    errors.username = 'Use 3–32 letters, numbers, dots, underscores, or hyphens. Start with a letter or number.';
  }
  if (!input.display_name.trim() || Array.from(input.display_name.trim()).length > 100) {
    errors.display_name = 'Enter a display name of 1–100 characters.';
  }
  if (!input.email.trim() || emailInvalid || bytes.encode(input.email.trim()).length > 254) {
    errors.email = 'Enter a valid email address.';
  }
  if (!input.password.trim() || Array.from(input.password).length < 15 || bytes.encode(input.password).length > 256) {
    errors.password = 'Use at least 15 characters and at most 256 bytes.';
  }
  return errors;
}

async function registrationError(response: Response): Promise<string> {
  if (response.status === 400) {
    const body: { error?: unknown } | null = await response.json().catch(() => null);
    return typeof body?.error === 'string' && body.error
      ? body.error
      : 'Check your account details and try again.';
  }
  if (response.status === 409) {
    return 'That username or email is already registered. Use a different one to create an account.';
  }
  if (response.status === 503) {
    return 'Registration is busy. Wait a moment, then try again.';
  }
  return 'The server could not create your account. Try again in a moment.';
}

export function Registration() {
  const inFlight = useRef(false);
  const [submitting, setSubmitting] = useState(false);
  const [created, setCreated] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [error, setError] = useState('');

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current) return;

    const form = event.currentTarget;
    const fields = new FormData(form);
    const input: RegistrationInput = {
      username: String(fields.get('username') ?? ''),
      email: String(fields.get('email') ?? ''),
      display_name: String(fields.get('display_name') ?? ''),
      password: String(fields.get('password') ?? ''),
    };
    const email = form.elements.namedItem('email') as HTMLInputElement;
    const errors = validate(input, email.validity.typeMismatch);
    setFieldErrors(errors);
    setError('');
    const firstInvalid = Object.keys(errors)[0];
    if (firstInvalid) {
      (form.elements.namedItem(firstInvalid) as HTMLInputElement).focus();
      return;
    }

    inFlight.current = true;
    setSubmitting(true);
    const payload = JSON.stringify(input);
    (form.elements.namedItem('password') as HTMLInputElement).value = '';
    try {
      const response = await fetch('/api/v1/auth/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: payload,
      });
      if (response.status === 201) {
        setCreated(true);
      } else {
        setError(await registrationError(response));
      }
    } catch {
      setError('Could not reach DealForge. Check your connection and try again.');
    } finally {
      inFlight.current = false;
      setSubmitting(false);
    }
  }

  return (
    <main className="registration">
      <div className="registration-shell">
        <div className="wordmark">
          <span className="wordmark-symbol" aria-hidden="true">D</span>
          <span>DealForge</span>
        </div>
        <section className="registration-card" aria-labelledby="registration-title">
          {created ? (
            <div className="confirmation">
              <span className="confirmation-mark" aria-hidden="true">✓</span>
              <h1 id="registration-title" tabIndex={-1} ref={(heading) => heading?.focus()}>
                Account created
              </h1>
              <p>Log in with your username and password to get started.</p>
            </div>
          ) : (
            <>
              <h1 id="registration-title">Create an account</h1>
              <p className="intro">Enter your details to set up your DealForge account.</p>
              <form method="post" noValidate onSubmit={submit} aria-busy={submitting}>
                {error && <p className="form-error" role="alert">{error}</p>}
                <fieldset disabled={submitting} aria-label="Account details">
                  <div className="field">
                    <label htmlFor="display_name">Display name</label>
                    <input id="display_name" name="display_name" autoComplete="nickname" required
                      aria-invalid={Boolean(fieldErrors.display_name)}
                      aria-describedby={fieldErrors.display_name ? 'display-name-error' : undefined} />
                    {fieldErrors.display_name && <p id="display-name-error" className="field-error">{fieldErrors.display_name}</p>}
                  </div>
                  <div className="field">
                    <label htmlFor="username">Username</label>
                    <input id="username" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false}
                      required aria-invalid={Boolean(fieldErrors.username)}
                      aria-describedby={fieldErrors.username ? 'username-error' : undefined} />
                    {fieldErrors.username && <p id="username-error" className="field-error">{fieldErrors.username}</p>}
                  </div>
                  <div className="field">
                    <label htmlFor="email">Email address</label>
                    <input id="email" name="email" type="email" autoComplete="email" autoCapitalize="none" spellCheck={false}
                      required aria-invalid={Boolean(fieldErrors.email)}
                      aria-describedby={fieldErrors.email ? 'email-error' : undefined} />
                    {fieldErrors.email && <p id="email-error" className="field-error">{fieldErrors.email}</p>}
                  </div>
                  <div className="field">
                    <label htmlFor="password">Password</label>
                    <input id="password" name="password" type="password" autoComplete="new-password" required
                      aria-invalid={Boolean(fieldErrors.password)} aria-describedby="password-help" />
                    <p id="password-help" className={fieldErrors.password ? 'field-error' : 'field-hint'}>
                      {fieldErrors.password ?? 'Use at least 15 characters.'}
                    </p>
                  </div>
                  <button type="submit">{submitting ? 'Creating account…' : 'Create account'}</button>
                </fieldset>
              </form>
            </>
          )}
        </section>
      </div>
    </main>
  );
}
