import { useCallback, useLayoutEffect, useRef } from 'react';

export type DealWorkspaceProps = {
  companyID: string;
  userID: string;
  sessionSignal: AbortSignal;
  authPending: boolean;
  onSessionExpired: () => void;
  onSessionChanged: () => void;
};

export type Deal = {
  id: string;
  company_id: string;
  team_id: string;
  contact_id: string;
  title: string;
  amount: string;
  currency: string;
  stage: DealStage;
  expected_close_date: string | null;
  lost_reason: string | null;
};

export type Team = { id: string; company_id: string; name: string; role: 'manager' | 'salesperson' };
export type DealContact = { id: string; company_id: string; name: string; email: string };
export type LoadResult<T> = { status: 'loading' | 'error' | 'unavailable' } | { status: 'ready'; items: T[] };
export type DealInput = Pick<Deal, 'title' | 'contact_id' | 'amount' | 'currency'> & {
  stage?: DealStage;
  expected_close_date?: string;
  lost_reason?: string;
};

export const stages = ['new', 'qualified', 'proposal', 'won', 'lost'] as const;
type DealStage = typeof stages[number];

export function isDealStage(stage: unknown): stage is DealStage {
  return stages.some((value) => value === stage);
}

export function stageLabel(stage: DealStage) {
  return stage.charAt(0).toUpperCase() + stage.slice(1);
}

export function isTeam(value: unknown, companyID: string): value is Team {
  const team = value as Partial<Team> | null;
  return team?.company_id === companyID && typeof team.id === 'string' && team.id !== '' &&
    typeof team.name === 'string' && (team.role === 'manager' || team.role === 'salesperson');
}

export function isDealContact(value: unknown, companyID: string): value is DealContact {
  const contact = value as Partial<DealContact> | null;
  return contact?.company_id === companyID && typeof contact.id === 'string' && contact.id !== '' &&
    typeof contact.name === 'string' && typeof contact.email === 'string';
}

export function isDeal(value: unknown, companyID: string, teamID: string): value is Deal {
  const deal = value as Partial<Deal> | null;
  return deal?.company_id === companyID && deal.team_id === teamID &&
    typeof deal.id === 'string' && deal.id !== '' &&
    typeof deal.title === 'string' && typeof deal.contact_id === 'string' &&
    typeof deal.amount === 'string' && typeof deal.currency === 'string' && isDealStage(deal.stage) &&
    (deal.expected_close_date === null || typeof deal.expected_close_date === 'string') &&
    (deal.lost_reason === null || typeof deal.lost_reason === 'string');
}

export function useDealRequests({
  userID, sessionSignal, onSessionExpired, onSessionChanged,
}: DealWorkspaceProps) {
  const scope = useRef<AbortSignal | null>(null);
  // The keyed workspace invalidates pending callbacks when its company or team changes.
  useLayoutEffect(() => {
    const controller = new AbortController();
    scope.current = AbortSignal.any([sessionSignal, controller.signal]);
    return () => {
      controller.abort();
      scope.current = null;
    };
  }, [sessionSignal]);

  const request = useCallback(async (url: string, signal: AbortSignal, body?: DealInput) => {
    if (signal.aborted) return;
    const headers: Record<string, string> = { 'X-DealForge-Expected-User-ID': userID };
    if (body) headers['Content-Type'] = 'application/json';
    const response = await fetch(url, {
      method: body ? 'POST' : 'GET',
      credentials: 'same-origin',
      headers,
      body: body ? JSON.stringify(body) : undefined,
      signal,
    });
    if (signal.aborted) return;
    if (response.status === 401) onSessionExpired();
    else if (response.status === 412) onSessionChanged();
    else return response;
  }, [userID, onSessionExpired, onSessionChanged]);

  return { scope, request };
}
