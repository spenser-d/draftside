import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import DraftApp from './DraftApp';
import './index.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <DraftApp />
  </StrictMode>,
);
