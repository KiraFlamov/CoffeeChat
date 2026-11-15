/** Проверки страницы «Состояние системы», в порядке показа. */
export const checkIds = ['http', 'store', 'wsBody', 'wsSend', 'workers'] as const;

export type CheckId = (typeof checkIds)[number];

export type CheckState =
  | { readonly status: 'pending' }
  | { readonly status: 'ok'; readonly detail?: string }
  | { readonly status: 'fail'; readonly reason: string };

export type Checks = Readonly<Record<CheckId, CheckState>>;

export function initialChecks(): Checks {
  const pending: CheckState = { status: 'pending' };
  return { http: pending, store: pending, wsBody: pending, wsSend: pending, workers: pending };
}
