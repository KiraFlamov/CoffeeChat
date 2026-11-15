import { checkIds, type Checks, type CheckState } from './checks/types';
import { strings } from './strings';

const dotClass: Record<CheckState['status'], string> = {
  pending: 'bg-slate-300 animate-pulse',
  ok: 'bg-emerald-500',
  fail: 'bg-red-500',
};

const statusClass: Record<CheckState['status'], string> = {
  pending: 'text-slate-500',
  ok: 'text-emerald-700',
  fail: 'text-red-700',
};

function note(state: CheckState): string | undefined {
  switch (state.status) {
    case 'pending':
      return undefined;
    case 'ok':
      return state.detail;
    case 'fail':
      return state.reason;
  }
}

export interface StatusPageProps {
  readonly checks: Checks;
  readonly running: boolean;
  readonly onRetry: () => void;
}

/** Страница «Состояние системы»: по строке на проверку и кнопка «Повторить». */
export function StatusPage({ checks, running, onRetry }: StatusPageProps) {
  return (
    <main className="mx-auto max-w-xl p-6 font-sans text-slate-900">
      <h1 className="text-2xl font-semibold">{strings.title}</h1>
      <p className="mt-1 text-sm text-slate-500">{strings.subtitle}</p>

      <ul className="mt-6 divide-y divide-slate-200 rounded-lg border border-slate-200">
        {checkIds.map((id) => {
          const state = checks[id];
          const text = note(state);
          return (
            <li
              key={id}
              data-check={id}
              data-status={state.status}
              className="flex items-start gap-3 p-4"
            >
              <span
                aria-hidden="true"
                className={`mt-1.5 size-3 shrink-0 rounded-full ${dotClass[state.status]}`}
              />
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline justify-between gap-3">
                  <span className="font-medium">{strings.checks[id]}</span>
                  <span className={`text-sm ${statusClass[state.status]}`}>
                    {strings.status[state.status]}
                  </span>
                </div>
                {text !== undefined && (
                  <p className="mt-0.5 text-sm break-words text-slate-500">{text}</p>
                )}
              </div>
            </li>
          );
        })}
      </ul>

      <button
        type="button"
        onClick={onRetry}
        disabled={running}
        className="mt-6 rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-50"
      >
        {running ? strings.running : strings.retry}
      </button>
    </main>
  );
}
