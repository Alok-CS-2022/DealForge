import { createRoot } from 'react-dom/client';
import { Login } from './login';
import { Registration } from './registration';

const root = document.getElementById('root');
if (!root) {
  throw new Error('Missing application root');
}

const showLogin = window.location.pathname === '/login' || window.location.pathname === '/login/';
if (showLogin) document.title = 'Log in | DealForge';
createRoot(root).render(showLogin ? <Login /> : <Registration />);
