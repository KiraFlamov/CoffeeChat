import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { createBrowserDeps } from './checks/browserDeps';
import { loadConfig } from './config';
import './index.css';

const root = document.getElementById('root');
if (!root) {
  throw new Error('В index.html нет элемента #root');
}

const deps = createBrowserDeps(loadConfig());

createRoot(root).render(
  <StrictMode>
    <App deps={deps} />
  </StrictMode>,
);
