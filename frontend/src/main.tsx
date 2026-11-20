import { createRoot } from 'react-dom/client';

const root = document.getElementById('root');
if (!root) {
  throw new Error('Missing application root');
}

createRoot(root).render(
  <main>
    <h1>DealForge</h1>
  </main>,
);
