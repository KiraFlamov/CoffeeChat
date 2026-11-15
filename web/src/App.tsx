import { useCallback, useEffect, useRef, useState } from 'react';
import { runChecks, type CheckDeps } from './checks/runChecks';
import { initialChecks } from './checks/types';
import { StatusPage } from './StatusPage';

export interface AppProps {
  readonly deps: CheckDeps;
}

/** Запускает проверки при открытии страницы и по кнопке «Повторить». */
export function App({ deps }: AppProps) {
  const [checks, setChecks] = useState(initialChecks);
  const [running, setRunning] = useState(true);
  // Номер текущего запуска: результаты прежних запусков отбрасываются.
  const runId = useRef(0);

  const start = useCallback(() => {
    const id = ++runId.current;
    setChecks(initialChecks());
    setRunning(true);
    void runChecks(deps, (checkId, state) => {
      if (runId.current === id) setChecks((prev) => ({ ...prev, [checkId]: state }));
    }).then(() => {
      if (runId.current === id) setRunning(false);
    });
  }, [deps]);

  useEffect(() => {
    start();
    return () => {
      runId.current++;
    };
  }, [start]);

  return <StatusPage checks={checks} running={running} onRetry={start} />;
}
