import { useCallback, useEffect, useState } from 'react';
import { isTeam, useDealRequests, type DealWorkspaceProps, type LoadResult, type Team } from './deal-workspace-api';
import { TeamDeals } from './team-deals';

export function Deals(props: DealWorkspaceProps) {
  const { companyID, authPending, sessionSignal } = props;
  const { scope, request } = useDealRequests(props);
  const [teams, setTeams] = useState<LoadResult<Team>>({ status: 'loading' });
  const [selectedID, setSelectedID] = useState('');

  const loadTeams = useCallback(async () => {
    const signal = scope.current;
    if (!signal || signal.aborted) return;
    setTeams({ status: 'loading' });
    try {
      const response = await request(`/api/v1/companies/${encodeURIComponent(companyID)}/teams`, signal);
      if (!response || signal.aborted) return;
      if (response.status === 403 || response.status === 404) {
        setTeams({ status: 'unavailable' });
        return;
      }
      if (!response.ok) {
        setTeams({ status: 'error' });
        return;
      }
      const items: unknown = await response.json();
      if (signal.aborted) return;
      if (Array.isArray(items) && items.every((item) => isTeam(item, companyID))) {
        setTeams({ status: 'ready', items });
      } else {
        setTeams({ status: 'error' });
      }
    } catch {
      if (!signal.aborted) setTeams({ status: 'error' });
    }
  }, [companyID, request, scope]);

  useEffect(() => { void loadTeams(); }, [loadTeams, sessionSignal]);

  const selected = teams.status === 'ready'
    ? teams.items.find((team) => team.id === selectedID) ?? teams.items[0]
    : undefined;

  return (
    <section className="deals" aria-labelledby="deals-title">
      <h2 id="deals-title">Deals</h2>
      {teams.status === 'loading' && <p role="status">Loading teams…</p>}
      {teams.status === 'unavailable' && <p className="form-error" role="alert">This company’s teams are unavailable.</p>}
      {teams.status === 'error' && (
        <>
          <p className="form-error" role="alert">Could not load teams. Try again.</p>
          <button type="button" disabled={authPending} onClick={loadTeams}>Reload teams</button>
        </>
      )}
      {teams.status === 'ready' && (selected ? (
        <>
          <div className="field">
            <label htmlFor="deal-team">Team</label>
            <select id="deal-team" value={selected.id} disabled={authPending}
              onChange={(event) => setSelectedID(event.currentTarget.value)}>
              {teams.items.map((team) => <option key={team.id} value={team.id}>{team.name}</option>)}
            </select>
          </div>
          <TeamDeals key={selected.id} {...props} teamID={selected.id} onReloadTeams={loadTeams} />
        </>
      ) : <p>You don’t belong to any active teams in this company.</p>)}
    </section>
  );
}
